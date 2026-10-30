//go:build windows

package collector

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestWindowsGPUUsageAggregation(t *testing.T) {
	counters := []windowsGPUEngine{
		{Name: `pid_1_luid_0x00000000_0x0000FF01_phys_0_eng_0_engtype_3D`, UtilizationPercentage: 40},
		{Name: `pid_2_luid_0x00000000_0x0000FF01_phys_0_eng_0_engtype_3D`, UtilizationPercentage: 30},
		{Name: `pid_1_luid_0x00000000_0x0000FF01_phys_0_eng_1_engtype_Copy`, UtilizationPercentage: 20},
		{Name: `pid_1_luid_0x00000000_0x0000FF02_phys_0_eng_0_engtype_3D`, UtilizationPercentage: 55},
		{Name: `malformed`, UtilizationPercentage: 99},
	}
	usage := gpuUsageByLUID(counters)
	if len(usage) != 2 || usage["0x00000000_0x0000FF01"] != 70 || usage["0x00000000_0x0000FF02"] != 55 {
		t.Fatalf("usage: %v", usage)
	}
	capped := gpuUsageByLUID([]windowsGPUEngine{
		{Name: `pid_1_luid_0x00000000_0x0000FF01_phys_0_eng_0_engtype_3D`, UtilizationPercentage: 90},
		{Name: `pid_2_luid_0x00000000_0x0000FF01_phys_0_eng_0_engtype_3D`, UtilizationPercentage: 80},
	})
	if capped["0x00000000_0x0000FF01"] != 100 {
		t.Fatalf("engine sum not capped: %v", capped)
	}
	if len(gpuUsageByLUID(nil)) != 0 {
		t.Fatal("missing counters invented usage")
	}
}

func TestWindowsDiskUsageMapping(t *testing.T) {
	partitions := []windowsPartition{
		{disk: 0, letter: "C"}, {disk: 0}, {disk: 1, letter: "D"},
		{disk: 2, letter: "E"}, {disk: 2, letter: "E"},
	}
	volumes := []windowsLogicalDisk{
		{letter: "c", driveType: windowsDriveFixed, size: 100, free: 40},
		{letter: "D", driveType: windowsDriveFixed, size: 200, free: 150},
		{letter: "E", driveType: windowsDriveFixed, size: 50, free: 10},
		{letter: "F", driveType: 5, size: 50, free: 0},
		{letter: "G", driveType: windowsDriveFixed, size: 10, free: 20},
		{letter: "H", driveType: windowsDriveFixed, size: 10, free: 5},
	}
	usage := windowsDiskUsage(partitions, volumes)
	if len(usage) != 2 || usage["PHYSICALDRIVE0"] != 60 || usage["PHYSICALDRIVE1"] != 50 {
		t.Fatalf("usage: %v", usage)
	}
	if len(windowsDiskUsage(nil, nil)) != 0 {
		t.Fatal("empty partitions invented usage")
	}
}

func TestWindowsBatteryPairing(t *testing.T) {
	if _, ok := batteryFields(systemPowerStatus{BatteryFlag: windowsBatteryNoSystem, BatteryLifePercent: 255}); ok {
		t.Fatal("desktop reported a battery")
	}
	if _, ok := batteryFields(systemPowerStatus{BatteryFlag: windowsBatteryUndefined, BatteryLifePercent: 50}); ok {
		t.Fatal("undefined flag reported a battery")
	}
	if _, ok := batteryFields(systemPowerStatus{BatteryFlag: 0, BatteryLifePercent: 255}); ok {
		t.Fatal("unknown percentage reported a battery")
	}
	fields, ok := batteryFields(systemPowerStatus{BatteryFlag: windowsBatteryCharging, BatteryLifePercent: 80})
	if !ok || fields["charge_percent"] != 80 || fields["charging"] != 1 {
		t.Fatalf("charging: %v %v", fields, ok)
	}
	fields, ok = batteryFields(systemPowerStatus{BatteryFlag: 0, BatteryLifePercent: 100})
	if !ok || fields["charge_percent"] != 100 || fields["charging"] != 0 {
		t.Fatalf("full battery: %v %v", fields, ok)
	}
}

