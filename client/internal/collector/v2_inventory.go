package collector

import (
	v2 "esp32-monitor/client/internal/collector/uploadv2"
	"fmt"
	"reflect"
	"runtime"
	"slices"
	"sort"
	"strings"
)

func (s *v2State) buildInventory(b Batch) error {
	i := &s.inventory
	i.CPU = &v2.CPUInfo{}
	// Native values override Core only when present.
	for _, name := range []string{"cpu", "darwin_cpu"} {
		m := fixedMetric(b, name, "", "")
		if p := text(m.Tags, "model"); p != nil {
			i.CPU.Model = p
		}
		for key, dst := range map[string]**uint16{"physical_cores": &i.CPU.PhysicalCores, "logical_cores": &i.CPU.LogicalCores, "performance_cores": &i.CPU.PerformanceCores, "efficiency_cores": &i.CPU.EfficiencyCores} {
			if p := count(m, key); p != nil {
				*dst = p
			}
		}
	}
	i.MemoryTotal = number(fixedMetric(b, "mem", "", ""), "total")
	system := fixedMetric(b, "system", "", "")
	if platform := system.Tags["os_platform"]; platform != "" {
		i.OS = &v2.OSInfo{Family: runtime.GOOS, Name: platform, Version: system.Tags["os_version"]}
	}
	native := fixedMetric(b, "darwin_system", "", "")
	if native.Tags["os_name"] != "" {
		name := native.Tags["os_name"]
		if code := native.Tags["os_codename"]; code != "" {
			name += " " + code
		}
		i.OS = &v2.OSInfo{Family: "darwin", Name: name, Version: native.Tags["os_version"], Build: text(native.Tags, "os_build")}
	}
	i.Product = text(native.Tags, "product_name")
	if i.CPU.LogicalCores != nil {
		for n := uint16(0); n < *i.CPU.LogicalCores; n++ {
			id, e := s.allocate("core", Metric{Tags: map[string]string{"cpu": fmt.Sprintf("cpu%d", n)}})
			if e != nil {
				return e
			}
			i.Cores = append(i.Cores, v2.CoreInfo{ID: id, Index: n})
		}
	}
	// Sorting makes first allocation deterministic; old IDs remain reserved forever.
	metrics := slices.Clone(b.Metrics)
	sort.Slice(metrics, func(a, b int) bool {
		return metrics[a].Name+tagKey(metrics[a].Tags) < metrics[b].Name+tagKey(metrics[b].Tags)
	})
	for _, m := range metrics {
		var kind string
		switch m.Name {
		case "net", "darwin_network":
			kind = "interface"
		case "diskio", "darwin_disk":
			kind = "drive"
		case "darwin_gpu":
			kind = "gpu"
		case "darwin_fan":
			kind = "fan"
		case "darwin_battery":
			kind = "battery"
		default:
			continue
		}
		id, e := s.allocate(kind, m)
		if e != nil {
			return e
		}
		switch kind {
		case "interface":
			p := findInterface(i, id)
			if medium := m.Tags["type"]; medium != "" {
				if medium == "lan" {
					medium = "ethernet"
				}
				p.Medium = ptr(medium)
			}
		case "volume":
			i.Volumes = append(i.Volumes, v2.VolumeInfo{ID: id, Filesystem: text(m.Tags, "fstype"), TotalBytes: number(m, "total")})
		case "drive":
			p := findDrive(i, id)
			if model := text(m.Tags, "model"); model != nil {
				p.Model = model
			}
			if total := number(m, "total"); total != nil {
				p.TotalBytes = total
			}
			if m.Name == "darwin_disk" {
				for n, f := range smartFields {
					if _, ok := m.Fields[f]; !ok {
						if _, ok = m.Exact[f]; !ok {
							continue
						}
					}
					unit := "count"
					if strings.Contains(f, "percent") {
						unit = "percent"
					}
					if strings.Contains(f, "hours") {
						unit = "hours"
					}
					p.SMART = append(p.SMART, v2.SMARTAttributeInfo{ID: uint16(n + 1), Key: f, Unit: unit, Scale10: 0, Source: "diskutil"})
				}
			}
		case "gpu":
			i.GPUs = append(i.GPUs, v2.ObjectInfo{ID: id})
		case "fan":
			i.Fans = append(i.Fans, v2.FanInfo{ID: id, Scope: "system"})
		case "battery":
			i.Batteries = append(i.Batteries, v2.ObjectInfo{ID: id})
		}
	}
	sortInventory(i)
	return nil
}
func findInterface(i *v2.Inventory, id uint16) *v2.InterfaceInfo {
	for n := range i.Interfaces {
		if i.Interfaces[n].ID == id {
			return &i.Interfaces[n]
		}
	}
	i.Interfaces = append(i.Interfaces, v2.InterfaceInfo{ID: id})
	return &i.Interfaces[len(i.Interfaces)-1]
}
func findDrive(i *v2.Inventory, id uint16) *v2.DriveInfo {
	for n := range i.Drives {
		if i.Drives[n].ID == id {
			return &i.Drives[n]
		}
	}
	i.Drives = append(i.Drives, v2.DriveInfo{ID: id})
	return &i.Drives[len(i.Drives)-1]
}
func sortInventory(i *v2.Inventory) {
	v := reflect.ValueOf(i).Elem()
	for n := 0; n < v.NumField(); n++ {
		f := v.Field(n)
		if f.Kind() == reflect.Slice {
			sort.Slice(f.Interface(), func(a, b int) bool { return f.Index(a).FieldByName("ID").Uint() < f.Index(b).FieldByName("ID").Uint() })
		}
	}
}
func (m *Manager) v2Capabilities(batch Batch) *V2Capabilities {
	if m.v2 == nil {
		return nil
	}
	out := &V2Capabilities{Inventory: detached(m.v2.inventory), Metrics: []V2Metric{}}
	keys := []string{}
	for k := range v2Bindings {
		keys = append(keys, k)
	}
	for k := range v2Static {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		kind, fixed := v2Static[key]
		if !fixed {
			kind = v2Bindings[key].kind
		}
		c := V2Metric{Metric: key, Fixed: fixed}
		for id := range m.v2.objectsOfKind(kind) {
			c.Objects = append(c.Objects, id)
		}
		sort.Slice(c.Objects, func(a, b int) bool { return c.Objects[a] < c.Objects[b] })
		if key == "drive.smart_attribute" {
			for n := range smartFields {
				c.Detail = append(c.Detail, uint16(n+1))
			}
		}
		c.Required = key == "cpu.usage" || key == "memory.usage" || key == "system.uptime"
		if fixed {
			c.Available = m.fixedV2Available(key)
			c.Objects = slices.DeleteFunc(c.Objects, func(id uint16) bool { return !m.fixedV2ObjectAvailable(key, id) })
		} else {
			binding := v2Bindings[key]
			observed := []uint16{}
			for _, v := range batch.Metrics {
				if v.Name != binding.source {
					continue
				}
				_, ok := v.Fields[binding.field]
				if _, exact := v.Exact[binding.field]; exact {
					ok = true
				}
				if key == "drive.smart_attribute" {
					ok = false
					for _, f := range smartFields {
						if _, has := smartInteger(v, f); has {
							ok = true
						}
					}
				}
				if ok {
					c.Available = true
					if kind != "" {
						if id := m.v2.object(kind, v); id != 0 {
							observed = append(observed, id)
						}
					}
				}
			}
			if kind != "" {
				slices.Sort(observed)
				c.Objects = slices.Compact(observed)
				c.Available = len(c.Objects) > 0
			}
			if key == "network.public_ipv4" && runtime.GOOS == "darwin" {
				c.Available = true
				c.Reason = "not_probed_until_selected"
			}
		}
		if !c.Available {
			c.Reason = "metric_unavailable"
		}
		out.Metrics = append(out.Metrics, c)
	}
	for key, id := range m.v2.IDs {
		kind, name, ok := strings.Cut(key, ":")
		if ok && m.v2.objectsOfKind(kind)[id] {
			out.Objects = append(out.Objects, V2Object{ID: id, Kind: kind, Name: name})
		}
	}
	sort.Slice(out.Objects, func(a, b int) bool { return out.Objects[a].ID < out.Objects[b].ID })
	return out
}

