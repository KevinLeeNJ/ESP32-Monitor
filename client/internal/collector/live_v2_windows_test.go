//go:build windows

package collector

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	v2 "esp32-monitor/client/internal/collector/uploadv2"
)

// TestLiveWindowsV2Upload exercises the real Core/Native collectors against a
// local fake Server. It never contacts a production endpoint and never selects
// network.public_ipv4, so no external address is queried or printed.
func TestLiveWindowsV2Upload(t *testing.T) {
	if os.Getenv("MONITOR_LIVE_V2") != "1" {
		t.Skip("explicit live upload test")
	}
	var mu sync.Mutex
	var sample []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /v2/collector/profile":
			w.WriteHeader(http.StatusNotFound)
		case "PUT /v2/collector/profile", "PATCH /v2/collector/profile":
			w.Header().Set("ETag", `"1"`)
			_, _ = io.WriteString(w, `{"r":1}`)
		case "POST /v2/collector/samples":
			body, _ := io.ReadAll(r.Body)
			mu.Lock()
			sample = body
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	m, err := New(Options{StateDirectory: t.TempDir(), UploadURL: server.URL + "/v2/collector/samples", UploadToken: func(context.Context) (string, error) { return strings.Repeat("b", 64), nil }, ApplyTimeout: 30 * time.Second, HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	caps, err := m.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	available := map[string]V2Metric{}
	for _, metric := range caps.V2.Metrics {
		available[metric.Metric] = metric
	}
	optional := []v2.UploadSelection{}
	for _, key := range []string{"cpu.load1", "cpu.load5", "cpu.load15", "cpu.temperature", "memory.used", "memory.swap_total", "memory.swap_used", "gpu.usage", "gpu.temperature", "drive.model", "drive.total", "drive.used", "drive.smart_health", "interface.medium", "interface.up", "battery.level", "battery.charging"} {
		metric, ok := available[key]
		if !ok || !metric.Available {
			continue
		}
		optional = append(optional, v2.UploadSelection{Metric: key, Objects: metric.Objects})
	}
	config := Config{IntervalSeconds: 5, Upload: &v2.UploadConfig{Enabled: true, IntervalSeconds: 5, Optional: optional}}
	if _, err := m.Apply(context.Background(), config); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		got := append([]byte(nil), sample...)
		mu.Unlock()
		if len(got) > 0 {
			var decoded map[string]any
			if err := json.Unmarshal(got, &decoded); err != nil {
				t.Fatal(err)
			}
			t.Logf("sample: %s", got)
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("no v2 sample uploaded")
}