func TestWindowsSystemInfo(t *testing.T) {
	tags := windowsSystemInfo(map[string]string{
		"ProductName": "Windows 10 Pro", "DisplayVersion": "25H2", "CurrentBuild": "26200", "UBR": "9711",
		"SystemManufacturer": "MAXSUN", "SystemProductName": "MS-eSport B650ITX",
	})
	if tags["os_name"] != "Windows 11 Pro" || tags["os_version"] != "25H2" || tags["os_build"] != "26200.9711" || tags["product_name"] != "MAXSUN MS-eSport B650ITX" {
		t.Fatalf("system tags: %v", tags)
	}
	tags = windowsSystemInfo(map[string]string{"ProductName": "Windows 10 Pro", "ReleaseId": "2009", "CurrentBuild": "19045"})
	if tags["os_name"] != "Windows 10 Pro" || tags["os_version"] != "2009" || tags["os_build"] != "19045" {
		t.Fatalf("windows 10 tags: %v", tags)
	}
	tags = windowsSystemInfo(map[string]string{"SystemManufacturer": "MAXSUN", "SystemProductName": "Default string", "BaseBoardProduct": "B650M"})
	if tags["product_name"] != "MAXSUN B650M" {
		t.Fatalf("placeholder model used: %v", tags)
	}
	if tags := windowsSystemInfo(map[string]string{"SystemProductName": "Default string"}); tags["product_name"] != "" {
		t.Fatalf("placeholder product invented: %v", tags)
	}
	if got := windowsProductName("Windows 11 Home", "22631"); got != "Windows 11 Home" {
		t.Fatalf("windows 11 renamed: %q", got)
	}
}

func TestWindowsAdapterKinds(t *testing.T) {
	adapters := []windowsNetworkAdapter{
		{GUID: "{37217669-42DA-4657-A55B-0D995D328250}", Name: "Tailscale Tunnel", PNPDeviceID: ptr(`SWD\WINTUN\{37217669}`), PhysicalAdapter: ptr(true)},
		{GUID: "{D17AED9D-C1B9-40C2-9034-CC5C24553502}", Name: "Realtek Gaming 2.5GbE Family Controller", NetConnectionID: ptr("以太网 2"), PNPDeviceID: ptr(`PCI\VEN_10EC&DEV_8125`), PhysicalAdapter: ptr(true)},
		{GUID: "{1EC68CE2-A9F1-490E-B975-A75125C943B6}", Name: "Intel(R) Wi-Fi 6E AX210 160MHz", NetConnectionID: ptr("WLAN"), PNPDeviceID: ptr(`PCI\VEN_8086&DEV_2725`), PhysicalAdapter: ptr(true)},
		{Name: "Bluetooth Device (Personal Area Network)", PNPDeviceID: ptr(`BTH\MS_BTHPAN`), PhysicalAdapter: ptr(true)},
		{Name: "USB 2.5G LAN Adapter", PNPDeviceID: ptr(`USB\VID_0BDA`), PhysicalAdapter: ptr(true)},
		{GUID: "{disabled}", Name: "Intel(R) Ethernet Controller I225-V", PNPDeviceID: ptr(`PCI\VEN_8086&DEV_125C`), PhysicalAdapter: ptr(false)},
	}
	kinds := windowsAdapterKinds(adapters)
	for name, want := range map[string]string{
		"WLAN":                                   "wifi",
		"以太网 2":                                  "lan",
		"{1EC68CE2-A9F1-490E-B975-A75125C943B6}": "wifi",
		"USB 2.5G LAN Adapter":                   "lan",
	} {
		if got, ok := adapterKindFor(kinds, name); !ok || got != want {
			t.Fatalf("%s = %q, %v; want %q", name, got, ok, want)
		}
	}
	for _, name := range []string{"Tailscale", "Bluetooth Device (Personal Area Network)", "Intel(R) Ethernet Controller I225-V"} {
		if _, ok := adapterKindFor(kinds, name); ok {
			t.Fatalf("non-physical adapter %q listed", name)
		}
	}
}