func (m *Manager) fixedV2Available(key string) bool {
	i := m.v2.inventory
	switch key {
	case "system.os":
		return i.OS != nil
	case "system.product":
		return i.Product != nil
	case "memory.total":
		return i.MemoryTotal != nil
	case "cpu.model":
		return i.CPU.Model != nil
	case "cpu.physical_cores":
		return i.CPU.PhysicalCores != nil
	case "cpu.logical_cores":
		return i.CPU.LogicalCores != nil
	case "cpu.performance_cores":
		return i.CPU.PerformanceCores != nil
	case "cpu.efficiency_cores":
		return i.CPU.EfficiencyCores != nil
	}
	for _, v := range i.Drives {
		if key == "drive.model" && v.Model != nil || key == "drive.total" && v.TotalBytes != nil {
			return true
		}
	}
	for _, v := range i.Volumes {
		if key == "volume.filesystem" && v.Filesystem != nil || key == "volume.total" && v.TotalBytes != nil {
			return true
		}
	}
	for _, v := range i.Interfaces {
		if key == "interface.medium" && v.Medium != nil {
			return true
		}
	}
	return false
}

func (m *Manager) fixedV2ObjectAvailable(key string, id uint16) bool {
	i := m.v2.inventory
	for _, v := range i.Drives {
		if v.ID == id {
			switch key {
			case "drive.model":
				return v.Model != nil
			case "drive.total":
				return v.TotalBytes != nil
			}
		}
	}
	for _, v := range i.Volumes {
		if v.ID == id {
			switch key {
			case "volume.filesystem":
				return v.Filesystem != nil
			case "volume.total":
				return v.TotalBytes != nil
			}
		}
	}
	for _, v := range i.Interfaces {
		if v.ID == id && key == "interface.medium" {
			return v.Medium != nil
		}
	}
	return false
}
func (s *v2State) objectsOfKind(kind string) map[uint16]bool {
	out := map[uint16]bool{}
	v := reflect.ValueOf(s.inventory)
	fields := map[string]string{"core": "Cores", "drive": "Drives", "volume": "Volumes", "interface": "Interfaces", "gpu": "GPUs", "battery": "Batteries", "fan": "Fans"}
	name := fields[kind]
	if name == "" {
		return out
	}
	a := v.FieldByName(name)
	for n := 0; n < a.Len(); n++ {
		out[uint16(a.Index(n).FieldByName("ID").Uint())] = true
	}
	return out
}
