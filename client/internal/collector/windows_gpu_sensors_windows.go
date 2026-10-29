//go:build windows

package collector

import (
	"context"
	"fmt"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The WDDM adapter sensor query mirrors prometheus-community/windows_exporter's
// gdi32 GPU sensor implementation. It reads the temperature the display driver
// reports through D3DKMT, so NVIDIA, AMD and Intel adapters all use their native
// kernel-mode driver path without vendor SDKs.
// See client/third_party/windows_exporter/LICENSE.

type d3dkmtHandle = uint32

const (
	kmtQueryAdapterRegistryInfo      = 8
	kmtQueryAdapterType              = 15
	kmtQueryPhysicalAdapterDeviceIDs = 31
	kmtQueryAdapterPerfData          = 62

	d3dkmtSoftwareDevice = 1 << 2
)

var (
	modGdi32D3DKMT             = windows.NewLazySystemDLL("gdi32.dll")
	procD3DKMTEnumAdapters2    = modGdi32D3DKMT.NewProc("D3DKMTEnumAdapters2")
	procD3DKMTQueryAdapterInfo = modGdi32D3DKMT.NewProc("D3DKMTQueryAdapterInfo")
	procD3DKMTCloseAdapter     = modGdi32D3DKMT.NewProc("D3DKMTCloseAdapter")
)

type d3dkmtEnumAdapters2 struct {
	NumAdapters uint32
	PAdapters   *d3dkmtAdapterInfo
}

type d3dkmtAdapterInfo struct {
	HAdapter     d3dkmtHandle
	AdapterLUID  windows.LUID
	NumOfSources uint32
	Present      int32
}

type d3dkmtQueryAdapterInfo struct {
	hAdapter              d3dkmtHandle
	queryType             int32
	pPrivateDriverData    unsafe.Pointer
	privateDriverDataSize uint32
}

type d3dkmtCloseAdapter struct {
	HAdapter d3dkmtHandle
}

type d3dkmtAdapterRegistryInfo struct {
	AdapterString [260]uint16
	BiosString    [260]uint16
	DacType       [260]uint16
	ChipType      [260]uint16
}

type d3dkmtQueryDeviceIDs struct {
	PhysicalAdapterIndex uint32
	DeviceIDs            struct {
		VendorID    uint32
		DeviceID    uint32
		SubVendorID uint32
		SubSystemID uint32
		RevisionID  uint32
		BusType     uint32
	}
}

// https://learn.microsoft.com/en-us/windows-hardware/drivers/ddi/d3dkmthk/ns-d3dkmthk-_d3dkmt_adapter_perfdata
type d3dkmtAdapterPerfData struct {
	PhysicalAdapterIndex uint32
	MemoryFrequency      uint64
	MaxMemoryFrequency   uint64
	MaxMemoryFrequencyOC uint64
	MemoryBandwidth      uint64
	PCIEBandwidth        uint64
	FanRPM               uint32
	Power                uint32
	// Temperature is in tenths of a degree Celsius.
	Temperature        uint32
	PowerStateOverride uint8
}

type windowsGPUSensor struct {
	luid        string
	deviceID    string
	model       string
	temperature float64
	hasTemp     bool
}

func d3dkmtCall(proc *windows.LazyProc, name string, data unsafe.Pointer) error {
	ret, _, _ := proc.Call(uintptr(data))
	if ret != 0 {
		return fmt.Errorf("%s failed: 0x%X", name, ret)
	}
	return nil
}

func d3dkmtQuery(handle d3dkmtHandle, queryType int32, data unsafe.Pointer, size uintptr) error {
	query := d3dkmtQueryAdapterInfo{
		hAdapter:              handle,
		queryType:             queryType,
		pPrivateDriverData:    data,
		privateDriverDataSize: uint32(size),
	}
	return d3dkmtCall(procD3DKMTQueryAdapterInfo, "D3DKMTQueryAdapterInfo", unsafe.Pointer(&query))
}

func d3dkmtClose(handle d3dkmtHandle) {
	closeAdapter := d3dkmtCloseAdapter{HAdapter: handle}
	_ = d3dkmtCall(procD3DKMTCloseAdapter, "D3DKMTCloseAdapter", unsafe.Pointer(&closeAdapter))
}

// windowsGPUAdapters enumerates WDDM adapters and returns the stable identity
// and driver-reported temperature of each non-software adapter. Adapters that
// reject the perf data query are still returned so usage counters can be
// attributed by LUID.
func windowsGPUAdapters(ctx context.Context) ([]windowsGPUSensor, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var count d3dkmtEnumAdapters2
	if err := d3dkmtCall(procD3DKMTEnumAdapters2, "D3DKMTEnumAdapters2", unsafe.Pointer(&count)); err != nil {
		return nil, err
	}
	if count.NumAdapters == 0 {
		return nil, nil
	}
	adapters := make([]d3dkmtAdapterInfo, count.NumAdapters)
	request := d3dkmtEnumAdapters2{NumAdapters: count.NumAdapters, PAdapters: &adapters[0]}
	if err := d3dkmtCall(procD3DKMTEnumAdapters2, "D3DKMTEnumAdapters2", unsafe.Pointer(&request)); err != nil {
		return nil, err
	}
	out := []windowsGPUSensor{}
	for i := uint32(0); i < request.NumAdapters && i < uint32(len(adapters)); i++ {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		handle := adapters[i].HAdapter
		if handle == 0 {
			continue
		}
		if sensor, ok := d3dkmtAdapter(handle, adapters[i].AdapterLUID); ok {
			out = append(out, sensor)
		}
		d3dkmtClose(handle)
	}
	return out, nil
}

func d3dkmtAdapter(handle d3dkmtHandle, luid windows.LUID) (windowsGPUSensor, bool) {
	var adapterType uint32
	if err := d3dkmtQuery(handle, kmtQueryAdapterType, unsafe.Pointer(&adapterType), unsafe.Sizeof(adapterType)); err != nil {
		return windowsGPUSensor{}, false
	}
	if adapterType&d3dkmtSoftwareDevice != 0 {
		return windowsGPUSensor{}, false
	}

	sensor := windowsGPUSensor{luid: fmt.Sprintf("0x%08X_0x%08X", luid.HighPart, luid.LowPart)}
	var registry d3dkmtAdapterRegistryInfo
	if err := d3dkmtQuery(handle, kmtQueryAdapterRegistryInfo, unsafe.Pointer(&registry), unsafe.Sizeof(registry)); err == nil {
		sensor.model = windows.UTF16ToString(registry.AdapterString[:])
	}
	var ids d3dkmtQueryDeviceIDs
	if err := d3dkmtQuery(handle, kmtQueryPhysicalAdapterDeviceIDs, unsafe.Pointer(&ids), unsafe.Sizeof(ids)); err == nil {
		sensor.deviceID = fmt.Sprintf("PCI\\VEN_%04X&DEV_%04X&SUBSYS_%04X%04X&REV_%02X",
			uint16(ids.DeviceIDs.VendorID),
			uint16(ids.DeviceIDs.DeviceID),
			uint16(ids.DeviceIDs.SubSystemID),
			uint16(ids.DeviceIDs.SubVendorID),
			uint8(ids.DeviceIDs.RevisionID),
		)
	}
	if sensor.model == "" || strings.HasPrefix(sensor.deviceID, `PCI\VEN_1414&`) {
		// Software and virtual display adapters have no sensor.
		return windowsGPUSensor{}, false
	}

	var perf d3dkmtAdapterPerfData
	if err := d3dkmtQuery(handle, kmtQueryAdapterPerfData, unsafe.Pointer(&perf), unsafe.Sizeof(perf)); err != nil {
		return sensor, true
	}
	value := float64(perf.Temperature) / 10
	if value > 0 && value < 150 {
		sensor.temperature, sensor.hasTemp = value, true
	}
	return sensor, true
}

// matchGPUAdapters maps D3DKMT adapters onto WMI video controllers. Only
// unambiguous matches are used: a PNP device-ID prefix or exact model must
// identify exactly one adapter and one controller.
func matchGPUAdapters(gpus []windowsGPUInfo, sensors []windowsGPUSensor) map[string]windowsGPUSensor {
	out := map[string]windowsGPUSensor{}
	candidates := make([][]int, len(gpus))
	claims := make([]int, len(sensors))
	for i, gpu := range gpus {
		for j, sensor := range sensors {
			if sensor.deviceID != "" && strings.HasPrefix(strings.ToUpper(gpu.device), strings.ToUpper(sensor.deviceID)) {
				candidates[i] = append(candidates[i], j)
				claims[j]++
				continue
			}
			if sensor.model != "" && strings.EqualFold(sensor.model, gpu.model) {
				candidates[i] = append(candidates[i], j)
				claims[j]++
			}
		}
	}
	for i, gpu := range gpus {
		if len(candidates[i]) == 1 && claims[candidates[i][0]] == 1 {
			out[gpu.device] = sensors[candidates[i][0]]
		}
	}
	if len(gpus) == 1 && len(out) == 0 && len(sensors) == 1 {
		out[gpus[0].device] = sensors[0]
	}
	return out
}

// gpuFieldsByDevice assigns usage counters by LUID and temperatures by device
// identity. The single-GPU fallback keeps D3DKMT failures from losing counters
// that are unambiguous by themselves.
func gpuFieldsByDevice(gpus []windowsGPUInfo, adapters []windowsGPUSensor, usage map[string]float64) map[string]map[string]float64 {
	out := map[string]map[string]float64{}
	for device, adapter := range matchGPUAdapters(gpus, adapters) {
		fields := map[string]float64{}
		if value, ok := usage[adapter.luid]; ok {
			fields["usage_percent"] = value
		}
		if adapter.hasTemp {
			fields["temperature_celsius"] = adapter.temperature
		}
		if len(fields) > 0 {
			out[device] = fields
		}
	}
	assignedUsage := false
	for _, fields := range out {
		if _, ok := fields["usage_percent"]; ok {
			assignedUsage = true
		}
	}
	if !assignedUsage && len(gpus) == 1 && len(usage) == 1 {
		value := 0.0
		for _, observed := range usage {
			value = observed
		}
		if out[gpus[0].device] == nil {
			out[gpus[0].device] = map[string]float64{}
		}
		out[gpus[0].device]["usage_percent"] = value
	}
	return out
}
