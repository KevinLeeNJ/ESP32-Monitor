package collector

import (
	"errors"
	v2 "esp32-monitor/client/internal/collector/uploadv2"
	"math"
	"net"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"
)

func scaled(m Metric, field string, scale, low, high float64) (int64, error) {
	v, ok := m.Fields[field]
	if !ok {
		return 0, errors.New("v2_required_metric_missing")
	}
	if math.IsNaN(v) || math.IsInf(v, 0) || v < low || v > high {
		return 0, errors.New("v2_metric_out_of_range")
	}
	return int64(math.Round(v * scale)), nil
}
func findSample(b Batch, source, field string) (Metric, bool) {
	for _, m := range b.Metrics {
		if m.Name == source {
			if source == "cpu" && m.Tags["cpu"] != "cpu-total" {
				continue
			}
			if _, ok := m.Fields[field]; ok {
				return m, true
			}
		}
	}
	return Metric{}, false
}
func requiredV2Sample(b Batch) (v2.Sample, error) {
	out := v2.Sample{Timestamp: time.Now().Unix()}
	for _, key := range []string{"cpu.usage", "memory.usage", "system.uptime"} {
		binding := v2Bindings[key]
		v, ok := findSample(b, binding.source, binding.field)
		if !ok {
			return out, errors.New("v2_required_metric_missing")
		}
		if v.Timestamp < out.Timestamp-3 || v.Timestamp > out.Timestamp {
			return out, errors.New("v2_required_metric_stale")
		}
		scale, hi := float64(100), float64(100)
		if key == "system.uptime" {
			scale = 1
			hi = math.MaxUint32
		}
		n, e := scaled(v, binding.field, scale, 0, hi)
		if e != nil {
			return out, e
		}
		switch key {
		case "cpu.usage":
			out.CPUUsage = ptr(uint16(n))
		case "memory.usage":
			out.MemoryUsage = ptr(uint16(n))
		case "system.uptime":
			out.Uptime = ptr(uint32(n))
		}
	}
	return out, nil
}
func (m *Manager) sampleV2(c Config, b Batch) (v2.Sample, error) {
	out, err := requiredV2Sample(b)
	if err != nil {
		return out, err
	}
	selections := append([]v2.UploadSelection{{Metric: "memory.used"}}, c.Upload.Optional...)
	for _, sel := range selections {
		binding, ok := v2Bindings[sel.Metric]
		if !ok {
			continue
		}
		for _, v := range b.Metrics {
			if v.Name != binding.source {
				continue
			}
			id := m.v2.object(binding.kind, v)
			if binding.kind != "" && !m.v2.objectsOfKind(binding.kind)[id] {
				continue
			}
			if binding.kind != "" && !slices.Contains(sel.Objects, id) {
				continue
			}
			if binding.kind == "" && binding.source == "cpu" && v.Tags["cpu"] != "cpu-total" {
				continue
			}
			if sel.Metric == "drive.smart_health" || sel.Metric == "network.public_ipv4" {
				if v.Timestamp < out.Timestamp-900 || v.Timestamp > out.Timestamp {
					continue
				}
			} else if v.Timestamp < out.Timestamp-3 || v.Timestamp > out.Timestamp {
				continue
			}
			m.fillV2Sample(&out, sel.Metric, v, id)
		}
	}
	finalizeV2Sample(&out, func(key string, id uint16) bool { return v2ObjectSelected(c, key, id) })
	return out, nil
}

// previewSampleV2 fills every metric present in the batch so the GUI can show
// live values for metrics the user has not selected. Uploaded samples are built
// by sampleV2 and stay restricted to the user's choices; the public IPv4 source
// is cached by the platform collector, so previewing it does not query per tick.
func (m *Manager) previewSampleV2(b Batch) (v2.Sample, error) {
	out, err := requiredV2Sample(b)
	if err != nil {
		return out, err
	}
	for key, binding := range v2Bindings {
		if key == "cpu.usage" || key == "memory.usage" || key == "system.uptime" {
			continue
		}
		for _, v := range b.Metrics {
			if v.Name != binding.source {
				continue
			}
			if binding.kind == "" && binding.source == "cpu" && v.Tags["cpu"] != "cpu-total" {
				continue
			}
			window := int64(3)
			if key == "drive.smart_health" || key == "network.public_ipv4" {
				window = 900
			}
			if v.Timestamp < out.Timestamp-window || v.Timestamp > out.Timestamp {
				continue
			}
			id := uint16(0)
			if binding.kind != "" {
				id = m.v2.object(binding.kind, v)
				if id == 0 || !m.v2.objectsOfKind(binding.kind)[id] {
					continue
				}
			}
			m.fillV2Sample(&out, key, v, id)
		}
	}
	finalizeV2Sample(&out, func(string, uint16) bool { return true })
	sortSampleObjects(&out)
	return out, nil
}

