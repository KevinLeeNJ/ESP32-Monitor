//go:build darwin

package collector

import (
	"context"
	"encoding/json"
	"testing"
)

func TestDarwinMemoryAndBatteryParsing(t *testing.T) {
	vm := `Mach Virtual Memory Statistics: (page size of 4096 bytes)
Pages active: 100.
Pages inactive: 50.
Pages speculative: 10.
Pages wired down: 20.
Pages occupied by compressor: 30.
Pages purgeable: 5.
File-backed pages: 15.`
	f := parseVMStat(vm)
	if f["app_bytes"] != 140*4096 || f["wired_bytes"] != 20*4096 || f["compressed_bytes"] != 30*4096 {
		t.Fatalf("memory: %v", f)
	}
	if len(parseVMStat("invalid")) != 0 {
		t.Fatal("invalid memory generated data")
	}
	f = parseBattery("Now drawing from 'AC Power'\n -InternalBattery-0 (id=123)\t85%; discharging; 6:55 remaining present: true")
	if f["charge_percent"] != 85 || f["charging"] != 0 || len(f) != 2 {
		t.Fatalf("battery: %v", f)
	}
	if len(parseBattery("Now drawing from 'AC Power'")) != 0 {
		t.Fatal("battery invented on desktop")
	}
}

func TestNetworkPortClassification(t *testing.T) {
	p := parseNetworkPorts("Hardware Port: Wi-Fi\nDevice: en0\n\nHardware Port: Thunderbolt Ethernet\nDevice: en8\n")
	if p["en0"] != "wifi" || p["en8"] != "lan" {
		t.Fatalf("ports: %v", p)
	}
}

func TestDarwinLinkStatus(t *testing.T) {
	for _, tc := range []struct {
		input string
		value float64
		ok    bool
	}{
		{"flags=UP,RUNNING\n\tstatus: active\n", 1, true},
		{"flags=UP,RUNNING\n\tmedia: none\n\tstatus: inactive\n", 0, true},
		{"flags=UP,RUNNING\n", 0, false},
		{"status: unknown\n", 0, false},
	} {
		value, ok := parseDarwinLinkStatus(tc.input)
		if value != tc.value || ok != tc.ok {
			t.Fatalf("%q: got %v %v", tc.input, value, ok)
		}
	}
}

func TestPlistGPUAndDiskParsing(t *testing.T) {
	input := `<?xml version="1.0"?><plist version="1.0"><array><dict><key>IOClass</key><string>AGX</string><key>PerformanceStatistics</key><dict><key>Device Utilization %</key><integer>37</integer><key>other</key><data>AA==</data></dict></dict></array></plist>`
	v, err := decodePlist(input)
	if err != nil {
		t.Fatal(err)
	}
	a, ok := v.([]any)
	if !ok || len(a) != 1 {
		t.Fatalf("plist: %#v", v)
	}
	d := a[0].(map[string]any)
	stats := d["PerformanceStatistics"].(map[string]any)
	if stats["Device Utilization %"] != float64(37) {
		t.Fatal(stats)
	}
	if _, err := decodePlist(`<plist><dict><key>a</key><integer>broken</integer></dict></plist>`); err == nil {
		t.Fatal("invalid plist accepted")
	}
}

func TestParsePublicIPv4(t *testing.T) {
	if ip, err := parsePublicIPv4([]byte(`{"ipv4":"8.8.8.8","country":"US"}`)); err != nil || ip != "8.8.8.8" {
		t.Fatalf("valid: %q %v", ip, err)
	}
	if ip, err := parsePublicIPv4([]byte(`{"ip":"8.8.4.4"}`)); err != nil || ip != "8.8.4.4" {
		t.Fatalf("fallback: %q %v", ip, err)
	}
	for _, value := range []string{`{"ipv4":"192.168.1.1"}`, `{"ipv4":"::1"}`, `{"ipv6":"::1"}`, `broken`} {
		if _, err := parsePublicIPv4([]byte(value)); err == nil {
			t.Fatalf("accepted %s", value)
		}
	}
}

func TestMacProductName(t *testing.T) {
	for model, want := range map[string]string{
		"Mac16,12":       "MacBook Air (13-inch, M4, 2025)",
		"Mac14,2":        "MacBook Air (M2, 2022)",
		"MacBookPro18,3": "MacBook Pro (14-inch, 2021)",
		"Macmini9,1":     "Mac mini (M1, 2020)",
	} {
		if got := macProductName(model); got != want {
			t.Fatalf("%s: %q != %q", model, got, want)
		}
	}
	if macProductName("Mac99,9") != "" {
		t.Fatal("unknown model invented a name")
	}
}

