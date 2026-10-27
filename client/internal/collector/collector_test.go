package collector

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Deterministic collectors exercise transactions, scheduling and the real uploader.
type testCollector struct{ dir string }

func (c *testCollector) Gather(ctx context.Context, ids []string) (Batch, map[string]string) {
	batch := Batch{Metrics: []Metric{}}
	reasons := map[string]string{}
	for _, id := range ids {
		if ctx.Err() != nil {
			reasons[id] = "canceled"
			continue
		}
		if id == "system" {
			if _, err := os.Stat(filepath.Join(c.dir, "fail-system")); err == nil {
				reasons[id] = "collect_failed"
				continue
			}
		}
		field := "value"
		switch id {
		case "mem":
			field = "used"
		case "cpu":
			field = "usage_idle"
		case "system":
			field = "uptime"
		}
		batch.Metrics = append(batch.Metrics, Metric{id, map[string]string{}, map[string]float64{field: 42}, time.Now().Unix()})
	}
	return batch, reasons
}
func newHelper(t *testing.T, uploadURL string) *Manager {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	m, err := New(Options{CoreFactory: func() Collector { return &testCollector{dir} }, NativeFactory: func() Collector { return &NativeCollector{} }, StateDirectory: dir, UploadURL: uploadURL, UploadToken: func(context.Context) (string, error) { return strings.Repeat("a", 64), nil }, ApplyTimeout: time.Second, HTTPClient: &http.Client{Timeout: time.Second}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Close() })
	return m
}
func cfg(id string, upload bool) Config { return Config{5, []Selection{{ID: id, Upload: upload}}} }
func TestConfigurationTransactionAndRestore(t *testing.T) {
	m := newHelper(t, "")
	ctx := context.Background()
	caps, err := m.Discover(ctx)
	if err != nil || len(caps.Categories) != 7 {
		t.Fatalf("scan: %v %+v", err, caps)
	}
	first, err := m.Apply(ctx, cfg("mem/mem/used", false))
	if err != nil || first.State != "healthy" || first.PID == 0 || first.Revision != 1 {
		t.Fatalf("first: %+v %v", first, err)
	}
	snapshotPath := filepath.Join(m.opts.StateDirectory, "collector.json")
	before, _ := os.ReadFile(snapshotPath)
	_, err = m.Apply(ctx, cfg("net/net/value", true))
	if err == nil || m.Snapshot().PID != first.PID {
		t.Fatal("invalid upload changed running configuration")
	}
	if err = os.WriteFile(filepath.Join(m.opts.StateDirectory, "fail-system"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	rolled, err := m.Apply(ctx, cfg("system/system/uptime", false))
	if err == nil || rolled.State != "healthy" || rolled.Revision != 1 || rolled.PID != first.PID || rolled.Config.Selections[0].ID != "mem/mem/used" {
		t.Fatalf("rollback: %+v %v", rolled, err)
	}
	after, _ := os.ReadFile(snapshotPath)
	if !bytes.Equal(before, after) {
		t.Fatal("failed candidate committed")
	}
	second, err := m.Apply(ctx, cfg("cpu/cpu/usage_idle", false))
	if err != nil || second.Revision != 2 || second.PID != rolled.PID || len(second.Preview.Metrics) != 1 || second.Preview.Metrics[0].Name != "cpu" {
		t.Fatalf("second: %+v %v", second, err)
	}
	second.Preview.Metrics[0].Fields["usage_idle"] = 999
	if m.Snapshot().Preview.Metrics[0].Fields["usage_idle"] == 999 {
		t.Fatal("snapshot aliases manager state")
	}
	if _, err = New(m.opts); err == nil {
		t.Fatal("duplicate owner accepted")
	}
	_ = m.Close()
	restored, err := New(m.opts)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if err = restored.Restore(ctx); err != nil || restored.Snapshot().Revision != 2 || restored.Snapshot().Config.Selections[0].ID != "cpu/cpu/usage_idle" {
		t.Fatalf("restore: %+v %v", restored.Snapshot(), err)
	}
	stopped, err := restored.Apply(ctx, DefaultConfig())
	if err != nil || stopped.State != "disabled" || stopped.PID != 0 || len(stopped.Preview.Metrics) != 0 {
		t.Fatalf("disable: %+v %v", stopped, err)
	}
}
func TestFrontendAPIContainsProbedMetrics(t *testing.T) {
	m := newHelper(t, "")
	token := strings.Repeat("b", 64)
	handler := m.Handler(token)
	request := func(method, path, body, auth, origin string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.RemoteAddr = "127.0.0.1:1234"
		r.Header.Set("Authorization", auth)
		r.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if request("GET", "/v1/collector/status", "", "", "").Code != 401 {
		t.Fatal("missing auth accepted")
	}
	if request("GET", "/v1/collector/status", "", "Bearer "+token, "https://evil.example").Code != 401 {
		t.Fatal("browser origin accepted")
	}
	res := request("POST", "/v1/collector/rescan", "", "Bearer "+token, "")
	if res.Code != 200 {
		t.Fatal(res.Body.String())
	}
	var caps Capabilities
	if json.Unmarshal(res.Body.Bytes(), &caps) != nil {
		t.Fatal("invalid capabilities")
	}
	found := false
	for _, cat := range caps.Categories {
		for _, src := range cat.Sources {
			for _, metric := range src.Metrics {
				if metric.ID == "mem/mem/used" && len(metric.Objects) == 1 && metric.UploadSupported {
					found = true
				}
			}
		}
	}
	if !found {
		t.Fatal("front-end API omits actual probe objects and fields")
	}
	res = request("PUT", "/v1/collector/config", `{"interval_seconds":5,"selections":[{"id":"mem/mem/used","upload":false}]}`, "Bearer "+token, "")
	if res.Code != 200 {
		t.Fatal(res.Body.String())
	}
	if strings.Contains(res.Body.String(), token) || strings.Contains(res.Body.String(), "Authorization") {
		t.Fatal("credential leak")
	}
	if request("PUT", "/v1/collector/config", `{"interval_seconds":5,"selections":[],"shell":"bad"}`, "Bearer "+token, "").Code != 400 {
		t.Fatal("unknown fields accepted")
	}
	if request("PUT", "/v1/collector/config", `{} {}`, "Bearer "+token, "").Code != 400 {
		t.Fatal("trailing JSON accepted")
	}
	if _, err := m.ListenAPI("0.0.0.0:0"); err == nil {
		t.Fatal("LAN API allowed")
	}
}
func TestFilteringAndObjectValidation(t *testing.T) {
	raw := []byte(`{"metrics":[{"name":"cpu","tags":{"cpu":"cpu-total","host":"private"},"fields":{"usage_idle":90,"usage_user":10,"label":"ignored"},"timestamp":100}]}`)
	b, err := parseBatch(raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := b.Metrics[0].Tags["host"]; ok {
		t.Fatal("hostname leaked")
	}
	c := cfg("cpu/cpu/usage_idle", true)
	c.Selections[0].Objects = []map[string]string{{"cpu": "cpu-total"}}
	filtered := filter(b, c, true)
	if len(filtered.Metrics) != 1 || len(filtered.Metrics[0].Fields) != 1 {
		t.Fatalf("extra fields: %+v", filtered)
	}
	c.Selections[0].Objects[0]["cpu"] = "cpu0"
	if len(filter(b, c, false).Metrics) != 0 {
		t.Fatal("unselected object leaked")
	}
	caps := Capabilities{Categories: []Category{{Sources: []Capability{{Metrics: capabilitiesFor("cpu", b)}}}}}
	if validate(c, caps) == nil {
		t.Fatal("unavailable core accepted")
	}
	if _, err = parseBatch([]byte(`{"metrics":[{"name":"x","tags":{},"fields":{"v":1e400},"timestamp":1}]}`)); err == nil {
		t.Fatal("invalid number accepted")
	}
}
func TestUploaderRetriesAndPermanentErrors(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := requests.Add(1)
		if r.Header.Get("Authorization") != "Bearer "+strings.Repeat("a", 64) {
			t.Error("bad upstream auth")
		}
		if n == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(429)
		} else {
			w.WriteHeader(204)
		}
	}))
	defer server.Close()
	m := newHelper(t, server.URL+"/v1/ingest/telegraf")
	if _, err := m.Apply(context.Background(), cfg("mem/mem/used", true)); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if m.Snapshot().LastUploadAt != nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if m.Snapshot().LastUploadAt == nil || requests.Load() < 2 {
		t.Fatalf("no successful retry: %+v", m.Snapshot())
	}
	m.delivery.Lock()
	m.mu.Lock()
	q := queued{batch: Batch{[]Metric{{Name: "mem", Fields: map[string]float64{"used": 1}, Tags: map[string]string{}, Timestamp: time.Now().Unix()}}}, revision: m.snapshot.Revision, at: time.Now()}
	m.queue = []queued{q}
	m.mu.Unlock()
	m.finishDelivery(q, 422, nil)
	if m.Snapshot().QueueDepth != 0 || !strings.Contains(m.Snapshot().UploadError, "invalid") {
		t.Fatal("permanent error retried")
	}
	m.mu.Lock()
	m.queue = []queued{q}
	m.mu.Unlock()
	m.finishDelivery(q, 401, nil)
	if m.Snapshot().UploadError != "upload_auth_required" {
		t.Fatal("auth error not surfaced")
	}
	m.delivery.Unlock()
}
func TestQueueBoundAndExpiry(t *testing.T) {
	m := newHelper(t, "")
	m.delivery.Lock()
	defer m.delivery.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := 0; i < 1002; i++ {
		m.enqueueLocked(Batch{Metrics: []Metric{{Name: "mem"}}}, 0, time.Now())
	}
	if queueMetrics(m.queue) != 1000 || m.snapshot.DroppedMetrics != 2 {
		t.Fatal("queue not bounded")
	}
	m.trimQueueLocked(time.Now().Add(6 * time.Minute))
	if len(m.queue) != 0 || m.snapshot.DroppedMetrics != 1002 {
		t.Fatal("old samples not expired")
	}
}

