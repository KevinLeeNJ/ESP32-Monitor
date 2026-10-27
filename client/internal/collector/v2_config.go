package collector

import (
	"encoding/json"
	"errors"
	v2 "esp32-monitor/client/internal/collector/uploadv2"
	"reflect"
	"slices"
	"sort"
	"strings"
)

func (m *Manager) prepareV2(c Config) (Config, error) {
	if c.IntervalSeconds != 0 && (c.IntervalSeconds < 5 || c.IntervalSeconds > 60) {
		return c, errors.New("invalid_collector_config")
	}
	if m.isV2() {
		for _, sel := range c.Selections {
			if sel.Upload {
				return c, errors.New("v2_use_upload_optional")
			}
		}
	}
	if c.Upload == nil {
		return c, nil
	}
	if !m.isV2() {
		return c, errors.New("v2_upload_requires_v2_endpoint")
	}
	if c.Upload.IntervalSeconds == 0 {
		c.Upload.IntervalSeconds = uint16(c.IntervalSeconds)
	}
	if c.Upload.IntervalSeconds < 5 || c.Upload.IntervalSeconds > 60 {
		return c, errors.New("invalid_collector_config")
	}
	c.IntervalSeconds = int(c.Upload.IntervalSeconds)
	if !c.Upload.Enabled {
		return c, nil
	}
	if len(c.Upload.Optional) > 256 {
		return c, errors.New("too_many_v2_selections")
	}
	seen := map[string]bool{}
	expanded := 0
	for _, sel := range c.Upload.Optional {
		kind, fixed := v2Static[sel.Metric]
		if !fixed {
			b, ok := v2Bindings[sel.Metric]
			if !ok {
				return c, errors.New("unknown_v2_metric")
			}
			kind = b.kind
		}
		if sel.Metric == "cpu.usage" || sel.Metric == "memory.usage" || sel.Metric == "system.uptime" || seen[sel.Metric] {
			return c, errors.New("duplicate_or_required_v2_metric")
		}
		seen[sel.Metric] = true
		if (kind != "" && len(sel.Objects) == 0) || (kind == "" && len(sel.Objects) > 0) || len(sel.Objects) > 256 {
			return c, errors.New("invalid_v2_objects")
		}
		allowed := m.v2.objectsOfKind(kind)
		ids := map[uint16]bool{}
		for _, id := range sel.Objects {
			if !allowed[id] || ids[id] {
				return c, errors.New("v2_object_unavailable")
			}
			ids[id] = true
		}
		if fixed && !m.fixedV2Available(sel.Metric) {
			return c, errors.New("v2_metric_unavailable")
		}
		if fixed {
			for _, id := range sel.Objects {
				if !m.fixedV2ObjectAvailable(sel.Metric, id) {
					return c, errors.New("v2_metric_object_unavailable")
				}
			}
		}
		if sel.Metric == "drive.smart_attribute" {
			if len(sel.Detail) == 0 {
				return c, errors.New("smart_detail_required")
			}
			details := map[uint16]bool{}
			for _, id := range sel.Detail {
				if id == 0 || int(id) > len(smartFields) || details[id] {
					return c, errors.New("invalid_smart_detail")
				}
				details[id] = true
			}
		} else if len(sel.Detail) > 0 {
			return c, errors.New("unexpected_smart_detail")
		}
		expanded += max(1, len(sel.Objects)) * max(1, len(sel.Detail))
		if expanded > 1024 {
			return c, errors.New("too_many_v2_selections")
		}
	}
	sort.Slice(c.Upload.Optional, func(a, b int) bool { return c.Upload.Optional[a].Metric < c.Upload.Optional[b].Metric })
	for n := range c.Upload.Optional {
		sort.Slice(c.Upload.Optional[n].Objects, func(a, b int) bool { return c.Upload.Optional[n].Objects[a] < c.Upload.Optional[n].Objects[b] })
		slices.Sort(c.Upload.Optional[n].Detail)
	}
	return c, nil
}
func v2CollectConfig(c Config) Config {
	out := detached(c)
	if c.Upload == nil || !c.Upload.Enabled {
		return out
	}
	sources := []string{"cpu", "mem", "system"}
	for _, sel := range c.Upload.Optional {
		if b, ok := v2Bindings[sel.Metric]; ok {
			sources = append(sources, b.source)
		}
	}
	for _, source := range sources {
		out.Selections = append(out.Selections, Selection{ID: source + "/" + source + "/__v2"})
	}
	return out
}
func (m *Manager) profileV2(c Config) v2.Profile {
	p := v2.Profile{Version: 2, IntervalSeconds: uint16(c.IntervalSeconds), Enabled: []v2.UploadSelection{}}
	if c.Upload == nil || !c.Upload.Enabled {
		return p
	}
	p.Enabled = append([]v2.UploadSelection{}, detached(c.Upload.Optional)...)
	for _, sel := range p.Enabled {
		switch sel.Metric {
		case "system.os":
			p.Inventory.OS = m.v2.inventory.OS
		case "system.product":
			p.Inventory.Product = m.v2.inventory.Product
		case "memory.total":
			p.Inventory.MemoryTotal = m.v2.inventory.MemoryTotal
		}
		if strings.HasPrefix(sel.Metric, "cpu.") {
			if p.Inventory.CPU == nil {
				p.Inventory.CPU = &v2.CPUInfo{}
			}
			src := m.v2.inventory.CPU
			dst := p.Inventory.CPU
			switch sel.Metric {
			case "cpu.model":
				dst.Model = src.Model
			case "cpu.physical_cores":
				dst.PhysicalCores = src.PhysicalCores
			case "cpu.logical_cores":
				dst.LogicalCores = src.LogicalCores
			case "cpu.performance_cores":
				dst.PerformanceCores = src.PerformanceCores
			case "cpu.efficiency_cores":
				dst.EfficiencyCores = src.EfficiencyCores
			}
			if reflect.DeepEqual(*dst, v2.CPUInfo{}) {
				p.Inventory.CPU = nil
			}
		}
		kind := v2Bindings[sel.Metric].kind
		if k, ok := v2Static[sel.Metric]; ok {
			kind = k
		}
		for _, id := range sel.Objects {
			switch kind {
			case "core":
				for _, v := range m.v2.inventory.Cores {
					if v.ID == id && !hasObject(p.Inventory.Cores, id) {
						p.Inventory.Cores = append(p.Inventory.Cores, v)
					}
				}
			case "gpu":
				if !hasObject(p.Inventory.GPUs, id) {
					p.Inventory.GPUs = append(p.Inventory.GPUs, v2.ObjectInfo{ID: id})
				}
			case "battery":
				if !hasObject(p.Inventory.Batteries, id) {
					p.Inventory.Batteries = append(p.Inventory.Batteries, v2.ObjectInfo{ID: id})
				}
			case "fan":
				for _, v := range m.v2.inventory.Fans {
					if v.ID == id && !hasObject(p.Inventory.Fans, id) {
						p.Inventory.Fans = append(p.Inventory.Fans, v)
					}
				}
			case "interface":
				dst := findInterface(&p.Inventory, id)
				if sel.Metric == "interface.medium" {
					for _, v := range m.v2.inventory.Interfaces {
						if v.ID == id {
							dst.Medium = v.Medium
						}
					}
				}
			case "drive":
				dst := findDrive(&p.Inventory, id)
				for _, v := range m.v2.inventory.Drives {
					if v.ID != id {
						continue
					}
					switch sel.Metric {
					case "drive.model":
						dst.Model = v.Model
					case "drive.total":
						dst.TotalBytes = v.TotalBytes
					case "drive.smart_attribute":
						for _, a := range v.SMART {
							if slices.Contains(sel.Detail, a.ID) {
								dst.SMART = append(dst.SMART, a)
							}
						}
					}
				}
			case "volume":
				var dst *v2.VolumeInfo
				for n := range p.Inventory.Volumes {
					if p.Inventory.Volumes[n].ID == id {
						dst = &p.Inventory.Volumes[n]
					}
				}
				if dst == nil {
					p.Inventory.Volumes = append(p.Inventory.Volumes, v2.VolumeInfo{ID: id})
					dst = &p.Inventory.Volumes[len(p.Inventory.Volumes)-1]
				}
				for _, v := range m.v2.inventory.Volumes {
					if v.ID == id {
						switch sel.Metric {
						case "volume.filesystem":
							dst.Filesystem = v.Filesystem
						case "volume.total":
							dst.TotalBytes = v.TotalBytes
						}
					}
				}
			}
		}
	}
	sortInventory(&p.Inventory)
	return p
}
func hasObject[T any](a []T, id uint16) bool {
	for _, v := range a {
		if reflect.ValueOf(v).FieldByName("ID").Uint() == uint64(id) {
			return true
		}
	}
	return false
}

func validateV2Profile(p v2.Profile) error {
	inventory := reflect.ValueOf(p.Inventory)
	for n := 0; n < inventory.NumField(); n++ {
		field := inventory.Field(n)
		if field.Kind() == reflect.Slice && field.Len() > 256 {
			return errors.New("too_many_v2_objects")
		}
	}
	body, err := json.Marshal(p)
	if err != nil || len(body) > maxBody {
		return errors.New("v2_profile_too_large")
	}
	return nil
}
