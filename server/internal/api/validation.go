package api

import (
	"encoding/json"
	v2 "esp32-monitor/server/internal/uploadv2"
	"net/netip"
	"reflect"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

func validateInventory(p *v2.Profile) error {
	if p.Enabled == nil {
		return errf(422, "enabled_required")
	}
	if len(p.Enabled) > 256 {
		return errf(422, "too_many_selections")
	}
	// The catalog is typed from the Client contract; each object class is bounded.
	raw, _ := json.Marshal(p.Inventory)
	var inv map[string]json.RawMessage
	_ = json.Unmarshal(raw, &inv)
	ids := map[string]map[uint16]bool{}
	for _, group := range []string{"cores", "gpus", "drives", "volumes", "interfaces", "batteries", "fans"} {
		var list []struct {
			ID uint16 `json:"id"`
		}
		_ = json.Unmarshal(inv[group], &list)
		if len(list) > 256 {
			return errf(422, "too_many_objects")
		}
		ids[group] = map[uint16]bool{}
		for _, obj := range list {
			if obj.ID == 0 || ids[group][obj.ID] {
				return errf(422, "duplicate_or_zero_object_id")
			}
			ids[group][obj.ID] = true
		}
	}
	// The full naming directory is implicit metadata, even when no dynamic
	// metrics are selected. Typed selected objects must agree with that directory.
	if len(p.Inventory.Objects) > 2048 {
		return errf(422, "too_many_objects")
	}
	directory := map[uint16]v2.ObjectDescriptor{}
	counts := map[string]int{}
	kinds := map[string]string{"core": "cores", "gpu": "gpus", "drive": "drives", "interface": "interfaces", "battery": "batteries", "fan": "fans", "volume": "volumes"}
	for _, o := range p.Inventory.Objects {
		if kinds[o.Kind] == "" || o.ID == 0 || directory[o.ID].ID != 0 {
			return errf(422, "invalid_object_directory")
		}
		if !utf8.ValidString(o.Name) || strings.TrimSpace(o.Name) == "" || len(o.Name) > 512 || strings.IndexFunc(o.Name, unicode.IsControl) >= 0 {
			return errf(422, "invalid_object_name")
		}
		if o.Model != nil && (!utf8.ValidString(*o.Model) || strings.TrimSpace(*o.Model) == "" || len(*o.Model) > 512 || strings.IndexFunc(*o.Model, unicode.IsControl) >= 0) {
			return errf(422, "invalid_object_model")
		}
		counts[o.Kind]++
		if counts[o.Kind] > 256 {
			return errf(422, "too_many_objects")
		}
		directory[o.ID] = o
	}
	if p.Inventory.Objects != nil {
		for kind, group := range kinds {
			for id := range ids[group] {
				if directory[id].Kind != kind {
					return errf(422, "object_directory_mismatch")
				}
			}
		}
	}
	expanded := 0
	for _, sel := range p.Enabled {
		if len(sel.Objects) == 0 {
			expanded++
			continue
		}
		group := map[string]string{"core": "cores", "gpu": "gpus", "drive": "drives", "interface": "interfaces", "battery": "batteries", "fan": "fans"}[strings.Split(sel.Metric, ".")[0]]
		seen := map[uint16]bool{}
		for _, id := range sel.Objects {
			if !ids[group][id] || seen[id] {
				return errf(422, "unknown_or_duplicate_selection_object")
			}
			seen[id] = true
			expanded++
		}
		if sel.Metric == "core.usage" && len(sel.Objects) != len(p.Inventory.Cores) {
			return errf(422, "all_cores_required")
		}
	}
	if expanded > 1024 {
		return errf(422, "too_many_selections")
	}
	// A fixed leaf must have an enabled declaration, except implicit memory_total.
	selected := selectionsOf(p)
	if p.Inventory.CPU != nil {
		c := p.Inventory.CPU
		for metric, present := range map[string]bool{"cpu.model": c.Model != nil, "cpu.physical_cores": c.PhysicalCores != nil, "cpu.logical_cores": c.LogicalCores != nil, "cpu.performance_cores": c.PerformanceCores != nil, "cpu.efficiency_cores": c.EfficiencyCores != nil} {
			if present && selected[metric] == nil {
				return errf(422, "inventory_not_enabled:"+metric)
			}
		}
	}
	if p.Inventory.OS != nil {
		os := p.Inventory.OS
		if selected["system.os"] == nil {
			return errf(422, "inventory_not_enabled:system.os")
		}
		if os.Family != "darwin" && os.Family != "windows" && os.Family != "linux" {
			return errf(422, "invalid_os_family")
		}
	}
	if p.Inventory.Product != nil && selected["system.product"] == nil {
		return errf(422, "inventory_not_enabled:system.product")
	}
	indexes := map[uint16]bool{}
	for _, c := range p.Inventory.Cores {
		if indexes[c.Index] {
			return errf(422, "duplicate_core_index")
		}
		indexes[c.Index] = true
		if c.Kind != nil && *c.Kind != "performance" && *c.Kind != "efficiency" && *c.Kind != "general" {
			return errf(422, "invalid_core_kind")
		}
	}
	for _, dr := range p.Inventory.Drives {
		if dr.Model != nil && !selectedFor(selected, "drive.model", dr.ID) || dr.TotalBytes != nil && !selectedFor(selected, "drive.total", dr.ID) {
			return errf(422, "inventory_not_enabled:drive")
		}
	}
	for _, nic := range p.Inventory.Interfaces {
		if nic.Medium != nil {
			if !selectedFor(selected, "interface.medium", nic.ID) {
				return errf(422, "inventory_not_enabled:interface.medium")
			}
			if *nic.Medium != "wifi" && *nic.Medium != "ethernet" && *nic.Medium != "other" {
				return errf(422, "invalid_interface_medium")
			}
		}
	}
	for _, fan := range p.Inventory.Fans {
		if fan.Scope != "cpu" && fan.Scope != "gpu" && fan.Scope != "system" {
			return errf(422, "invalid_fan_scope")
		}
		if fan.GPU != nil && !ids["gpus"][*fan.GPU] {
			return errf(422, "unknown_gpu")
		}
	}
	// Check dependencies that the Client always expands together.
	for _, sel := range p.Enabled {
		for _, id := range sel.Objects {
			dep := ""
			switch sel.Metric {
			case "gpu.temperature":
				dep = "gpu.usage"
			case "interface.upload_rate", "interface.download_rate", "interface.ipv4":
				dep = "interface.up"
			case "battery.level":
				dep = "battery.charging"
			case "battery.charging":
				dep = "battery.level"
			}
			if dep != "" && !selectedFor(selected, dep, id) {
				return errf(422, "selection_dependency_missing:"+dep)
			}
		}
	}
	var checkStrings func(reflect.Value) bool
	checkStrings = func(v reflect.Value) bool {
		if v.Kind() == reflect.Pointer {
			if v.IsNil() {
				return true
			}
			return checkStrings(v.Elem())
		}
		switch v.Kind() {
		case reflect.String:
			return utf8.ValidString(v.String()) && len(v.String()) <= 512
		case reflect.Struct:
			for i := 0; i < v.NumField(); i++ {
				if !checkStrings(v.Field(i)) {
					return false
				}
			}
		case reflect.Slice:
			for i := 0; i < v.Len(); i++ {
				if !checkStrings(v.Index(i)) {
					return false
				}
			}
		}
		return true
	}
	if !checkStrings(reflect.ValueOf(p.Inventory)) {
		return errf(422, "invalid_inventory_text")
	}
	return nil
}
func validateSampleShape(sm *v2.Sample) error {
	if sm.Memory != nil && sm.Memory.Pressure != nil && *sm.Memory.Pressure > 2 {
		return errf(422, "metric_out_of_range")
	}
	// All dynamic object groups require unique IDs, non-empty values and valid percentages.
	groups := []any{sm.Cores, sm.GPUs, sm.Drives, sm.Interfaces, sm.Batteries, sm.Fans}
	for _, group := range groups {
		list := reflect.ValueOf(group)
		if list.Len() > 256 {
			return errf(422, "too_many_objects")
		}
		seen := map[uint16]bool{}
		for i := 0; i < list.Len(); i++ {
			obj := list.Index(i)
			id := uint16(obj.FieldByName("ID").Uint())
			if id == 0 || seen[id] {
				return errf(422, "duplicate_or_zero_object_id")
			}
			seen[id] = true
			hasValue := false
			for j := 1; j < obj.NumField(); j++ {
				v := obj.Field(j)
				if v.Kind() == reflect.Pointer && !v.IsNil() {
					hasValue = true
					if (obj.Type().Field(j).Name == "Usage" || obj.Type().Field(j).Name == "Level") && v.Elem().Uint() > 10000 {
						return errf(422, "metric_out_of_range")
					}
				}
			}
			if !hasValue {
				return errf(422, "empty_object")
			}
		}
	}
	for _, c := range sm.Cores {
		if c.Usage == nil {
			return errf(422, "empty_core")
		}
	}
	for _, dr := range sm.Drives {
		if dr.SMART != nil {
			if dr.SMART.Healthy == nil {
				return errf(422, "empty_smart")
			}
			if dr.SMART.Timestamp < sm.Timestamp-900 {
				return errf(422, "observation_too_old")
			}
		}
	}
	for _, nic := range sm.Interfaces {
		if nic.IPv4 != nil {
			if len(*nic.IPv4) > 32 {
				return errf(422, "too_many_addresses")
			}
			seen := map[string]bool{}
			for _, v := range *nic.IPv4 {
				ip, err := netip.ParseAddr(v)
				if err != nil || !ip.Is4() || seen[v] {
					return errf(422, "invalid_ipv4")
				}
				seen[v] = true
			}
		}
	}
	if sm.PublicIPv4 != nil {
		ip, err := netip.ParseAddr(sm.PublicIPv4.Address)
		if err != nil || !ip.Is4() || !ip.IsGlobalUnicast() || ip.IsPrivate() {
			return errf(422, "invalid_public_ipv4")
		}
		if sm.PublicIPv4.Timestamp < sm.Timestamp-900 {
			return errf(422, "observation_too_old")
		}
	}
	if sm.CPU != nil && reflect.ValueOf(*sm.CPU).IsZero() {
		return errf(422, "empty_cpu")
	}
	if sm.Memory != nil && reflect.ValueOf(*sm.Memory).IsZero() {
		return errf(422, "empty_memory")
	}
	return nil
}

var unsignedDecimal = regexp.MustCompile(`^(0|[1-9][0-9]*)$`)

// Go's ,string decoder accepts some noncanonical integer spellings. The wire
// contract uses unsigned decimal text so exact byte values survive JS clients.
func validateWireStrings(raw []byte, dst any) error {
	var tree any
	if err := json.Unmarshal(raw, &tree); err != nil {
		return err
	}
	var visit func(any, reflect.Type) bool
	visit = func(node any, t reflect.Type) bool {
		for t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		if t == reflect.TypeOf(json.RawMessage{}) {
			return true
		}
		switch t.Kind() {
		case reflect.Struct:
			obj, ok := node.(map[string]any)
			if !ok {
				return false
			}
			// encoding/json otherwise accepts case-insensitive aliases for tags.
			// Reject aliases as unknown to avoid duplicate semantic keys.
			allowed := map[string]bool{}
			for i := 0; i < t.NumField(); i++ {
				allowed[strings.Split(t.Field(i).Tag.Get("json"), ",")[0]] = true
			}
			for key := range obj {
				if !allowed[key] {
					return false
				}
			}
			for i := 0; i < t.NumField(); i++ {
				field := t.Field(i)
				tag := strings.Split(field.Tag.Get("json"), ",")
				value, exists := obj[tag[0]]
				if !exists {
					continue
				}
				quoted := false
				for _, part := range tag[1:] {
					quoted = quoted || part == "string"
				}
				if quoted {
					v, ok := value.(string)
					if !ok || !unsignedDecimal.MatchString(v) {
						return false
					}
				} else if !visit(value, field.Type) {
					return false
				}
			}
		case reflect.Slice:
			list, ok := node.([]any)
			if !ok {
				return false
			}
			for _, v := range list {
				if !visit(v, t.Elem()) {
					return false
				}
			}
		}
		return true
	}
	if !visit(tree, reflect.TypeOf(dst)) {
		return errf(422, "invalid_integer_text")
	}
	return nil
}