// fillV2Sample converts one batch metric into the sample section for key.
func (m *Manager) fillV2Sample(out *v2.Sample, key string, v Metric, id uint16) {
	binding := v2Bindings[key]
	value, exists := v.Fields[binding.field]
	if key == "interface.ipv4" {
		if present, ok := v.Fields["present"]; ok {
			value, exists = present, true
		}
	}
	if _, ok := v.Exact[binding.field]; ok {
		exists = true
	}
	if !exists {
		return
	}
	scale, lo, hi := float64(1), float64(0), float64(9007199254740991)
	switch key {
	case "core.usage", "gpu.usage", "battery.level":
		scale, hi = 100, 100
	case "cpu.temperature", "gpu.temperature":
		scale, lo, hi = 10, -273.1, 3276.7
	case "cpu.load1", "cpu.load5", "cpu.load15":
		scale, hi = 1000, float64(math.MaxUint32)/1000
	case "memory.pressure":
		hi = 4
	case "battery.charging", "interface.up", "drive.smart_health":
		hi = 1
		if value != 0 && value != 1 {
			return
		}
	}
	var n int64
	var unsigned uint64
	if byteMetric(key) {
		if raw, ok := v.Exact[binding.field]; ok {
			parsed, e := strconv.ParseUint(raw, 10, 64)
			if e != nil {
				return
			}
			unsigned = parsed
		} else {
			parsed, e := scaled(v, binding.field, 1, 0, 9007199254740991)
			if e != nil {
				return
			}
			unsigned = uint64(parsed)
		}
	} else {
		var e error
		n, e = scaled(v, binding.field, scale, lo, hi)
		if e != nil {
			return
		}
	}
	switch key {
	case "cpu.load1", "cpu.load5", "cpu.load15", "cpu.temperature":
		if out.CPU == nil {
			out.CPU = &v2.CPUSample{}
		}
		switch key {
		case "cpu.load1":
			out.CPU.Load1 = ptr(uint32(n))
		case "cpu.load5":
			out.CPU.Load5 = ptr(uint32(n))
		case "cpu.load15":
			out.CPU.Load15 = ptr(uint32(n))
		case "cpu.temperature":
			out.CPU.Temperature = ptr(int16(n))
		}
	case "memory.used", "memory.app", "memory.wired", "memory.compressed", "memory.swap_total", "memory.swap_used", "memory.pressure":
		if out.Memory == nil {
			out.Memory = &v2.MemorySample{}
		}
		switch key {
		case "memory.used":
			out.Memory.Used = ptr(unsigned)
		case "memory.app":
			out.Memory.App = ptr(unsigned)
		case "memory.wired":
			out.Memory.Wired = ptr(unsigned)
		case "memory.compressed":
			out.Memory.Compressed = ptr(unsigned)
		case "memory.swap_total":
			out.Memory.SwapTotal = ptr(unsigned)
		case "memory.swap_used":
			out.Memory.SwapUsed = ptr(unsigned)
		case "memory.pressure":
			pressure := uint8(0)
			switch value {
			case 1:
				pressure = 0
			case 2:
				pressure = 1
			case 4:
				pressure = 2
			default:
				return
			}
			out.Memory.Pressure = &pressure
		}
	case "core.usage":
		out.Cores = append(out.Cores, v2.CoreSample{ID: id, Usage: ptr(uint16(n))})
	case "gpu.usage", "gpu.temperature":
		dst := sampleGPU(out, id)
		if key == "gpu.usage" {
			dst.Usage = ptr(uint16(n))
		} else {
			dst.Temperature = ptr(int16(n))
		}
	case "drive.read_rate":
		sampleDrive(out, id).ReadRate = ptr(unsigned)
	case "drive.write_rate":
		sampleDrive(out, id).WriteRate = ptr(unsigned)
	case "drive.used":
		sampleDrive(out, id).Used = ptr(unsigned)
	case "drive.smart_health":
		dst := sampleDrive(out, id)
		if dst.SMART == nil {
			dst.SMART = &v2.SMARTSample{Timestamp: v.Timestamp}
		}
		dst.SMART.Healthy = ptr(n == 1)
	case "interface.upload_rate":
		sampleInterface(out, id).UploadRate = ptr(unsigned)
	case "interface.download_rate":
		sampleInterface(out, id).DownloadRate = ptr(unsigned)
	case "interface.up":
		sampleInterface(out, id).Up = ptr(n == 1)
	case "interface.ipv4":
		ips := []string{}
		for _, ip := range strings.Split(v.Tags["ipv4"], ",") {
			if parsed := net.ParseIP(ip); parsed != nil && parsed.To4() != nil {
				ips = append(ips, parsed.To4().String())
			}
		}
		slices.Sort(ips)
		ips = slices.Compact(ips)
		sampleInterface(out, id).IPv4 = &ips
	case "network.public_ipv4":
		ip := net.ParseIP(v.Tags["ipv4"])
		if ip != nil && ip.To4() != nil && ip.IsGlobalUnicast() && !ip.IsPrivate() {
			out.PublicIPv4 = &v2.IPv4Observation{Timestamp: v.Timestamp, Address: ip.To4().String()}
		}
	case "battery.level", "battery.charging":
		dst := sampleBattery(out, id)
		if key == "battery.level" {
			dst.Level = ptr(uint16(n))
		} else {
			dst.Charging = ptr(n == 1)
		}
	}
}

