package collector

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
)

func TestCPUDeltasAndCounterReset(t *testing.T) {
	fields := cpuUsage(cpu.TimesStat{User: 10, System: 10, Idle: 80}, cpu.TimesStat{User: 12, System: 11, Idle: 87})
	if fields["usage_active"] != 30 || fields["usage_user"] != 20 || fields["usage_system"] != 10 {
		t.Fatalf("unexpected CPU deltas: %+v", fields)
	}
	guest := cpuUsage(cpu.TimesStat{}, cpu.TimesStat{User: 20, Guest: 5, Idle: 80})
	if guest["usage_user"] != 15 || guest["usage_active"] != 20 {
		t.Fatalf("guest counted twice: %+v", guest)
	}
	if cpuUsage(cpu.TimesStat{Idle: 100}, cpu.TimesStat{Idle: 90}) != nil {
		t.Fatal("counter reset fabricated CPU sample")
	}
}
func TestNativeUnknownSourceNeverProducesMetrics(t *testing.T) {
	batch, reasons := newNativeCollector().Gather(context.Background(), []string{"unknown_source"})
	if len(batch.Metrics) != 0 || reasons["unknown_source"] == "" {
		t.Fatal("stub fabricated metrics")
	}
}
func TestRealCoreSamplesUseTelegrafJSON(t *testing.T) {
	core := &CoreCollector{}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	b, reasons := core.Gather(ctx, []string{"cpu", "mem", "system"})
	if len(reasons) > 0 {
		t.Fatalf("core: %v", reasons)
	}
	found := map[string]bool{}
	for _, m := range b.Metrics {
		if m.Timestamp < time.Now().Unix()-10 || m.Tags == nil || len(m.Fields) == 0 {
			t.Fatalf("invalid Telegraf sample: %+v", m)
		}
		found[m.Name] = true
	}
	for _, name := range []string{"cpu", "mem", "system"} {
		if !found[name] {
			t.Fatalf("missing %s", name)
		}
	}
	encoded, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = parseBatch(encoded); err != nil {
		t.Fatal(err)
	}
}
func TestPersistenceFailureKeepsLiveGeneration(t *testing.T) {
	m := newHelper(t, "")
	first, err := m.Apply(context.Background(), cfg("mem/mem/used", false))
	if err != nil {
		t.Fatal(err)
	}
	current := m.current
	path := filepath.Join(m.opts.StateDirectory, "collector.json")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	_, err = m.Apply(context.Background(), cfg("cpu/cpu/usage_active", false))
	if err == nil || m.current != current || m.Snapshot().Revision != first.Revision || m.Snapshot().State != "healthy" {
		t.Fatalf("failed persist replaced live config: %v %+v", err, m.Snapshot())
	}
}

func TestRatesAndScope(t *testing.T) {
	c := &CoreCollector{}
	c.rates = map[string]rateSample{"net" + tagKey(map[string]string{"interface": "en0"}): {at: time.Now().Add(-time.Second), values: map[string]float64{"bytes_sent": 100, "bytes_recv": 200}}}
	b := c.restrictAndRate(Batch{Metrics: []Metric{
		{Name: "net", Tags: map[string]string{"interface": "en0"}, Fields: map[string]float64{"bytes_sent": 200, "bytes_recv": 100}},
		{Name: "cpu", Tags: map[string]string{}, Fields: map[string]float64{"usage_active": 20, "usage_idle": 80}},
	}})
	if len(b.Metrics) != 2 || b.Metrics[0].Fields["upload_bytes_per_second"] <= 0 {
		t.Fatalf("rate: %+v", b)
	}
	if _, ok := b.Metrics[0].Fields["download_bytes_per_second"]; ok {
		t.Fatal("counter reset produced a rate")
	}
	if _, ok := b.Metrics[1].Fields["usage_idle"]; ok {
		t.Fatal("out of scope field retained")
	}
}