func TestDarwinOSCodename(t *testing.T) {
	for version, want := range map[string]string{"27.2": "Golden Gate", "26.0": "Tahoe", "14.5": "Sonoma", "10.15": ""} {
		if got := darwinOSCodename(version); got != want {
			t.Fatalf("%s: %q != %q", version, got, want)
		}
	}
	if darwinOSCodename("broken") != "" || darwinOSCodename("") != "" {
		t.Fatal("invalid version invented a codename")
	}
}

func TestParseSwVers(t *testing.T) {
	name, version, build := parseSwVers("ProductName:\t\tmacOS\nProductVersion:\t\t27.2\nBuildVersion:\t\t26B118\n")
	if name != "macOS" || version != "27.2" || build != "26B118" {
		t.Fatalf("sw_vers: %q %q %q", name, version, build)
	}
	name, version, build = parseSwVers("")
	if name != "" || version != "" || build != "" {
		t.Fatal("empty output invented values")
	}
}

func TestDarwinFixedInventorySurvivesForks(t *testing.T) {
	fixed := &darwinFixed{}
	fixed.once.Do(func() {
		fixed.brand = "cached-cpu"
		fixed.batch = Batch{Metrics: []Metric{
			{Name: "darwin_cpu", Tags: map[string]string{"model": "cached-cpu"}, Fields: map[string]float64{"physical_cores": 123, "logical_cores": 456}},
			{Name: "darwin_system", Tags: map[string]string{"product_name": "cached-product", "os_name": "cached-os"}, Fields: map[string]float64{"present": 1}},
		}}
	})
	base := &darwinCollector{fixed: fixed}
	for n := 0; n < 3; n++ {
		fork := base.Fork().(*darwinCollector)
		batch, _ := fork.Gather(context.Background(), []string{"darwin_cpu", "darwin_system"})
		if got := fixedMetric(batch, "darwin_cpu", "", ""); got.Tags["model"] != "cached-cpu" || got.Fields["physical_cores"] != 123 {
			t.Fatalf("CPU metadata re-probed: %+v", got)
		}
		if got := fixedMetric(batch, "darwin_system", "", ""); got.Tags["product_name"] != "cached-product" {
			t.Fatalf("system metadata re-probed: %+v", got)
		}
	}
}

func TestPlistLargeSMARTIntegerRemainsExact(t *testing.T) {
	value, err := decodePlist(`<plist><dict><key>counter</key><integer>340282366920938463463374607431768211455</integer></dict></plist>`)
	if err != nil {
		t.Fatal(err)
	}
	n, ok := value.(map[string]any)["counter"].(json.Number)
	if !ok || n.String() != "340282366920938463463374607431768211455" {
		t.Fatalf("rounded SMART counter: %#v", value)
	}
}

func TestPhysicalAPFSDiskUsage(t *testing.T) {
	disk := func(name string, parts ...any) any {
		return map[string]any{"DeviceIdentifier": name, "Partitions": parts}
	}
	part := func(name, kind string) any { return map[string]any{"DeviceIdentifier": name, "Content": kind} }
	store := func(name string) any { return map[string]any{"DeviceIdentifier": name} }
	container := func(total, free any, stores ...any) any {
		return map[string]any{"CapacityCeiling": total, "CapacityFree": free, "PhysicalStores": stores}
	}
	physical := map[string]any{"AllDisksAndPartitions": []any{
		disk("disk0", part("disk0s1", "Apple_APFS_ISC"), part("disk0s2", "Apple_APFS")),
		disk("disk1", part("disk1s1", "Apple_APFS")),
		disk("disk2", part("disk2s1", "Apple_APFS"), part("disk2s2", "Microsoft Basic Data")),
		disk("disk3", part("disk3s1", "Apple_APFS")),
	}}
	partitions := physicalAPFSPartitions(physical)
	apfs := map[string]any{"Containers": []any{
		container(float64(100), float64(20), store("disk0s1")), container(float64(400), float64(100), store("disk0s2")),
		container(float64(500), float64(250), store("disk1s1"), store("disk3s1")),
		container(float64(50), float64(60), store("disk2s1")),
	}}
	used := apfsDiskUsage(apfs, partitions)
	if len(used) != 1 || used["disk0"] != 380 {
		t.Fatalf("shared or invalid data accepted: %#v", used)
	}
	apfs["Containers"] = append(apfs["Containers"].([]any), container(float64(100), float64(50), store("disk0s1")))
	if _, ok := apfsDiskUsage(apfs, partitions)["disk0"]; ok {
		t.Fatal("duplicate physical store accepted")
	}
}
