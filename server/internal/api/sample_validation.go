package api

import (
	v2 "esp32-monitor/server/internal/uploadv2"
	"net/http"
)

// selection indexes the profile's enabled declarations by semantic ID.
type selection struct {
	objects map[uint16]bool
}

func selectionsOf(p *v2.Profile) map[string]*selection {
	out := map[string]*selection{}
	for _, sel := range p.Enabled {
		s, ok := out[sel.Metric]
		if !ok {
			s = &selection{objects: map[uint16]bool{}}
			out[sel.Metric] = s
		}
		for _, id := range sel.Objects {
			s.objects[id] = true
		}
	}
	return out
}

func objectIDs(list []v2.ObjectInfo) map[uint16]bool {
	out := map[uint16]bool{}
	for _, o := range list {
		out[o.ID] = true
	}
	return out
}

func objectID[T any](list []T, id func(*T) uint16) map[uint16]bool {
	out := map[uint16]bool{}
	for i := range list {
		out[id(&list[i])] = true
	}
	return out
}

func coreIDs(p *v2.Profile) map[uint16]bool {
	return objectID(p.Inventory.Cores, func(c *v2.CoreInfo) uint16 { return c.ID })
}

func validateSample(d *Device, sm *v2.Sample, serverNow int64) error {
	if sm.Timestamp == 0 {
		return errf(http.StatusUnprocessableEntity, "timestamp_required")
	}
	if sm.Timestamp > serverNow+30 {
		return errf(http.StatusUnprocessableEntity, "timestamp_in_future").withTime()
	}
	if sm.Timestamp < serverNow-86400 {
		return errf(http.StatusUnprocessableEntity, "timestamp_too_old").withTime()
	}
	if sm.CPUUsage == nil || sm.MemoryUsage == nil || sm.Uptime == nil {
		return errf(http.StatusUnprocessableEntity, "required_metric_missing")
	}
	if *sm.CPUUsage > 10000 || *sm.MemoryUsage > 10000 {
		return errf(http.StatusUnprocessableEntity, "metric_out_of_range")
	}
	sel := selectionsOf(d.Profile)
	inv := d.Profile.Inventory
	if mem := sm.Memory; mem != nil {
		// memory.used is an implicit auto-upload field; the rest need selections.
		for _, pair := range []struct {
			selected bool
			set      bool
			name     string
		}{
			{true, mem.Used != nil, "memory.used"},
			{sel["memory.app"] != nil, mem.App != nil, "memory.app"},
			{sel["memory.wired"] != nil, mem.Wired != nil, "memory.wired"},
			{sel["memory.compressed"] != nil, mem.Compressed != nil, "memory.compressed"},
			{sel["memory.swap_total"] != nil, mem.SwapTotal != nil, "memory.swap_total"},
			{sel["memory.swap_used"] != nil, mem.SwapUsed != nil, "memory.swap_used"},
			{sel["memory.pressure"] != nil, mem.Pressure != nil, "memory.pressure"},
		} {
			if pair.set && !pair.selected {
				return errf(http.StatusUnprocessableEntity, "metric_not_enabled:"+pair.name)
			}
		}
	}
	if cpu := sm.CPU; cpu != nil {
		for _, pair := range []struct {
			selected bool
			set      bool
			name     string
		}{
			{sel["cpu.load1"] != nil, cpu.Load1 != nil, "cpu.load1"},
			{sel["cpu.load5"] != nil, cpu.Load5 != nil, "cpu.load5"},
			{sel["cpu.load15"] != nil, cpu.Load15 != nil, "cpu.load15"},
			{sel["cpu.temperature"] != nil, cpu.Temperature != nil, "cpu.temperature"},
		} {
			if pair.set && !pair.selected {
				return errf(http.StatusUnprocessableEntity, "metric_not_enabled:"+pair.name)
			}
		}
	}
	cores := coreIDs(d.Profile)
	for _, core := range sm.Cores {
		if !cores[core.ID] {
			return errf(http.StatusUnprocessableEntity, "metric_not_enabled:core.usage object")
		}
		if sel["core.usage"] == nil || !sel["core.usage"].objects[core.ID] {
			return errf(http.StatusUnprocessableEntity, "metric_not_enabled:core.usage")
		}
	}
	gpus := objectIDs(inv.GPUs)
	for _, gpu := range sm.GPUs {
		if !gpus[gpu.ID] || sel["gpu.usage"] == nil || !sel["gpu.usage"].objects[gpu.ID] {
			return errf(http.StatusUnprocessableEntity, "metric_not_enabled:gpu.usage")
		}
		if gpu.Temperature != nil && (sel["gpu.temperature"] == nil || !sel["gpu.temperature"].objects[gpu.ID]) {
			return errf(http.StatusUnprocessableEntity, "metric_not_enabled:gpu.temperature")
		}
	}
	drives := objectID(inv.Drives, func(dr *v2.DriveInfo) uint16 { return dr.ID })
	for _, drive := range sm.Drives {
		if !drives[drive.ID] {
			return errf(http.StatusUnprocessableEntity, "metric_not_enabled:drive object")
		}
		if (drive.ReadRate != nil && !selectedFor(sel, "drive.read_rate", drive.ID)) ||
			(drive.WriteRate != nil && !selectedFor(sel, "drive.write_rate", drive.ID)) ||
			(drive.Used != nil && !selectedFor(sel, "drive.used", drive.ID)) {
			return errf(http.StatusUnprocessableEntity, "metric_not_enabled:drive rates")
		}
		if drive.SMART != nil {
			if drive.SMART.Timestamp > sm.Timestamp {
				return errf(http.StatusUnprocessableEntity, "timestamp_in_future").withTime()
			}
			if !selectedFor(sel, "drive.smart_health", drive.ID) && drive.SMART.Healthy != nil {
				return errf(http.StatusUnprocessableEntity, "metric_not_enabled:drive.smart_health")
			}
		}
	}
	interfaces := objectID(inv.Interfaces, func(in *v2.InterfaceInfo) uint16 { return in.ID })
	for _, net := range sm.Interfaces {
		if !interfaces[net.ID] {
			return errf(http.StatusUnprocessableEntity, "metric_not_enabled:interface object")
		}
		if (net.Up != nil && !selectedFor(sel, "interface.up", net.ID)) ||
			(net.UploadRate != nil && !selectedFor(sel, "interface.upload_rate", net.ID)) ||
			(net.DownloadRate != nil && !selectedFor(sel, "interface.download_rate", net.ID)) ||
			(net.IPv4 != nil && !selectedFor(sel, "interface.ipv4", net.ID)) {
			return errf(http.StatusUnprocessableEntity, "metric_not_enabled:interface fields")
		}
	}
	if sm.PublicIPv4 != nil {
		if sm.PublicIPv4.Timestamp > sm.Timestamp {
			return errf(http.StatusUnprocessableEntity, "timestamp_in_future").withTime()
		}
		if sel["network.public_ipv4"] == nil {
			return errf(http.StatusUnprocessableEntity, "metric_not_enabled:network.public_ipv4")
		}
	}
	batteries := objectIDs(inv.Batteries)
	for _, bat := range sm.Batteries {
		if !batteries[bat.ID] || !selectedFor(sel, "battery.level", bat.ID) || !selectedFor(sel, "battery.charging", bat.ID) {
			return errf(http.StatusUnprocessableEntity, "metric_not_enabled:battery.level")
		}
		if bat.Level == nil || bat.Charging == nil {
			return errf(http.StatusUnprocessableEntity, "metric_not_enabled:battery pair")
		}
	}
	fans := objectID(inv.Fans, func(f *v2.FanInfo) uint16 { return f.ID })
	for _, fan := range sm.Fans {
		if !fans[fan.ID] || sel["fan.rpm"] == nil || !sel["fan.rpm"].objects[fan.ID] {
			return errf(http.StatusUnprocessableEntity, "metric_not_enabled:fan.rpm")
		}
	}
	if len(sm.Volumes) > 0 {
		return errf(http.StatusUnprocessableEntity, "metric_not_enabled:volume (legacy)")
	}
	return nil
}

func selectedFor(sel map[string]*selection, metric string, id uint16) bool {
	s := sel[metric]
	return s != nil && s.objects[id]
}

func (e *apiError) withTime() *apiError {
	e.serverTime = true
	return e
}