func TestWindowsLHMCPUTemperatureSelection(t *testing.T) {
	f32 := func(value float32) *float32 { return &value }
	sensors := []windowsLMHSensor{
		{Identifier: "/intelcpu/0/temperature/0", Name: "CPU Package", SensorType: "Temperature", Value: f32(55)},
		{Identifier: "/intelcpu/0/temperature/1", Name: "CPU Core #1", SensorType: "Temperature", Value: f32(45)},
		{Identifier: "/gpu-nvidia/0/temperature/0", Name: "GPU Core", SensorType: "Temperature", Value: f32(60)},
		{Identifier: "/amdcpu/0/temperature/0", Name: "Core (Tctl/Tdie)", SensorType: "Temperature", Value: f32(65)},
		{Identifier: "/amdcpu/0/load/0", Name: "CPU Total", SensorType: "Load", Value: f32(30)},
		{Identifier: "/amdcpu/0/temperature/1", Name: "Core #1", SensorType: "Temperature", Value: f32(0)},
	}
	value, ok := lhmCPUTemperature(sensors)
	if !ok || value != 60 {
		t.Fatalf("package/tctl average: %v %v", value, ok)
	}
	if value, ok = lhmCPUTemperature(sensors[1:3]); !ok || value != 45 {
		t.Fatalf("core fallback: %v %v", value, ok)
	}
	if _, ok = lhmCPUTemperature(sensors[4:]); ok {
		t.Fatal("non-CPU temperature invented")
	}
}

func TestWindowsLHMStatusAndMappings(t *testing.T) {
	status := hardwareMonitorStatus(map[string]string{"windows_cpu": "dotnet_framework_472_required"})
	if status == nil || status.State != "missing_dependency" || status.Code != "dotnet_framework_472_required" {
		t.Fatalf("runtime requirement was not exposed: %#v", status)
	}
	if status := hardwareMonitorStatus(map[string]string{"windows_cpu": "unavailable"}); status != nil {
		t.Fatalf("ordinary unavailable result became a dependency warning: %#v", status)
	}
	status = hardwareMonitorStatus(map[string]string{"windows_disk": "lhm_permission_required"})
	if status == nil || status.State != "permission_required" || status.Code != "lhm_permission_required" {
		t.Fatalf("LHM permission requirement was not exposed: %#v", status)
	}
	if reason := lhmDataReason(windowsLHMData{Warnings: []string{"not_elevated"}}, "windows_disk"); reason != "lhm_permission_required" {
		t.Fatalf("elevation warning reason: %q", reason)
	}
	gpus := []windowsGPUInfo{{device: "gpu0", model: "NVIDIA Test"}, {device: "gpu1", model: "NVIDIA Test"}}
	fields := lhmGPUFields(gpus, []windowsLHMGPU{{Model: "NVIDIA Test", Temperature: ptr(54.5)}})
	if len(fields) != 0 {
		t.Fatalf("ambiguous LHM GPU was attributed: %#v", fields)
	}
	fields = lhmGPUFields(gpus[:1], []windowsLHMGPU{{Model: "NVIDIA Test", Temperature: ptr(54.5)}})
	if fields["gpu0"]["temperature_celsius"] != 54.5 {
		t.Fatalf("LHM GPU temperature lost: %#v", fields)
	}
	disks := lhmDiskByDevice([]windowsLHMDisk{{Device: "physicaldrive0"}})
	if _, ok := disks["PHYSICALDRIVE0"]; !ok {
		t.Fatalf("disk identity was not normalized: %#v", disks)
	}
}