// finalizeV2Sample applies the pairing and emptiness rules shared by uploaded
// and preview samples. selected reports whether a rate metric is part of the
// upload set for an object; unpaired rates are dropped only when the upload
// profile required both sides of the pair.
func finalizeV2Sample(out *v2.Sample, selected func(key string, id uint16) bool) {
	// A partial battery reading is unavailable, never an independently uploaded field.
	out.Batteries = slices.DeleteFunc(out.Batteries, func(b v2.BatterySample) bool { return b.Level == nil || b.Charging == nil })
	// Paired rates describe one observation interval. If either side is missing,
	// omit both while retaining other valid fields on that device.
	for n := range out.Drives {
		d := &out.Drives[n]
		if selected("drive.read_rate", d.ID) && selected("drive.write_rate", d.ID) && (d.ReadRate == nil || d.WriteRate == nil) {
			d.ReadRate = nil
			d.WriteRate = nil
		}
	}
	for n := range out.Interfaces {
		i := &out.Interfaces[n]
		if selected("interface.upload_rate", i.ID) && selected("interface.download_rate", i.ID) && (i.UploadRate == nil || i.DownloadRate == nil) {
			i.UploadRate = nil
			i.DownloadRate = nil
		}
	}
	out.Interfaces = slices.DeleteFunc(out.Interfaces, func(i v2.InterfaceSample) bool {
		return i.Up == nil && i.UploadRate == nil && i.DownloadRate == nil && i.IPv4 == nil
	})
	// Failed SMART reads must not create an object containing only an ID/time.
	for n := len(out.Drives) - 1; n >= 0; n-- {
		d := &out.Drives[n]
		if d.SMART != nil && d.SMART.Healthy == nil {
			d.SMART = nil
		}
		if d.SMART == nil && d.Used == nil && d.ReadRate == nil && d.WriteRate == nil {
			out.Drives = append(out.Drives[:n], out.Drives[n+1:]...)
		}
	}
	if out.CPU != nil && reflect.DeepEqual(*out.CPU, v2.CPUSample{}) {
		out.CPU = nil
	}
	if out.Memory != nil && reflect.DeepEqual(*out.Memory, v2.MemorySample{}) {
		out.Memory = nil
	}
}

func sortSampleObjects(out *v2.Sample) {
	less := func(a, b uint16) int { return int(a) - int(b) }
	slices.SortFunc(out.Cores, func(a, b v2.CoreSample) int { return less(a.ID, b.ID) })
	slices.SortFunc(out.GPUs, func(a, b v2.GPUSample) int { return less(a.ID, b.ID) })
	slices.SortFunc(out.Drives, func(a, b v2.DriveSample) int { return less(a.ID, b.ID) })
	slices.SortFunc(out.Interfaces, func(a, b v2.InterfaceSample) int { return less(a.ID, b.ID) })
	slices.SortFunc(out.Batteries, func(a, b v2.BatterySample) int { return less(a.ID, b.ID) })
}

func v2ObjectSelected(c Config, key string, id uint16) bool {
	if c.Upload == nil {
		return false
	}
	for _, selection := range c.Upload.Optional {
		if selection.Metric == key && slices.Contains(selection.Objects, id) {
			return true
		}
	}
	return false
}
func sampleDrive(s *v2.Sample, id uint16) *v2.DriveSample {
	for n := range s.Drives {
		if s.Drives[n].ID == id {
			return &s.Drives[n]
		}
	}
	s.Drives = append(s.Drives, v2.DriveSample{ID: id})
	return &s.Drives[len(s.Drives)-1]
}
func sampleGPU(s *v2.Sample, id uint16) *v2.GPUSample {
	for n := range s.GPUs {
		if s.GPUs[n].ID == id {
			return &s.GPUs[n]
		}
	}
	s.GPUs = append(s.GPUs, v2.GPUSample{ID: id})
	return &s.GPUs[len(s.GPUs)-1]
}
func sampleInterface(s *v2.Sample, id uint16) *v2.InterfaceSample {
	for n := range s.Interfaces {
		if s.Interfaces[n].ID == id {
			return &s.Interfaces[n]
		}
	}
	s.Interfaces = append(s.Interfaces, v2.InterfaceSample{ID: id})
	return &s.Interfaces[len(s.Interfaces)-1]
}
func sampleBattery(s *v2.Sample, id uint16) *v2.BatterySample {
	for n := range s.Batteries {
		if s.Batteries[n].ID == id {
			return &s.Batteries[n]
		}
	}
	s.Batteries = append(s.Batteries, v2.BatterySample{ID: id})
	return &s.Batteries[len(s.Batteries)-1]
}

func byteMetric(key string) bool {
	switch key {
	case "memory.used", "memory.app", "memory.wired", "memory.compressed", "memory.swap_total", "memory.swap_used", "drive.used", "drive.read_rate", "drive.write_rate", "interface.upload_rate", "interface.download_rate":
		return true
	}
	return false
}
