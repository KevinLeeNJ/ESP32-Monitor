package simserver

import (
	"encoding/json"
	"strconv"
	"strings"

	v2 "esp32-monitor/client/internal/collector/uploadv2"
)

// semantic IDs accepted in Profile.Enabled (docs/COLLECTOR_UPLOAD_V2.md §2).
var (
	fixedMetrics = map[string]bool{
		"cpu.model": true, "cpu.physical_cores": true, "cpu.logical_cores": true,
		"cpu.performance_cores": true, "cpu.efficiency_cores": true, "memory.total": true,
		"system.os": true, "system.product": true,
	}
	scalarMetrics = map[string]bool{
		"cpu.load1": true, "cpu.load5": true, "cpu.load15": true, "cpu.temperature": true,
		"memory.app": true, "memory.wired": true, "memory.compressed": true,
		"memory.swap_total": true, "memory.swap_used": true, "memory.pressure": true,
		"network.public_ipv4": true,
	}
	objectMetrics = map[string]bool{
		"core.usage": true, "gpu.usage": true, "gpu.temperature": true,
		"drive.model": true, "drive.total": true, "drive.used": true,
		"drive.read_rate": true, "drive.write_rate": true,
		"drive.smart_health": true,
		"interface.medium": true, "interface.up": true,
		"interface.upload_rate": true, "interface.download_rate": true,
		"interface.ipv4": true, "battery.level": true, "fan.rpm": true,
	}
	implicitMetrics = map[string]bool{
		"cpu.usage": true, "memory.usage": true, "memory.used": true, "system.uptime": true,
	}
	inventoryArrays = map[string]bool{
		"cores": true, "gpus": true, "drives": true, "volumes": true,
		"interfaces": true, "batteries": true, "fans": true,
	}
)

func validateProfile(p *v2.Profile) error {
	if p.Version != 2 {
		return errf(400, "invalid_profile_version")
	}
	if p.IntervalSeconds < 5 || p.IntervalSeconds > 60 {
		return errf(422, "invalid_interval")
	}
	seen := map[string]bool{}
	expanded := 0
	for _, sel := range p.Enabled {
		if seen[sel.Metric] {
			return errf(422, "duplicate_selection:"+sel.Metric)
		}
		seen[sel.Metric] = true
		switch {
		case implicitMetrics[sel.Metric]:
			return errf(422, "implicit_metric_selected:"+sel.Metric)
		case fixedMetrics[sel.Metric], scalarMetrics[sel.Metric]:
			if len(sel.Objects) > 0 {
				return errf(422, "objects_not_allowed:"+sel.Metric)
			}
		case objectMetrics[sel.Metric]:
			if len(sel.Objects) == 0 {
				return errf(422, "objects_required:"+sel.Metric)
			}
			for _, id := range sel.Objects {
				if id == 0 {
					return errf(422, "invalid_object_id:"+sel.Metric)
				}
			}
			expanded += len(sel.Objects)
		default:
			return errf(422, "unknown_metric:"+sel.Metric)
		}
	}
	if expanded > 1024 {
		return errf(422, "too_many_selections")
	}
	return nil
}

// profilesEqual compares canonical JSON, independent of property order.
func profilesEqual(a, b *v2.Profile) bool {
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return string(ab) == string(bb)
}

// applyPatch rebuilds the candidate profile from generic JSON, applies the
// operations in order (arrays are addressed by confirmed index, doc §4) and
// decodes the result back.
func applyPatch(current *v2.Profile, patch v2.ProfilePatch) (*v2.Profile, error) {
	raw, err := json.Marshal(current)
	if err != nil {
		return nil, errf(500, "profile_marshal")
	}
	var tree map[string]any
	if err := json.Unmarshal(raw, &tree); err != nil {
		return nil, errf(500, "profile_unmarshal")
	}
	for _, op := range patch {
		next, err := applyOp(tree, op)
		if err != nil {
			return nil, err
		}
		tree = next.(map[string]any)
	}
	patched, err := json.Marshal(tree)
	if err != nil {
		return nil, errf(500, "patch_marshal")
	}
	var candidate v2.Profile
	if err := json.Unmarshal(patched, &candidate); err != nil {
		return nil, errf(422, "invalid_patch_result")
	}
	return &candidate, nil
}

