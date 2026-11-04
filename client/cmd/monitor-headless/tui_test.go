package main

import (
	"slices"
	"strings"
	"testing"

	"esp32-monitor/client/internal/collector"
	v2 "esp32-monitor/client/internal/collector/uploadv2"
	tea "github.com/charmbracelet/bubbletea"
)

func TestMetricMenuBundlesAndRAID(t *testing.T) {
	caps := &collector.V2Capabilities{Objects: []collector.V2Object{{ID: 7, Kind: "drive", Name: "md127 RAID5"}}, Metrics: []collector.V2Metric{
		{Metric: "cpu.usage", Available: true, Required: true}, {Metric: "memory.usage", Available: true, Required: true}, {Metric: "system.uptime", Available: true, Required: true},
		{Metric: "cpu.model", Available: true}, {Metric: "cpu.logical_cores", Available: true}, {Metric: "cpu.load1", Available: true}, {Metric: "cpu.load5", Available: true}, {Metric: "cpu.load15", Available: true},
		{Metric: "drive.model", Available: true, Objects: []uint16{7}}, {Metric: "drive.total", Available: true, Objects: []uint16{7}}, {Metric: "drive.used", Available: true, Objects: []uint16{7}},
		{Metric: "drive.read_rate", Available: true, Objects: []uint16{7}}, {Metric: "drive.write_rate", Available: true, Objects: []uint16{7}}, {Metric: "drive.smart_health", Available: true, Objects: []uint16{7}},
	}}
	previous := &v2.UploadConfig{Enabled: true, IntervalSeconds: 30, Optional: []v2.UploadSelection{{Metric: "cpu.load1"}, {Metric: "drive.model", Objects: []uint16{7}}}}
	m, err := makeMetrics(caps, previous)
	if err != nil {
		t.Fatal(err)
	}
	if !m.rows[0].required || !strings.Contains(m.View(), "总体 CPU 占用率") {
		t.Fatal("missing required CPU metric")
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = next.(metricsModel)
	if !m.rows[0].required || m.rows[0].selected[0] {
		t.Fatal("required selection changed")
	}
	m.group = slices.Index(m.groups, "磁盘")
	m.cursor = 1
	if !strings.Contains(m.View(), "md127 RAID5") || !strings.Contains(m.View(), "磁盘信息") {
		t.Fatalf("missing RAID: %s", m.View())
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = next.(metricsModel)
	upload := m.upload(previous)
	if upload.IntervalSeconds != 30 {
		t.Fatalf("interval: %d", upload.IntervalSeconds)
	}
	for _, key := range []string{"cpu.load1", "cpu.load5", "cpu.load15", "drive.model", "drive.total", "drive.used", "drive.read_rate", "drive.write_rate"} {
		if !slices.ContainsFunc(upload.Optional, func(s v2.UploadSelection) bool {
			return s.Metric == key && (!strings.HasPrefix(key, "drive.") || slices.Equal(s.Objects, []uint16{7}))
		}) {
			t.Errorf("missing bundled metric %s: %+v", key, upload.Optional)
		}
	}
	if slices.ContainsFunc(upload.Optional, func(s v2.UploadSelection) bool {
		return s.Metric == "cpu.usage" || s.Metric == "memory.usage" || s.Metric == "system.uptime"
	}) {
		t.Fatal("required metrics included in optional selections")
	}
	m.cursor = 0
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = next.(metricsModel)
	if slices.ContainsFunc(m.upload(previous).Optional, func(s v2.UploadSelection) bool { return strings.HasPrefix(s.Metric, "drive.") }) {
		t.Fatal("dependent RAID metrics not cleared with parent")
	}
}

func TestMenuRequiresParentAndNavigatesCategories(t *testing.T) {
	caps := &collector.V2Capabilities{Objects: []collector.V2Object{{ID: 7, Kind: "drive", Name: "md127 RAID5"}}, Metrics: []collector.V2Metric{
		{Metric: "cpu.usage", Available: true, Required: true},
		{Metric: "drive.model", Available: true, Objects: []uint16{7}},
		{Metric: "drive.smart_health", Available: true, Objects: []uint16{7}},
	}}
	m, err := makeMetrics(caps, nil)
	if err != nil {
		t.Fatal(err)
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = next.(metricsModel)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = next.(metricsModel)
	if m.groups[m.group] != "磁盘" {
		t.Fatalf("category: %s", m.groups[m.group])
	}
	m.cursor = 1
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = next.(metricsModel)
	if m.notice == "" || len(m.upload(nil).Optional) != 0 {
		t.Fatalf("dependent selected without parent: %+v", m)
	}
	m.cursor = 0
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = next.(metricsModel)
	m.cursor = 1
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = next.(metricsModel)
	if !slices.ContainsFunc(m.upload(nil).Optional, func(s v2.UploadSelection) bool {
		return s.Metric == "drive.smart_health" && slices.Equal(s.Objects, []uint16{7})
	}) {
		t.Fatal("could not select RAID health after enabling its parent")
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !next.(metricsModel).canceled || cmd == nil {
		t.Fatal("escape did not cancel")
	}
}
