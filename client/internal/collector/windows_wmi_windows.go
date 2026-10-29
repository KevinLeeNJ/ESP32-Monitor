//go:build windows

package collector

import (
	"context"
	"errors"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/yusufpapurcu/wmi"
)

// Null WMI properties must stay nil so "unknown" is not read as zero.
var windowsWMI = &wmi.Client{PtrNil: true}

func windowsQuery(ctx context.Context, namespace, query string, dst any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- windowsWMI.Query(query, dst, nil, namespace) }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func windowsReason(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, os.ErrPermission) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return gatherReason(err)
	}
	text := strings.ToLower(err.Error())
	if strings.Contains(text, "access is denied") || strings.Contains(text, "access denied") || strings.Contains(text, "0x80070005") {
		return "permission_required"
	}
	if strings.Contains(text, "invalid class") || strings.Contains(text, "not supported") || strings.Contains(text, "not found") {
		return "unavailable"
	}
	return "collect_failed"
}

type windowsGPUInfo struct{ device, model string }

func windowsVideoControllers(ctx context.Context) ([]windowsGPUInfo, error) {
	var controllers []struct {
		Name        string
		PNPDeviceID *string
		Status      *string
	}
	if err := windowsQuery(ctx, `root\cimv2`, `SELECT Name,PNPDeviceID,Status FROM Win32_VideoController`, &controllers); err != nil {
		return nil, err
	}
	out := []windowsGPUInfo{}
	for _, controller := range controllers {
		if controller.Name == "" || controller.PNPDeviceID == nil {
			continue
		}
		pnp := *controller.PNPDeviceID
		upper := strings.ToUpper(pnp)
		if !strings.HasPrefix(upper, `PCI\`) && !strings.HasPrefix(upper, `USB\`) {
			continue
		}
		if controller.Status != nil && *controller.Status != "" && *controller.Status != "OK" {
			continue
		}
		out = append(out, windowsGPUInfo{device: pnp, model: controller.Name})
	}
	return out, nil
}

type windowsPhysicalDisk struct {
	device string
	model  string
	size   uint64
	health *uint16
}

func windowsPhysicalDisks(ctx context.Context) ([]windowsPhysicalDisk, error) {
	var disks []struct {
		DeviceId     string
		FriendlyName string
		Size         uint64
		HealthStatus *uint16
	}
	if err := windowsQuery(ctx, `root\Microsoft\Windows\Storage`, `SELECT DeviceId,FriendlyName,Size,HealthStatus FROM MSFT_PhysicalDisk`, &disks); err != nil {
		return nil, err
	}
	out := []windowsPhysicalDisk{}
	for _, disk := range disks {
		if disk.DeviceId == "" {
			continue
		}
		out = append(out, windowsPhysicalDisk{device: "PHYSICALDRIVE" + disk.DeviceId, model: strings.TrimSpace(disk.FriendlyName), size: disk.Size, health: disk.HealthStatus})
	}
	return out, nil
}

// MSFT_PhysicalDisk HealthStatus: 0 healthy, 1 warning, 2 unhealthy, 5 unknown.
func diskHealthValue(status *uint16) (float64, bool) {
	if status == nil {
		return 0, false
	}
	switch *status {
	case 0:
		return 1, true
	case 1, 2:
		return 0, true
	}
	return 0, false
}

type windowsPartition struct {
	disk   uint32
	letter string
}

func windowsPartitions(ctx context.Context) ([]windowsPartition, error) {
	var partitions []struct {
		DiskNumber  uint32
		DriveLetter *uint16
	}
	if err := windowsQuery(ctx, `root\Microsoft\Windows\Storage`, `SELECT DiskNumber,DriveLetter FROM MSFT_Partition`, &partitions); err != nil {
		return nil, err
	}
	out := []windowsPartition{}
	for _, partition := range partitions {
		letter := ""
		if partition.DriveLetter != nil && *partition.DriveLetter >= 'A' && *partition.DriveLetter <= 'Z' {
			letter = string(rune(*partition.DriveLetter))
		}
		out = append(out, windowsPartition{disk: partition.DiskNumber, letter: letter})
	}
	return out, nil
}

type windowsLogicalDisk struct {
	letter    string
	driveType uint32
	size      uint64
	free      uint64
}

func windowsLogicalDisks(ctx context.Context) ([]windowsLogicalDisk, error) {
	var disks []struct {
		DeviceID  string
		DriveType uint32
		Size      *uint64
		FreeSpace *uint64
	}
	if err := windowsQuery(ctx, `root\cimv2`, `SELECT DeviceID,DriveType,Size,FreeSpace FROM Win32_LogicalDisk`, &disks); err != nil {
		return nil, err
	}
	out := []windowsLogicalDisk{}
	for _, disk := range disks {
		if len(disk.DeviceID) < 2 || disk.Size == nil || disk.FreeSpace == nil {
			continue
		}
		out = append(out, windowsLogicalDisk{letter: disk.DeviceID[:1], driveType: disk.DriveType, size: *disk.Size, free: *disk.FreeSpace})
	}
	return out, nil
}

const windowsDriveFixed = 3

// windowsDiskUsage attributes used bytes to a physical disk only when a drive
// letter maps to exactly one partition. Ambiguous or unmapped volumes are
// omitted rather than guessed.
func windowsDiskUsage(partitions []windowsPartition, volumes []windowsLogicalDisk) map[string]uint64 {
	disks := map[string]uint32{}
	ambiguous := map[string]bool{}
	for _, partition := range partitions {
		if partition.letter == "" {
			continue
		}
		key := strings.ToUpper(partition.letter)
		if _, exists := disks[key]; exists {
			ambiguous[key] = true
			continue
		}
		disks[key] = partition.disk
	}
	used := map[uint32]uint64{}
	for _, volume := range volumes {
		if volume.driveType != windowsDriveFixed || volume.size == 0 || volume.free > volume.size {
			continue
		}
		key := strings.ToUpper(volume.letter)
		if ambiguous[key] {
			continue
		}
		disk, ok := disks[key]
		if !ok {
			continue
		}
		used[disk] += volume.size - volume.free
	}
	out := map[string]uint64{}
	for disk, value := range used {
		out["PHYSICALDRIVE"+strconv.FormatUint(uint64(disk), 10)] = value
	}
	return out
}

type windowsNetworkAdapter struct {
	GUID            string
	Name            string
	NetConnectionID *string
	PNPDeviceID     *string
	PhysicalAdapter *bool
}

func windowsNetworkAdapters(ctx context.Context) ([]windowsNetworkAdapter, error) {
	var adapters []windowsNetworkAdapter
	if err := windowsQuery(ctx, `root\cimv2`, `SELECT GUID,Name,NetConnectionID,PNPDeviceID,PhysicalAdapter FROM Win32_NetworkAdapter`, &adapters); err != nil {
		return nil, err
	}
	return adapters, nil
}

func normalizeIfaceName(name string) string {
	return strings.ToLower(strings.Trim(strings.TrimSpace(name), "{}"))
}

func adapterMedium(name string) (string, bool) {
	lower := strings.ToLower(name)
	for _, keyword := range []string{"wi-fi", "wifi", "wireless", "wlan", "无线"} {
		if strings.Contains(lower, keyword) {
			return "wifi", true
		}
	}
	for _, keyword := range []string{"ethernet", "gbe", "gigabit", " lan", "lan ", "以太网", "イーサネット"} {
		if strings.Contains(lower, keyword) {
			return "lan", true
		}
	}
	return "", false
}

// windowsAdapterKinds keys adapters by GUID, connection name and device name so
// Go's net.Interface friendly name can be matched regardless of Windows locale.
func windowsAdapterKinds(adapters []windowsNetworkAdapter) map[string]string {
	out := map[string]string{}
	for _, adapter := range adapters {
		if adapter.PhysicalAdapter != nil && !*adapter.PhysicalAdapter {
			continue
		}
		pnp := ""
		if adapter.PNPDeviceID != nil {
			pnp = strings.ToUpper(*adapter.PNPDeviceID)
		}
		if !strings.HasPrefix(pnp, `PCI\`) && !strings.HasPrefix(pnp, `USB\`) {
			continue
		}
		connection := ""
		if adapter.NetConnectionID != nil {
			connection = *adapter.NetConnectionID
		}
		medium, ok := adapterMedium(adapter.Name + " " + connection)
		if !ok {
			medium = "other"
		}
		for _, key := range []string{adapter.GUID, connection, adapter.Name} {
			if key != "" {
				out[normalizeIfaceName(key)] = medium
			}
		}
	}
	return out
}

func adapterKindFor(kinds map[string]string, name string) (string, bool) {
	kind, ok := kinds[normalizeIfaceName(name)]
	return kind, ok
}

type windowsGPUEngine struct {
	Name                  string
	UtilizationPercentage uint64
}

func windowsGPUUsage(ctx context.Context) (map[string]float64, error) {
	var counters []windowsGPUEngine
	if err := windowsQuery(ctx, `root\cimv2`, `SELECT Name,UtilizationPercentage FROM Win32_PerfFormattedData_GPUPerformanceCounters_GPUEngine`, &counters); err != nil {
		return nil, err
	}
	return gpuUsageByLUID(counters), nil
}

var gpuEnginePattern = regexp.MustCompile(`luid_(0x[0-9a-fA-F]+_0x[0-9a-fA-F]+)_phys_(\d+)_eng_(\d+)_engtype_`)

// GPU Engine counters are per process and per engine. Sum each engine's
// processes, then report the busiest engine per adapter as GPU utilization.
func gpuUsageByLUID(counters []windowsGPUEngine) map[string]float64 {
	engineSums := map[string]float64{}
	engineLUID := map[string]string{}
	for _, counter := range counters {
		match := gpuEnginePattern.FindStringSubmatch(counter.Name)
		if len(match) != 4 {
			continue
		}
		key := match[1] + "/" + match[2] + "/" + match[3]
		engineSums[key] += min(100, float64(counter.UtilizationPercentage))
		engineLUID[key] = match[1]
	}
	out := map[string]float64{}
	for key, sum := range engineSums {
		luid := engineLUID[key]
		if value := min(100, sum); value > out[luid] {
			out[luid] = value
		}
	}
	return out
}