func applyOp(tree map[string]any, op v2.ProfileOperation) (any, error) {
	switch op.Op {
	case "add", "replace", "remove":
	default:
		return nil, errf(422, "invalid_patch_op:"+op.Op)
	}
	if op.Op == "remove" && len(op.Value) > 0 {
		return nil, errf(422, "remove_with_value")
	}
	if op.Op != "remove" && len(op.Value) == 0 {
		return nil, errf(422, "missing_value")
	}
	segments, err := parsePointer(op.Path)
	if err != nil {
		return nil, err
	}
	if err := checkPath(op, segments); err != nil {
		return nil, err
	}
	var value any
	if len(op.Value) > 0 {
		if err := json.Unmarshal(op.Value, &value); err != nil {
			return nil, errf(422, "invalid_patch_value")
		}
	}
	return applyAt(tree, segments, op.Op, value, op.Path)
}

func parsePointer(path string) ([]string, error) {
	if path == "" || path[0] != '/' {
		return nil, errf(422, "invalid_patch_path:"+path)
	}
	parts := strings.Split(path[1:], "/")
	for i, p := range parts {
		p = strings.ReplaceAll(p, "~1", "/")
		p = strings.ReplaceAll(p, "~0", "~")
		parts[i] = p
	}
	return parts, nil
}

// checkPath enforces the restricted pointer grammar: /enabled, /interval_seconds
// and /inventory scalar leaves or object-array members. Replacing /inventory
// wholesale or the CPU / OS info groups is forbidden (doc §4).
func checkPath(op v2.ProfileOperation, segments []string) error {
	switch segments[0] {
	case "enabled":
		if len(segments) > 2 {
			return errf(422, "invalid_patch_path:"+op.Path)
		}
		if len(segments) == 2 && op.Op == "add" && segments[1] != "-" {
			return errf(422, "add_requires_dash:"+op.Path)
		}
		return nil
	case "interval_seconds":
		if len(segments) != 1 || op.Op != "replace" {
			return errf(422, "invalid_patch_path:"+op.Path)
		}
		return nil
	case "inventory":
		if len(segments) < 2 {
			return errf(422, "inventory_replacement_forbidden")
		}
		group := segments[1]
		if group == "cpu" || group == "os" {
			if len(segments) == 2 {
				return errf(422, "inventory_group_replacement_forbidden:"+group)
			}
			return nil
		}
		if inventoryArrays[group] {
			if len(segments) == 2 {
				return errf(422, "inventory_group_replacement_forbidden:"+group)
			}
			if len(segments) > 3 && segments[2] == "-" {
				return errf(422, "invalid_patch_path:"+op.Path)
			}
			if len(segments) == 3 && op.Op == "add" && segments[2] != "-" {
				return errf(422, "add_requires_dash:"+op.Path)
			}
			return nil
		}
		if len(segments) != 2 || op.Op == "add" {
			return errf(422, "invalid_patch_path:"+op.Path)
		}
		return nil
	case "version":
		return errf(422, "version_immutable")
	default:
		return errf(422, "invalid_patch_path:"+op.Path)
	}
}

// applyAt applies one operation and returns the (possibly reallocated) node so
// parents can store slice header changes back.
func applyAt(node any, segments []string, op string, value any, path string) (any, error) {
	seg := segments[0]
	last := len(segments) == 1
	switch cur := node.(type) {
	case map[string]any:
		if !last {
			child, ok := cur[seg]
			if !ok {
				if op != "add" {
					return nil, errf(422, "patch_target_missing:"+path)
				}
				// omitempty groups (inventory sublists, /enabled) may be absent;
				// an add descending into them creates the container first.
				if len(segments) > 1 && segments[1] == "-" {
					child = []any{}
				} else {
					child = map[string]any{}
				}
				cur[seg] = child
			}
			next, err := applyAt(child, segments[1:], op, value, path)
			if err != nil {
				return nil, err
			}
			cur[seg] = next
			return node, nil
		}
		switch op {
		case "add", "replace":
			cur[seg] = value
		case "remove":
			if _, ok := cur[seg]; !ok {
				return nil, errf(422, "patch_target_missing:"+path)
			}
			delete(cur, seg)
		}
		return node, nil
	case []any:
		if seg == "-" {
			if !last || op != "add" {
				return nil, errf(422, "append_requires_add:"+path)
			}
			return append(cur, value), nil
		}
		idx, err := strconv.Atoi(seg)
		if err != nil || idx < 0 || idx >= len(cur) {
			return nil, errf(422, "patch_index_out_of_range:"+path)
		}
		if !last {
			next, err := applyAt(cur[idx], segments[1:], op, value, path)
			if err != nil {
				return nil, err
			}
			cur[idx] = next
			return node, nil
		}
		switch op {
		case "replace":
			cur[idx] = value
			return node, nil
		case "remove":
			return append(cur[:idx], cur[idx+1:]...), nil
		default:
			return nil, errf(422, "add_requires_dash:"+path)
		}
	default:
		return nil, errf(422, "invalid_patch_target:"+path)
	}
}