func TestCancellationPreservesRunningSelection(t *testing.T) {
	m := newHelper(t, "")
	if _, err := m.Apply(context.Background(), cfg("mem/mem/used", false)); err != nil {
		t.Fatal(err)
	}
	before := m.Snapshot()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m.Apply(ctx, cfg("cpu/cpu/usage_idle", false)); err == nil || m.Snapshot().Revision != before.Revision {
		t.Fatal("cancellation changed configuration")
	}
	paths, _ := filepath.Glob(filepath.Join(m.opts.StateDirectory, "telegraf-*"))
	if len(paths) > 0 {
		t.Fatal("legacy config created")
	}
}

func TestSplitOversizedBatchAndPauseAuthentication(t *testing.T) {
	m := newHelper(t, "")
	m.delivery.Lock()
	defer m.delivery.Unlock()
	q := queued{batch: Batch{Metrics: []Metric{{Name: "mem"}, {Name: "cpu"}}}, at: time.Now()}
	m.mu.Lock()
	m.queue = []queued{q}
	m.mu.Unlock()
	m.finishDelivery(q, 413, nil)
	m.mu.Lock()
	if len(m.queue) != 2 || len(m.queue[0].batch.Metrics) != 1 || len(m.queue[1].batch.Metrics) != 1 {
		t.Fatal("413 did not split batch")
	}
	q = m.queue[0]
	m.mu.Unlock()
	m.finishDelivery(q, 413, nil)
	if m.Snapshot().QueueDepth != 1 || m.Snapshot().DroppedMetrics != 1 {
		t.Fatal("oversized single metric was not isolated")
	}
}

func TestRestoreUnavailableSelectionRemainsRepairable(t *testing.T) {
	m := newHelper(t, "")
	missing := cfg("mem/mem/removed_field", false)
	if err := m.persist(missing, 4); err != nil {
		t.Fatal(err)
	}
	opts := m.opts
	_ = m.Close()
	restored, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if err = restored.Restore(context.Background()); err == nil {
		t.Fatal("missing field restored as healthy")
	}
	s := restored.Snapshot()
	if s.State != "error" || s.Revision != 4 || s.Config.Selections[0].ID != missing.Selections[0].ID || s.PID != 0 {
		t.Fatalf("unrepairable restore state: %+v", s)
	}
	api, err := restored.ListenAPI("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	repaired, err := restored.Apply(context.Background(), cfg("mem/mem/used", false))
	if err != nil || repaired.State != "healthy" || repaired.Revision != 5 {
		t.Fatalf("cannot repair: %+v %v", repaired, err)
	}
}
