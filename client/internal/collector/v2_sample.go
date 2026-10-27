package collector

import (
	"errors"
	v2 "esp32-monitor/client/internal/collector/uploadv2"
	"math"
	"math/big"
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
func (m *Manager) sampleV2(c Config, b Batch) (v2.Sample, error) {
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
	for _, sel := range c.Upload.Optional {
		binding, ok := v2Bindings[sel.Metric]
		if !ok {
			continue
		}
		for _, v := range b.Metrics {
			if v.Name != binding.source {
				continue
			}
			id := m.v2.object(binding.kind, v)
			if binding.kind != "" && !slices.Contains(sel.Objects, id) {
				continue
			}
			if binding.kind == "" && binding.source == "cpu" && v.Tags["cpu"] != "cpu-total" {
				continue
			}
			if strings.HasPrefix(sel.Metric, "drive.smart_") || sel.Metric == "network.public_ipv4" {
				if v.Timestamp < out.Timestamp-900 || v.Timestamp > out.Timestamp {
					continue
				}
			} else if v.Timestamp < out.Timestamp-3 || v.Timestamp > out.Timestamp {
				continue
			}
			if sel.Metric == "drive.smart_attribute" {
				dst := sampleDrive(&out, id)
				if dst.SMART == nil {
					dst.SMART = &v2.SMARTSample{Timestamp: v.Timestamp}
				}
				for _, detail := range sel.Detail {
					field := smartFields[int(detail)-1]
					if raw, ok := smartInteger(v, field); ok {
						if dst.SMART.Values == nil {
							dst.SMART.Values = map[uint16]string{}
						}
						dst.SMART.Values[detail] = raw
					}
				}
				continue
			}
			value, exists := v.Fields[binding.field]
			if _, ok := v.Exact[binding.field]; ok {
				exists = true
			}
			if !exists {
				continue
			}
			scale, lo, hi := float64(1), float64(0), float64(9007199254740991)
			switch sel.Metric {
			case "core.usage", "gpu.usage", "battery.level":
				scale, hi = 100, 100
			case "cpu.temperature", "gpu.temperature":
				scale, lo, hi = 10, -273.1, 3276.7
			case "cpu.load1", "cpu.load5", "cpu.load15":
				scale, hi = 1000, float64(math.MaxUint32)/1000
			case "fan.rpm":
				hi = math.MaxUint32
			case "memory.pressure":
				hi = 4
			case "battery.charging", "interface.up", "drive.smart_health":
				hi = 1
				if value != 0 && value != 1 {
					continue
				}
			}
			var n int64
			var unsigned uint64
			if byteMetric(sel.Metric) {
				if raw, ok := v.Exact[binding.field]; ok {
					value, e := strconv.ParseUint(raw, 10, 64)
					if e != nil {
						continue
					}
					unsigned = value
				} else {
					value, e := scaled(v, binding.field, 1, 0, 9007199254740991)
					if e != nil {
						continue
					}
					unsigned = uint64(value)
				}
			} else {
				var e error
				n, e = scaled(v, binding.field, scale, lo, hi)
				if e != nil {
					continue
				}
			}
			switch sel.Metric {
			case "cpu.load1", "cpu.load5", "cpu.load15", "cpu.temperature":
				if out.CPU == nil {
					out.CPU = &v2.CPUSample{}
				}
				switch sel.Metric {
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
				switch sel.Metric {
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
						continue
					}
					out.Memory.Pressure = &pressure
				}
			case "core.usage":
				out.Cores = append(out.Cores, v2.CoreSample{ID: id, Usage: ptr(uint16(n))})
			case "gpu.usage", "gpu.temperature":
				dst := sampleGPU(&out, id)
				if sel.Metric == "gpu.usage" {
					dst.Usage = ptr(uint16(n))
				} else {
					dst.Temperature = ptr(int16(n))
				}
			case "drive.read_rate":
				sampleDrive(&out, id).ReadRate = ptr(unsigned)
			case "drive.write_rate":
				sampleDrive(&out, id).WriteRate = ptr(unsigned)
			case "drive.smart_health":
				dst := sampleDrive(&out, id)
				if dst.SMART == nil {
					dst.SMART = &v2.SMARTSample{Timestamp: v.Timestamp}
				}
				dst.SMART.Healthy = ptr(n == 1)
			case "volume.used":
				out.Volumes = append(out.Volumes, v2.VolumeSample{ID: id, Used: ptr(unsigned)})
			case "interface.upload_rate":
				sampleInterface(&out, id).UploadRate = ptr(unsigned)
			case "interface.download_rate":
				sampleInterface(&out, id).DownloadRate = ptr(unsigned)
			case "interface.up":
				sampleInterface(&out, id).Up = ptr(n == 1)
			case "interface.ipv4":
				ips := []string{}
				for _, ip := range strings.Split(v.Tags["ipv4"], ",") {
					if parsed := net.ParseIP(ip); parsed != nil && parsed.To4() != nil {
						ips = append(ips, parsed.To4().String())
					}
				}
				slices.Sort(ips)
				ips = slices.Compact(ips)
				sampleInterface(&out, id).IPv4 = &ips
			case "network.public_ipv4":
				ip := net.ParseIP(v.Tags["ipv4"])
				if ip != nil && ip.To4() != nil && ip.IsGlobalUnicast() && !ip.IsPrivate() {
					out.PublicIPv4 = &v2.IPv4Observation{Timestamp: v.Timestamp, Address: ip.To4().String()}
				}
			case "battery.level", "battery.charging":
				dst := sampleBattery(&out, id)
				if sel.Metric == "battery.level" {
					dst.Level = ptr(uint16(n))
				} else {
					dst.Charging = ptr(n == 1)
				}
			case "fan.rpm":
				out.Fans = append(out.Fans, v2.FanSample{ID: id, RPM: ptr(uint32(n))})
			}
		}
	}
	// Failed SMART reads must not create an object containing only an ID/time.
	for n := len(out.Drives) - 1; n >= 0; n-- {
		d := &out.Drives[n]
		if d.SMART != nil && d.SMART.Healthy == nil && len(d.SMART.Values) == 0 {
			d.SMART = nil
		}
		if d.SMART == nil && d.ReadRate == nil && d.WriteRate == nil {
			out.Drives = append(out.Drives[:n], out.Drives[n+1:]...)
		}
	}
	if out.CPU != nil && reflect.DeepEqual(*out.CPU, v2.CPUSample{}) {
		out.CPU = nil
	}
	if out.Memory != nil && reflect.DeepEqual(*out.Memory, v2.MemorySample{}) {
		out.Memory = nil
	}
	return out, nil
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
	case "memory.used", "memory.app", "memory.wired", "memory.compressed", "memory.swap_total", "memory.swap_used", "volume.used", "drive.read_rate", "drive.write_rate", "interface.upload_rate", "interface.download_rate":
		return true
	}
	return false
}
func smartInteger(m Metric, key string) (string, bool) {
	if raw, ok := m.Exact[key]; ok {
		n, valid := new(big.Int).SetString(raw, 10)
		if valid && n.Sign() >= 0 && n.BitLen() <= 128 {
			return n.String(), true
		}
		return "", false
	}
	if n := number(m, key); n != nil {
		return strconv.FormatUint(*n, 10), true
	}
	return "", false
}
