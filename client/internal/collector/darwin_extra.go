//go:build darwin

package collector

import (
	"context"
	"strconv"
	"strings"
	"time"
)

func readGPU(ctx context.Context) ([]Metric, error) {
	s, e := darwinCommand(ctx, "/usr/sbin/ioreg", "-r", "-c", "IOAccelerator", "-a")
	if e != nil {
		return nil, e
	}
	v, e := decodePlist(s)
	if e != nil {
		return nil, e
	}
	list, _ := v.([]any)
	out := []Metric{}
	for index, item := range list {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		stats, _ := obj["PerformanceStatistics"].(map[string]any)
		if stats == nil {
			continue
		}
		fields := map[string]float64{}
		for _, k := range []string{"Device Utilization %", "GPU Activity(%)"} {
			if n, ok := stats[k].(float64); ok && n >= 0 {
				fields["usage_percent"] = min(100, n)
				break
			}
		}
		if n, ok := stats["Temperature(C)"].(float64); ok && n > 0 && n < 110 {
			fields["temperature_celsius"] = n
		}
		if len(fields) > 0 {
			class, _ := obj["IOClass"].(string)
			if class == "" {
				class = "gpu"
			}
			out = append(out, Metric{Name: "darwin_gpu", Tags: map[string]string{"device": class + "-" + strconv.Itoa(index)}, Fields: fields, Timestamp: time.Now().Unix()})
		}
	}
	return out, nil
}
func readDisks(ctx context.Context) ([]Metric, error) {
	s, e := darwinCommand(ctx, "/usr/sbin/diskutil", "list", "-plist", "physical")
	if e != nil {
		return nil, e
	}
	v, e := decodePlist(s)
	if e != nil {
		return nil, e
	}
	root, _ := v.(map[string]any)
	disks, _ := root["WholeDisks"].([]any)
	devices := []string{}
	for _, item := range disks {
		if device, ok := item.(string); ok {
			devices = append(devices, device)
		}
	}
	return readDisksFor(ctx, devices, true)
}

func readDisksFor(ctx context.Context, devices []string, fixed bool) ([]Metric, error) {
	out := []Metric{}

	for _, disk := range devices {
		if !strings.HasPrefix(disk, "disk") {
			continue
		}
		s, e := darwinCommand(ctx, "/usr/sbin/diskutil", "info", "-plist", disk)
		if e != nil {
			continue
		}
		v, e := decodePlist(s)
		if e != nil {
			continue
		}
		info, _ := v.(map[string]any)
		if info == nil {
			continue
		}
		tags := map[string]string{"device": disk}
		model := ""
		if fixed {
			model, _ = info["MediaName"].(string)
		}
		if fixed && model != "" {
			tags["model"] = model
		}
		fields := map[string]float64{}
		exact := map[string]string{}
		if fixed {
			if n, ok := plistUint(info["TotalSize"]); ok {
				fields["total"] = float64(n)
				exact["total"] = strconv.FormatUint(n, 10)
			}
		}
		if status, ok := info["SMARTStatus"].(string); ok {
			switch strings.ToLower(status) {
			case "verified":
				fields["smart_healthy"] = 1
			case "failing":
				fields["smart_healthy"] = 0
			}
		}
		if fixed && model != "" {
			fields["present"] = 1
		}
		if len(fields) > 0 || len(exact) > 0 {
			out = append(out, Metric{Name: "darwin_disk", Tags: tags, Fields: fields, Exact: exact, Timestamp: time.Now().Unix()})
		}
	}
	return out, nil
}
func parseSwVers(s string) (name, version, build string) {
	for _, line := range strings.Split(s, "\n") {
		key, value, _ := strings.Cut(line, ":")
		switch strings.TrimSpace(key) {
		case "ProductName":
			name = strings.TrimSpace(value)
		case "ProductVersion":
			version = strings.TrimSpace(value)
		case "BuildVersion":
			build = strings.TrimSpace(value)
		}
	}
	return name, version, build
}

// macOS marketing names keyed by major version, adapted from fastfetch
// (src/detection/os/os_apple.m, MIT license, (c) fastfetch-cli contributors).
var darwinOSCodenames = map[int]string{
	27: "Golden Gate", 26: "Tahoe", 16: "Tahoe", 15: "Sequoia", 14: "Sonoma", 13: "Ventura", 12: "Monterey", 11: "Big Sur",
}

func darwinOSCodename(version string) string {
	major, _, _ := strings.Cut(version, ".")
	n, err := strconv.Atoi(major)
	if err != nil {
		return ""
	}
	return darwinOSCodenames[n]
}
