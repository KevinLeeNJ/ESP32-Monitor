package transport

import (
	"esp32-monitor/client/internal/transport/wire"
	"testing"
)

type semanticsFixture struct {
	age       int64
	timeMS    int64
	today     int
	percent   float64
	task      string
	condition int
	metric    string
	offset    int
}

func fixtureSections(c semanticsFixture) wire.Sections {
	observed, last := int64(1700000000), int64(1700000000)
	section := func(state string, stale, expire int64, data any) wire.Section {
		age := c.age
		return wire.Section{Health: wire.Health{State: state, ObservedAt: &observed, LastSuccessAt: &last,
			AgeMS: &age, StaleAfterMS: stale, ExpireAfterMS: expire}, Data: data}
	}
	return wire.Sections{
		GitHub: section("healthy", 86400000, 604800000, map[string]any{
			"login": "monitor", "start_date": "2026-03-16", "days": 7, "levels": "0123400",
			"today_count": c.today, "total_count": 10,
		}),
		Codex: section("healthy", 15000, 15000, map[string]any{
			"quota": section("healthy", 180000, 300000, map[string]any{
				"windows":   []any{map[string]any{"bucket_id": "codex", "window_minutes": int64(300), "used_percent": c.percent}},
				"truncated": false,
			}),
			"tasks": section("healthy", 15000, 15000, map[string]any{
				"adapter": "codex", "scope": "local", "state": c.task, "active_count": 1,
				"selected_task": map[string]any{"id": "abcdef", "state": c.task, "observed_at": observed},
			}),
		}),
		Music: section("healthy", 15000, 15000, map[string]any{
			"adapter": "mediaremote", "coverage": "system_current_source",
			"current": map[string]any{"state": "not_playing", "title": "Blue", "artist": "Yung Kai",
				"source": "Spotify", "duration": 225.0, "position": 15.0, "position_observed_at": observed},
		}),
		Proxy: section("healthy", 15000, 15000, wire.ProxyData{
			Epoch: "epoch", Revision: 1,
			Time:    section("healthy", 45000, 60000, wire.TimeData{UnixMS: c.timeMS, Offset: c.offset, Source: "system_clock"}),
			Weather: section("healthy", 1800000, 7200000, map[string]any{"condition": c.condition, "latitude": 1.0, "longitude": 2.0}),
			AQI:     section("healthy", 1800000, 7200000, map[string]any{"value": 42, "scale": "qweather_aqi"}),
			Devices: section("healthy", 45000, 90000, wire.DeviceSummary{Count: 1, Devices: []wire.DeviceCard{{
				Name: "NAS", Uptime: "3d", MetricCount: 3,
				Metrics: []wire.Metric{{Name: "CPU", Value: c.metric}, {Name: "MEM", Value: "20%"}, {Name: "DISK", Value: "30%"}},
			}}}),
		}),
	}
}

func TestSemanticIgnoresTimePassageAndObservationMetadata(t *testing.T) {
	a := fixtureSections(semanticsFixture{age: 1000, timeMS: 1000, today: 2, percent: 10,
		task: "running", condition: 1, metric: "12%", offset: 480})
	b := fixtureSections(semanticsFixture{age: 59000, timeMS: 590001, today: 2, percent: 10,
		task: "running", condition: 1, metric: "12%", offset: 480})
	if semantic(a) != semantic(b) {
		t.Fatal("metadata-only ageing or clock movement produced a snapshot")
	}
}

func TestSemanticDetectsContentChanges(t *testing.T) {
	base := semanticsFixture{today: 2, percent: 10, task: "running", condition: 1, metric: "12%", offset: 480}
	a := fixtureSections(base)
	cases := []struct {
		name   string
		change func(*semanticsFixture)
	}{
		{"github", func(c *semanticsFixture) { c.today = 3 }},
		{"codex quota", func(c *semanticsFixture) { c.percent = 11 }},
		{"codex tasks", func(c *semanticsFixture) { c.task = "waiting_input" }},
		{"weather", func(c *semanticsFixture) { c.condition = 2 }},
		{"devices", func(c *semanticsFixture) { c.metric = "13%" }},
		{"time zone", func(c *semanticsFixture) { c.offset = 540 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := base
			tc.change(&c)
			if semantic(a) == semantic(fixtureSections(c)) {
				t.Fatalf("%s change was suppressed", tc.name)
			}
		})
	}
}