func TestWindowsProcessorBusy(t *testing.T) {
	u64 := func(value uint64) *uint64 { return &value }
	rows := []windowsPerfProcessor{
		{Name: "_Total", PercentProcessorTime: u64(42)},
		{Name: "0", PercentProcessorTime: u64(10)},
		{Name: "1", PercentProcessorTime: u64(20)},
		{Name: "2", PercentProcessorTime: nil},
	}
	if value, ok := processorBusy(rows); !ok || value != 42 {
		t.Fatalf("total: %v %v", value, ok)
	}
	if value, ok := processorBusy(rows[1:]); !ok || value != 15 {
		t.Fatalf("per-processor average: %v %v", value, ok)
	}
	if value, ok := processorBusy([]windowsPerfProcessor{{Name: "_Total", PercentProcessorTime: u64(150)}}); !ok || value != 100 {
		t.Fatalf("clamp: %v %v", value, ok)
	}
	if _, ok := processorBusy(nil); ok {
		t.Fatal("missing counters invented usage")
	}
}

func TestWindowsLoadEWMA(t *testing.T) {
	var state windowsLoadState
	start := time.Unix(1000, 0)
	load1, load5, load15 := state.observe(2, start)
	if load1 != 2 || load5 != 2 || load15 != 2 {
		t.Fatalf("seed: %v %v %v", load1, load5, load15)
	}
	load1, load5, load15 = state.observe(8, start.Add(60*time.Second))
	if load1 < 5.79 || load1 > 5.80 || load5 < 3.08 || load5 > 3.09 || load15 < 2.38 || load15 > 2.39 {
		t.Fatalf("decay: %v %v %v", load1, load5, load15)
	}
	load1, load5, load15 = state.observe(4, start.Add(10*time.Minute))
	if load1 != 4 || load5 != 4 || load15 != 4 {
		t.Fatalf("reseed after gap: %v %v %v", load1, load5, load15)
	}
}

func TestWindowsGPUFieldsByDevice(t *testing.T) {
	gpus := []windowsGPUInfo{
		{device: `PCI\VEN_10DE&DEV_2783&SUBSYS_88881043&REV_A1\4&2D5A8E1F&0&0019`, model: "NVIDIA GeForce RTX 4070 SUPER"},
		{device: `PCI\VEN_8086&DEV_46A6&SUBSYS_88881043&REV_0C\4&2D5A8E1F&0&0008`, model: "Intel(R) UHD Graphics"},
	}
	adapters := []windowsGPUSensor{
		{luid: "0x00000000_0x0000FF01", deviceID: `PCI\VEN_10DE&DEV_2783&SUBSYS_88881043&REV_A1`, model: "NVIDIA GeForce RTX 4070 SUPER", temperature: 48.5, hasTemp: true},
		{luid: "0x00000000_0x0000FF02", deviceID: `PCI\VEN_8086&DEV_46A6&SUBSYS_88881043&REV_0C`, model: "Intel(R) UHD Graphics", temperature: 42, hasTemp: true},
	}
	usage := map[string]float64{"0x00000000_0x0000FF01": 30, "0x00000000_0x0000FF02": 5}
	fields := gpuFieldsByDevice(gpus, adapters, usage)
	if len(fields) != 2 || fields[gpus[0].device]["usage_percent"] != 30 || fields[gpus[0].device]["temperature_celsius"] != 48.5 ||
		fields[gpus[1].device]["usage_percent"] != 5 || fields[gpus[1].device]["temperature_celsius"] != 42 {
		t.Fatalf("multi-GPU fields: %v", fields)
	}
	fields = gpuFieldsByDevice(gpus[:1], []windowsGPUSensor{{luid: "0x1_0x1"}}, map[string]float64{"0x1_0x1": 7})
	if len(fields) != 1 || fields[gpus[0].device]["usage_percent"] != 7 {
		t.Fatalf("single-GPU fallback: %v", fields)
	}
	fields = gpuFieldsByDevice(gpus, adapters, nil)
	if len(fields) != 2 || len(fields[gpus[0].device]) != 1 || fields[gpus[0].device]["temperature_celsius"] != 48.5 ||
		len(fields[gpus[1].device]) != 1 || fields[gpus[1].device]["temperature_celsius"] != 42 {
		t.Fatalf("temperature-only fields: %v", fields)
	}
	ambiguous := gpuFieldsByDevice(append([]windowsGPUInfo{}, gpus[0], gpus[0]), adapters[:1], usage)
	if len(ambiguous) != 0 {
		t.Fatalf("ambiguous adapters attributed: %v", ambiguous)
	}
}

