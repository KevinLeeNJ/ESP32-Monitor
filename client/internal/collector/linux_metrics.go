//go:build linux

package collector

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// nvtop's snapshot identifies cards by name only. Repeated names cannot be
// associated reliably across samples and are therefore omitted.
func parseLinuxNVTop(data []byte) []Metric {
	var devices []struct {
		Name string `json:"device_name"`
		Util string `json:"gpu_util"`
		Temp string `json:"temp"`
	}
	if json.Unmarshal(data, &devices) != nil {
		return nil
	}
	counts := map[string]int{}
	for _, d := range devices {
		counts[strings.ToLower(strings.TrimSpace(d.Name))]++
	}
	var metrics []Metric
	for _, d := range devices {
		name := strings.TrimSpace(d.Name)
		key := strings.ToLower(name)
		if name == "" || counts[key] != 1 {
			continue
		}
		fields := map[string]float64{}
		if strings.HasSuffix(d.Util, "%") {
			if v, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(d.Util, "%")), 64); err == nil && !math.IsNaN(v) && v >= 0 && v <= 100 {
				fields["usage_percent"] = v
			}
		}
		temp := strings.TrimSpace(d.Temp)
		if strings.HasSuffix(temp, "C") || strings.HasSuffix(temp, "F") {
			fahrenheit := strings.HasSuffix(temp, "F")
			if v, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(temp, "C"), "F")), 64); err == nil {
				if fahrenheit {
					v = (v - 32) * 5 / 9
				}
				if !math.IsNaN(v) && v > -50 && v < 200 {
					fields["temperature_celsius"] = v
				}
			}
		}
		if len(fields) == 0 {
			continue
		}
		hash := sha256.Sum256([]byte(key))
		metrics = append(metrics, Metric{Tags: map[string]string{"device": fmt.Sprintf("nvtop-%x", hash[:16]), "model": name}, Fields: fields})
	}
	return metrics
}

type linuxBlockDevice struct {
	Name     string             `json:"name"`
	Type     string             `json:"type"`
	Size     uint64             `json:"size"`
	FSType   string             `json:"fstype"`
	UUID     string             `json:"uuid"`
	Used     *uint64            `json:"fsused"`
	Children []linuxBlockDevice `json:"children"`
}

type linuxDiskStat struct {
	Used  uint64
	Total uint64
}

// Sum usage and capacity for mounted filesystems with an unambiguous physical parent.
func linuxDiskUsage(data []byte, btrfsRoot string) map[string]linuxDiskStat {
	var result struct {
		Devices []linuxBlockDevice `json:"blockdevices"`
	}
	if json.Unmarshal(data, &result) != nil {
		return nil
	}
	uuids := map[string]int{}
	var count func(linuxBlockDevice)
	count = func(d linuxBlockDevice) {
		if d.UUID != "" {
			uuids[d.UUID]++
		}
		for _, child := range d.Children {
			count(child)
		}
	}
	for _, d := range result.Devices {
		count(d)
	}
	out := map[string]linuxDiskStat{}
	for _, disk := range result.Devices {
		if disk.Type != "disk" || disk.Name == "" || disk.Size == 0 {
			continue
		}
		var used, capacity uint64
		found, valid := false, true
		var visit func(linuxBlockDevice)
		visit = func(d linuxBlockDevice) {
			if d.Type != "disk" && d.Type != "part" {
				valid = false
				return
			}
			if len(d.Children) == 0 && d.Used == nil && d.Type == "part" {
				return
			}
			if d.UUID != "" && uuids[d.UUID] != 1 {
				valid = false
				return
			}
			if d.FSType == "btrfs" {
				if d.UUID == "" || filepath.Base(d.UUID) != d.UUID {
					valid = false
					return
				}
				devices, err := os.ReadDir(filepath.Join(btrfsRoot, d.UUID, "devices"))
				if err != nil || len(devices) != 1 {
					valid = false
					return
				}
			}
			if d.FSType != "" && d.FSType != "swap" {
				if d.Used == nil || d.UUID == "" || uuids[d.UUID] != 1 || *d.Used > d.Size || *d.Used > disk.Size ||
					used > disk.Size-*d.Used || capacity > disk.Size-d.Size {
					valid = false
					return
				}
				used += *d.Used
				capacity += d.Size
				found = true
			}
			for _, child := range d.Children {
				visit(child)
			}
		}
		visit(disk)
		if valid && found {
			out[disk.Name] = linuxDiskStat{Used: used, Total: capacity}
		}
	}
	return out
}
