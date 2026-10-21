// Package panel converts persisted metrics into the small ESP32 display contract.
package panel

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"

	"esp32-monitor/server/internal/config"
	"esp32-monitor/server/internal/metrics"
	"esp32-monitor/server/internal/storage"
)

type Metric struct {
	Name    string `json:"name"`
	Value   string `json:"value"`
	Percent *int   `json:"percent"`
}
type Device struct {
	Name        string   `json:"name"`
	Uptime      string   `json:"uptime"`
	MetricCount int      `json:"metric_count"`
	Metrics     []Metric `json:"metrics"`
}
type Summary struct {
	DeviceCount int      `json:"device_count"`
	Devices     []Device `json:"devices"`
}

func Project(c config.Config, snapshot storage.Snapshot, now int64) Summary {
	out := Summary{Devices: make([]Device, 0, len(c.Panel.Devices))}
	for _, d := range c.Panel.Devices {
		state, ok := snapshot[d.ID]
		if !ok || state.LastSeen == nil || now-*state.LastSeen > c.OnlineTimeoutSeconds {
			continue
		}
		cpu, okCPU := selectValue(state.Samples, "cpu", "usage_active", map[string]string{"cpu": "cpu-total"}, now, c.MetricTTLSeconds)
		mem, okMem := selectValue(state.Samples, "mem", "used_percent", nil, now, c.MetricTTLSeconds)
		disk, okDisk := selectValue(state.Samples, "disk", "used_percent", map[string]string{"path": d.DiskMountPath}, now, c.MetricTTLSeconds)
		uptime, okUptime := selectValue(state.Samples, "system", "uptime", nil, now, c.MetricTTLSeconds)
		out.Devices = append(out.Devices, Device{Name: d.Name, Uptime: FormatUptime(uptime, okUptime), MetricCount: 3, Metrics: []Metric{FormatPercent("CPU", cpu, okCPU), FormatPercent("MEM", mem, okMem), FormatPercent("DISK", disk, okDisk)}})
	}
	out.DeviceCount = len(out.Devices)
	return out
}
func selectValue(samples []metrics.Sample, measurement, field string, tags map[string]string, now, ttl int64) (float64, bool) {
	found := false
	var selected metrics.Sample
	for _, s := range samples {
		if s.Measurement != measurement || s.Field != field {
			continue
		}
		var actual map[string]string
		if json.Unmarshal([]byte(s.TagsJSON), &actual) != nil {
			continue
		}
		match := true
		for k, v := range tags {
			if actual[k] != v {
				match = false
				break
			}
		}
		if !match {
			continue
		}
		if found {
			return 0, false
		}
		found = true
		selected = s
	}
	return selected.Value, found && selected.SampledAt >= now-ttl && selected.SampledAt <= now+30
}
func FormatPercent(name string, v float64, valid bool) Metric {
	m := Metric{Name: name, Value: "--"}
	if !valid || math.IsNaN(v) || math.IsInf(v, 0) {
		return m
	}
	rounded := math.Round(v)
	text := strconv.FormatFloat(rounded, 'f', 0, 64) + "%"
	if len(text) > 8 {
		m.Value = "OVR"
		return m
	}
	p := int(math.Max(0, math.Min(100, rounded)))
	m.Value = text
	m.Percent = &p
	return m
}
func FormatUptime(v float64, valid bool) string {
	if !valid || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v != math.Trunc(v) {
		return "--"
	}
	// Check before conversion to avoid overflowing int64. Even 100000 days exceeds the label.
	if v >= 100000*86400 {
		return "OVR"
	}
	n := int64(v)
	var text string
	switch {
	case n >= 86400:
		text = fmt.Sprintf("%dd %dh", n/86400, n/3600%24)
	case n >= 3600:
		text = fmt.Sprintf("%dh %dm", n/3600, n/60%60)
	case n >= 60:
		text = fmt.Sprintf("%dm %ds", n/60, n%60)
	default:
		text = fmt.Sprintf("%ds", n)
	}
	if len(text) > 8 {
		return "OVR"
	}
	return text
}