func TestWindowsGPUAdapterMatching(t *testing.T) {
	gpus := []windowsGPUInfo{
		{device: `PCI\VEN_10DE&DEV_2783&SUBSYS_88881043&REV_A1\4&2D5A8E1F&0&0019`, model: "NVIDIA GeForce RTX 4070 SUPER"},
		{device: `PCI\VEN_8086&DEV_46A6&SUBSYS_88881043&REV_0C\4&2D5A8E1F&0&0008`, model: "Intel(R) UHD Graphics"},
	}
	sensors := []windowsGPUSensor{
		{luid: "0x00000000_0x0000FF01", deviceID: `PCI\VEN_10DE&DEV_2783&SUBSYS_88881043&REV_A1`, model: "NVIDIA GeForce RTX 4070 SUPER", temperature: 48.5, hasTemp: true},
		{luid: "0x00000000_0x0000FF02", deviceID: `PCI\VEN_8086&DEV_46A6&SUBSYS_88881043&REV_0C`, model: "Intel(R) UHD Graphics", temperature: 42, hasTemp: true},
	}
	matched := matchGPUAdapters(gpus, sensors)
	if len(matched) != 2 || matched[gpus[0].device].temperature != 48.5 || matched[gpus[1].device].temperature != 42 {
		t.Fatalf("device matching: %v", matched)
	}
	if matched[gpus[0].device].luid != "0x00000000_0x0000FF01" {
		t.Fatalf("LUID lost: %v", matched[gpus[0].device])
	}
	if matched = matchGPUAdapters(gpus[:1], sensors); len(matched) != 1 || matched[gpus[0].device].temperature != 48.5 {
		t.Fatalf("prefix matching: %v", matched)
	}
	ambiguous := matchGPUAdapters(append([]windowsGPUInfo{}, gpus[0], gpus[0]), sensors)
	if len(ambiguous) != 0 {
		t.Fatalf("ambiguous adapters attributed: %v", ambiguous)
	}
	fallback := matchGPUAdapters([]windowsGPUInfo{{device: "gpu0", model: "Unknown"}}, []windowsGPUSensor{{luid: "0x1_0x1", temperature: 50, hasTemp: true}})
	if len(fallback) != 1 || fallback["gpu0"].temperature != 50 {
		t.Fatalf("single adapter fallback: %v", fallback)
	}
}

func waitPublicIP(t *testing.T, d *windowsCollector, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		d.mu.Lock()
		fetching, ip := d.publicIPFetching, d.publicIP
		d.mu.Unlock()
		if !fetching && ip == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("public IP fetch did not settle at %q", want)
}

