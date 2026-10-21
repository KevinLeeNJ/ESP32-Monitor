//go:build integration

package integration_test

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"testing"

	"esp32-monitor/server/internal/config"
)

func TestCapturedMacOSTelegrafBatch(t *testing.T) {
	// Captured HTTP body from Telegraf 1.40.2 on macOS arm64, not generated
	// by the server. Disk device tags are redacted; timestamps/values are retained.
	body, err := os.ReadFile("../e2e/evidence/macos/macos-real-batch.json")
	if err != nil {
		t.Fatal(err)
	}
	var captured struct {
		Metrics []struct {
			Name      string             `json:"name"`
			Fields    map[string]float64 `json:"fields"`
			Timestamp int64              `json:"timestamp"`
		} `json:"metrics"`
	}
	if err = json.Unmarshal(body, &captured); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, func(c *config.Config) {
		c.Panel.Devices = []config.Device{config.DefaultDevice("a", "MAC", "/")}
	})
	var now int64
	for _, metric := range captured.Metrics {
		if metric.Timestamp > now {
			now = metric.Timestamp
		}
	}
	h.clock.Store(now) // Replay the historical fixture without rewriting its timestamps.
	requireStatus(t, h.post(t, "a", body), 204)
	summary := h.summary(t)
	if summary.DeviceCount != 1 {
		t.Fatal("captured device missing")
	}
	for i, source := range []struct{ name, field string }{
		{"cpu", "usage_active"}, {"mem", "used_percent"}, {"disk", "used_percent"},
	} {
		var value float64
		var sampledAt int64
		for _, metric := range captured.Metrics {
			if v, ok := metric.Fields[source.field]; ok && metric.Name == source.name && metric.Timestamp > sampledAt {
				value, sampledAt = v, metric.Timestamp
			}
		}
		got := summary.Devices[0].Metrics[i]
		expected := int(math.Round(value))
		if sampledAt == 0 || got.Value != fmt.Sprintf("%d%%", expected) || got.Percent == nil || *got.Percent != expected {
			t.Fatalf("real %s field not projected correctly: %+v", source.name, got)
		}
	}
	if summary.Devices[0].Uptime != "1d 11h" || h.count(t, "metric_latest") != 29 || h.count(t, "metric_history") != 76 {
		t.Fatal("captured uptime or numeric field persistence changed")
	}
	before := h.dump(t)
	requireStatus(t, h.post(t, "a", body), 204)
	h.assertUnchanged(t, before)
}
