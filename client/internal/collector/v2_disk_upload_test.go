package collector

import (
	"context"
	"encoding/json"
	v2 "esp32-monitor/client/internal/collector/uploadv2"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

type diskUsageCollector struct {
	Collector
	used    uint64
	smartAt int64
}

func (c *diskUsageCollector) Gather(ctx context.Context, ids []string) (Batch, map[string]string) {
	batch, reasons := c.Collector.Gather(ctx, ids)
	for n := range batch.Metrics {
		metric := &batch.Metrics[n]
		switch metric.Name {
		case "darwin_disk_usage":
			metric.Fields["used"] = float64(c.used)
			metric.Exact = map[string]string{"used": strconv.FormatUint(c.used, 10)}
		case "darwin_disk":
			metric.Timestamp = c.smartAt
		}
	}
	return batch, reasons
}

func TestV2DiskUsageReachesUploadWithoutRates(t *testing.T) {
	for _, tc := range []struct {
		name  string
		used  uint64
		smart bool
	}{
		{name: "used_only", used: 1234},
		{name: "zero_used", used: 0},
		{name: "exact_large_used", used: 1<<53 + 1},
		{name: "cached_smart", used: 1234, smart: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			received := make(chan v2.SampleBatch, 4)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.Method + " " + r.URL.Path {
				case "GET /v2/collector/profile":
					w.WriteHeader(http.StatusNotFound)
				case "PUT /v2/collector/profile":
					w.Header().Set("Content-Type", "application/json")
					w.Header().Set("ETag", `"1"`)
					_ = json.NewEncoder(w).Encode(map[string]uint64{"r": 1})
				case "POST /v2/collector/samples":
					var batch v2.SampleBatch
					if err := json.NewDecoder(r.Body).Decode(&batch); err != nil {
						t.Errorf("decode uploaded sample: %v", err)
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					received <- batch
					w.WriteHeader(http.StatusNoContent)
				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			t.Cleanup(server.Close)
			m := newV2Fixture(t, server.URL+"/v2/collector/samples", "", &atomic.Int32{}, &atomic.Bool{})
			factory, smartAt := m.opts.CoreFactory, time.Now().Unix()
			m.opts.NativeFactory = func() Collector {
				return &diskUsageCollector{Collector: factory(), used: tc.used, smartAt: smartAt}
			}
			drive := m.v2.inventory.Drives[0].ID
			config := v2Config(v2.UploadSelection{Metric: "drive.used", Objects: []uint16{drive}})
			if tc.smart {
				config.Upload.Optional = append(config.Upload.Optional, v2.UploadSelection{Metric: "drive.smart_health", Objects: []uint16{drive}})
			}
			if _, err := m.Apply(context.Background(), config); err != nil {
				t.Fatal(err)
			}
			rounds := 1
			if tc.smart {
				rounds = 2
			}
			for round := 0; round < rounds; round++ {
				var batch v2.SampleBatch
				select {
				case batch = <-received:
				case <-time.After(8 * time.Second):
					t.Fatal("timed out waiting for uploaded disk usage")
				}
				if batch.Revision != 1 || len(batch.Samples) != 1 || len(batch.Samples[0].Drives) != 1 {
					t.Fatalf("disk missing from HTTP request: %+v", batch)
				}
				disk := batch.Samples[0].Drives[0]
				if disk.ID != drive || disk.Used == nil || *disk.Used != tc.used || disk.ReadRate != nil || disk.WriteRate != nil {
					t.Fatalf("unexpected uploaded disk: %+v; want used=%d without rates", disk, tc.used)
				}
				if (disk.SMART != nil) != (tc.smart && round == 0) {
					t.Fatalf("cached SMART should upload only once: round=%d disk=%+v", round, disk)
				}
				if tc.smart && round == 0 {
					m.mu.Lock()
					g := m.current
					m.mu.Unlock()
					// Repeat the same SMART observation with a fresh ordinary sample.
					// Removing the cached SMART must preserve the current used bytes.
					batch, _ := gather(context.Background(), g.core, g.native, selectedSources(g.collect))
					m.accept(g, batch)
				}
			}
		})
	}
}

func TestV2CachedSMARTOnlyDiskIsOmitted(t *testing.T) {
	m := &Manager{wake: make(chan struct{}, 1)}
	g := &generation{slow: map[string]int64{"smart:1": 123}}
	m.enqueueV2Locked(g, v2.Sample{Drives: []v2.DriveSample{{ID: 1, SMART: &v2.SMARTSample{Timestamp: 123}}}}, time.Now())
	if len(m.queue) != 1 || len(m.queue[0].v2.Samples[0].Drives) != 0 {
		t.Fatalf("disk with only a repeated SMART observation must be omitted: %+v", m.queue)
	}
}