func TestWindowsPublicIPCache(t *testing.T) {
	d := &windowsCollector{}
	var calls atomic.Int32
	d.fetchIP = func(context.Context) (string, error) {
		calls.Add(1)
		return "8.8.8.8", nil
	}
	if _, _, err := d.publicIPLocked(context.Background()); !errors.Is(err, errPublicIPPending) {
		t.Fatalf("first lookup must defer to the background fetch: %v", err)
	}
	waitPublicIP(t, d, "8.8.8.8")
	for n := 0; n < 3; n++ {
		ip, at, err := d.publicIPLocked(context.Background())
		if err != nil || ip != "8.8.8.8" || at.IsZero() {
			t.Fatalf("cached lookup: %q %v %v", ip, at, err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("public IP re-fetched %d times", calls.Load())
	}
	d.mu.Lock()
	d.publicIPAt = time.Now().Add(-6 * time.Minute)
	d.mu.Unlock()
	if _, _, err := d.publicIPLocked(context.Background()); !errors.Is(err, errPublicIPPending) {
		t.Fatalf("expired cache must refresh in the background: %v", err)
	}
	waitPublicIP(t, d, "8.8.8.8")
	if calls.Load() != 2 {
		t.Fatalf("expired cache refreshed %d times", calls.Load())
	}
	failing := &windowsCollector{fetchIP: func(context.Context) (string, error) { return "", errors.New("offline") }}
	if _, _, err := failing.publicIPLocked(context.Background()); !errors.Is(err, errPublicIPPending) {
		t.Fatalf("failed lookup must be deferred: %v", err)
	}
	waitPublicIP(t, failing, "")
	failing.mu.Lock()
	retained := failing.publicIP
	failing.mu.Unlock()
	if retained != "" {
		t.Fatal("failed lookup retained an address")
	}
}

func TestWindowsReasonMapping(t *testing.T) {
	for input, want := range map[string]string{
		"Access is denied. (0x80070005)": "permission_required",
		"Invalid class":                  "unavailable",
		"unexpected":                     "collect_failed",
	} {
		if got := windowsReason(errors.New(input)); got != want {
			t.Fatalf("%q = %q, want %q", input, got, want)
		}
	}
	if got := windowsReason(context.Canceled); got != "collect_timeout_or_canceled" {
		t.Fatalf("canceled = %q", got)
	}
}

func TestWindowsPublicIPv4Validation(t *testing.T) {
	if ip, err := parsePublicIPv4([]byte(`{"ipv4":"8.8.8.8"}`)); err != nil || ip != "8.8.8.8" {
		t.Fatalf("valid: %q %v", ip, err)
	}
	for _, value := range []string{`{"ipv4":"192.168.1.1"}`, `{"ipv4":"::1"}`, `broken`} {
		if _, err := parsePublicIPv4([]byte(value)); err == nil {
			t.Fatalf("accepted %s", value)
		}
	}
}

func TestWindowsNativeLiveScan(t *testing.T) {
	if testing.Short() {
		t.Skip("live WMI scan")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ids := []string{}
	for _, src := range nativeSources {
		if src.id == nativeSourceID("public_ip") {
			continue
		}
		ids = append(ids, src.id)
	}
	batch, reasons := newNativeCollector().Gather(ctx, ids)
	if ctx.Err() != nil {
		t.Skipf("WMI scan timed out: %v", ctx.Err())
	}
	if len(batch.Metrics) == 0 {
		t.Fatalf("no native metrics; reasons=%v", reasons)
	}
	for _, m := range batch.Metrics {
		if m.Timestamp <= 0 || m.Tags == nil || (len(m.Fields) == 0 && len(m.Exact) == 0) {
			t.Fatalf("invalid metric: %+v", m)
		}
		t.Logf("%s %v %v", m.Name, m.Tags, m.Fields)
	}
	for id, reason := range reasons {
		t.Logf("%s unavailable: %s", id, reason)
	}
}

func TestWindowsAdapterModelAliases(t *testing.T) {
	adapters := []windowsNetworkAdapter{
		{GUID: "{adapter-guid}", Name: "Intel Ethernet Controller", NetConnectionID: ptr("以太网 2"), PNPDeviceID: ptr(`PCI\VEN_8086`), PhysicalAdapter: ptr(true)},
		{Name: "Virtual adapter", NetConnectionID: ptr("VPN"), PNPDeviceID: ptr(`SWD\virtual`), PhysicalAdapter: ptr(false)},
	}
	models := windowsAdapterModels(adapters)
	names := windowsAdapterDisplayNames(adapters)
	if names[normalizeIfaceName("{adapter-guid}")] != "以太网 2" {
		t.Fatal("GUID was used instead of friendly connection name")
	}
	for _, name := range []string{"以太网 2", "{adapter-guid}", "Intel Ethernet Controller"} {
		if models[normalizeIfaceName(name)] != "Intel Ethernet Controller" {
			t.Fatalf("model not matched for %s", name)
		}
	}
	if models["vpn"] != "" {
		t.Fatal("virtual adapter model leaked into physical directory")
	}
}
