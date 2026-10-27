//go:build darwin

package collector

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
)

// Only APFS-only physical drives are eligible: every partition must have a
// uniquely attributable single-store container. Shared stores are never split.
func physicalAPFSPartitions(value any) map[string][]string {
	root, _ := value.(map[string]any)
	disks, _ := root["AllDisksAndPartitions"].([]any)
	out := map[string][]string{}
	for _, entry := range disks {
		disk, _ := entry.(map[string]any)
		name, _ := disk["DeviceIdentifier"].(string)
		parts, _ := disk["Partitions"].([]any)
		if name == "" || len(parts) == 0 {
			continue
		}
		ids := []string{}
		seen := map[string]bool{}
		valid := true
		for _, entry := range parts {
			part, _ := entry.(map[string]any)
			id, _ := part["DeviceIdentifier"].(string)
			content, _ := part["Content"].(string)
			if id == "" || seen[id] || !strings.HasPrefix(content, "Apple_APFS") {
				valid = false
				break
			}
			seen[id] = true
			ids = append(ids, id)
		}
		if valid {
			out[name] = ids
		}
	}
	return out
}
func plistUint(value any) (uint64, bool) {
	switch v := value.(type) {
	case float64:
		if v >= 0 && v <= 9007199254740991 && math.Trunc(v) == v {
			return uint64(v), true
		}
	case json.Number:
		n, e := strconv.ParseUint(string(v), 10, 64)
		return n, e == nil
	}
	return 0, false
}
func apfsDiskUsage(value any, partitions map[string][]string) map[string]uint64 {
	root, _ := value.(map[string]any)
	containers, _ := root["Containers"].([]any)
	type reading struct {
		used  uint64
		valid bool
	}
	byStore := map[string]reading{}
	for _, entry := range containers {
		container, _ := entry.(map[string]any)
		stores, _ := container["PhysicalStores"].([]any)
		total, hasTotal := plistUint(container["CapacityCeiling"])
		free, hasFree := plistUint(container["CapacityFree"])
		for _, entry := range stores {
			store, _ := entry.(map[string]any)
			id, _ := store["DeviceIdentifier"].(string)
			if id == "" {
				continue
			}
			_, duplicate := byStore[id]
			valid := len(stores) == 1 && hasTotal && hasFree && free <= total && !duplicate
			used := uint64(0)
			if valid {
				used = total - free
			}
			byStore[id] = reading{used, valid}
		}
	}
	out := map[string]uint64{}
	for disk, ids := range partitions {
		var used uint64
		valid := len(ids) > 0
		for store := range byStore {
			if strings.HasPrefix(store, disk+"s") {
				known := false
				for _, id := range ids {
					if id == store {
						known = true
						break
					}
				}
				if !known {
					valid = false
					break
				}
			}
		}
		for _, id := range ids {
			r, ok := byStore[id]
			if !ok || !r.valid || math.MaxUint64-used < r.used {
				valid = false
				break
			}
			used += r.used
		}
		if valid {
			out[disk] = used
		}
	}
	return out
}
