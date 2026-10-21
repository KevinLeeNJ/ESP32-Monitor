package panel

import (
	"encoding/json"
	"math"
	"testing"

	"esp32-monitor/server/internal/config"
	"esp32-monitor/server/internal/metrics"
	"esp32-monitor/server/internal/storage"
)

const now int64 = 1792454400

func cfg() config.Config {
	c := config.Defaults()
	c.Panel.Devices = []config.Device{config.DefaultDevice("a", "NAS", "/data"), config.DefaultDevice("b", "PC", "/")}
	return c
}
func value(m, f, tags string, ts int64, v float64) metrics.Sample {
	return metrics.Sample{Measurement: m, Field: f, TagsJSON: tags, SampledAt: ts, Value: v}
}
func TestProjectFixedFieldsAndFreshness(t *testing.T) {
	seen := now
	state := storage.Snapshot{"a": {LastSeen: &seen, Samples: []metrics.Sample{
		value("cpu", "usage_active", `{"cpu":"cpu-total"}`, now, 26.5), value("cpu", "usage_active", `{"cpu":"cpu0"}`, now, 90),
		value("mem", "used_percent", `{}`, now-45, 42.1), value("disk", "used_percent", `{"path":"/data","device":"sda"}`, now+30, 61),
		value("disk", "used_percent", `{"path":"/"}`, now, 99), value("system", "uptime", `{}`, now, 12*86400+4*3600),
	}}, "b": {}}
	result := Project(cfg(), state, now)
	if result.DeviceCount != 1 || result.Devices[0].Name != "NAS" || result.Devices[0].Uptime != "12d 4h" {
		t.Fatal(result)
	}
	for i, want := range []Metric{{Name: "CPU", Value: "27%"}, {Name: "MEM", Value: "42%"}, {Name: "DISK", Value: "61%"}} {
		got := result.Devices[0].Metrics[i]
		if got.Name != want.Name || got.Value != want.Value || got.Percent == nil {
			t.Fatal(got)
		}
	}
	// Agent remains reachable after old-only replay, but every metric expires separately.
	result = Project(cfg(), state, now+46)
	if result.DeviceCount != 0 {
		t.Fatal("offline card returned")
	}
	later := now + 46
	d := state["a"]
	d.LastSeen = &later
	state["a"] = d
	result = Project(cfg(), state, later)
	if result.Devices[0].Metrics[0].Value != "--" || result.Devices[0].Metrics[1].Percent != nil || result.Devices[0].Uptime != "--" || result.Devices[0].Metrics[2].Value != "61%" {
		t.Fatal(result)
	}
}
func TestAmbiguousSourcesAndStableOrder(t *testing.T) {
	seen := now
	ambiguous := []metrics.Sample{value("disk", "used_percent", `{"path":"/data","device":"a"}`, now, 1), value("disk", "used_percent", `{"path":"/data","device":"b"}`, now, 2), value("mem", "used_percent", `{"x":"a"}`, now, 1), value("mem", "used_percent", `{"x":"b"}`, now, 2)}
	s := storage.Snapshot{"a": {LastSeen: &seen, Samples: ambiguous}, "b": {LastSeen: &seen}}
	result := Project(cfg(), s, now)
	if result.DeviceCount != 2 || result.Devices[0].Name != "NAS" || result.Devices[1].Name != "PC" || result.Devices[0].MetricCount != 3 {
		t.Fatal(result)
	}
	for _, m := range result.Devices[0].Metrics {
		if m.Value != "--" || m.Percent != nil {
			t.Fatal(m)
		}
	}
	c := cfg()
	c.Panel.Devices[0], c.Panel.Devices[1] = c.Panel.Devices[1], c.Panel.Devices[0]
	if Project(c, s, now).Devices[0].Name != "PC" {
		t.Fatal("order ignored")
	}
	for _, empty := range []Summary{Project(c, nil, now), Project(config.Defaults(), s, now)} {
		b, err := json.Marshal(empty)
		if err != nil || string(b) != `{"device_count":0,"devices":[]}` {
			t.Fatal(string(b), err)
		}
	}
}
func TestFormats(t *testing.T) {
	for _, tt := range []struct {
		v       float64
		valid   bool
		text    string
		percent int
	}{{0, true, "0%", 0}, {99.5, true, "100%", 100}, {-1, true, "-1%", 0}, {101, true, "101%", 100}, {1e20, true, "OVR", -1}, {0, false, "--", -1}, {math.Inf(1), true, "--", -1}} {
		m := FormatPercent("CPU", tt.v, tt.valid)
		if m.Value != tt.text || tt.percent < 0 && m.Percent != nil || tt.percent >= 0 && (m.Percent == nil || *m.Percent != tt.percent) {
			t.Fatal(tt, m)
		}
	}
	for _, tt := range []struct {
		v    float64
		text string
	}{{0, "0s"}, {59, "59s"}, {60, "1m 0s"}, {3599, "59m 59s"}, {3600, "1h 0m"}, {86399, "23h 59m"}, {86400, "1d 0h"}, {-1, "--"}, {1.5, "--"}, {1e30, "OVR"}, {math.NaN(), "--"}} {
		if got := FormatUptime(tt.v, true); got != tt.text || len(got) > 8 {
			t.Fatal(tt, got)
		}
	}
	if FormatUptime(10, false) != "--" {
		t.Fatal("missing uptime")
	}
}
func TestMaximumSummarySize(t *testing.T) {
	c := cfg()
	c.Panel.Devices = nil
	s := storage.Snapshot{}
	seen := now
	for _, id := range []string{"a", "b", "c", "d"} {
		c.Panel.Devices = append(c.Panel.Devices, config.DefaultDevice(id, "12345678", "/"))
		s[id] = storage.DeviceState{LastSeen: &seen, Samples: []metrics.Sample{value("system", "uptime", `{}`, now, 9999*86400+23*3600)}}
	}
	body, err := json.Marshal(Project(c, s, now))
	if err != nil || len(body) > 4096 {
		t.Fatal(len(body), err)
	}
}

func TestAmbiguityIsNotResolvedByExpiringOneCandidate(t *testing.T) {
	seen := now
	state := storage.Snapshot{"a": {LastSeen: &seen, Samples: []metrics.Sample{
		value("disk", "used_percent", `{"path":"/data","device":"old"}`, now-46, 1),
		value("disk", "used_percent", `{"path":"/data","device":"new"}`, now, 2),
	}}}
	if got := Project(cfg(), state, now).Devices[0].Metrics[2]; got.Value != "--" || got.Percent != nil {
		t.Fatal("ambiguous source selected", got)
	}
}
