package collector

import (
	"bytes"
	"context"
	"encoding/json"
	v2 "esp32-monitor/client/internal/collector/uploadv2"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Deterministic collectors exercise transactions, scheduling and previews.
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
		batch.Metrics = append(batch.Metrics, Metric{Name: id, Tags: map[string]string{}, Fields: map[string]float64{field: 42}, Timestamp: time.Now().Unix()})
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
func cfg(id string) Config {
	return Config{IntervalSeconds: 5, Selections: []Selection{{ID: id}}}
}
func TestConfigurationTransactionAndRestore(t *testing.T) {
	m := newHelper(t, "")
	ctx := context.Background()
	caps, err := m.Discover(ctx)
	if err != nil || len(caps.Categories) != 7 {
		t.Fatalf("scan: %v %+v", err, caps)
	}
	first, err := m.Apply(ctx, cfg("mem/mem/used"))
	if err != nil || first.State != "healthy" || first.PID == 0 || first.Revision != 1 {
		t.Fatalf("first: %+v %v", first, err)
	}
	snapshotPath := filepath.Join(m.opts.StateDirectory, "collector.json")
	before, _ := os.ReadFile(snapshotPath)
	_, err = m.Apply(ctx, cfg("net/net/missing"))
	if err == nil || m.Snapshot().PID != first.PID {
		t.Fatal("invalid selection changed running configuration")
	}
	if err = os.WriteFile(filepath.Join(m.opts.StateDirectory, "fail-system"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	rolled, err := m.Apply(ctx, cfg("system/system/uptime"))
	if err == nil || rolled.State != "healthy" || rolled.Revision != 1 || rolled.PID != first.PID || rolled.Config.Selections[0].ID != "mem/mem/used" {
		t.Fatalf("rollback: %+v %v", rolled, err)
	}
	after, _ := os.ReadFile(snapshotPath)
	if !bytes.Equal(before, after) {
		t.Fatal("failed candidate committed")
	}
	second, err := m.Apply(ctx, cfg("cpu/cpu/usage_idle"))
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
				if metric.ID == "mem/mem/used" && len(metric.Objects) == 1 {
					found = true
				}
			}
		}
	}
	if !found {
		t.Fatal("front-end API omits actual probe objects and fields")
	}
	res = request("PUT", "/v1/collector/config", `{"interval_seconds":5,"selections":[{"id":"mem/mem/used"}]}`, "Bearer "+token, "")
	if res.Code != 200 {
		t.Fatal(res.Body.String())
	}
	if strings.Contains(res.Body.String(), token) || strings.Contains(res.Body.String(), "Authorization") {
		t.Fatal("credential leak")
	}
	if request("PUT", "/v1/collector/config", `{"interval_seconds":5,"selections":[],"shell":"bad"}`, "Bearer "+token, "").Code != 400 {
		t.Fatal("unknown fields accepted")
	}
	if request("PUT", "/v1/collector/config", `{"interval_seconds":5,"selections":[{"id":"mem/mem/used","upload":false}]}`, "Bearer "+token, "").Code != 400 {
		t.Fatal("removed v1 upload selection accepted")
	}
	if request("PUT", "/v1/collector/config", `{} {}`, "Bearer "+token, "").Code != 400 {
		t.Fatal("trailing JSON accepted")
	}
	if _, err := m.ListenAPI("0.0.0.0:0"); err == nil {
		t.Fatal("LAN API allowed")
	}
}
func TestFilteringAndObjectValidation(t *testing.T) {
	b := Batch{Metrics: []Metric{{Name: "cpu", Tags: map[string]string{"cpu": "cpu-total"}, Fields: map[string]float64{"usage_idle": 90, "usage_user": 10}, Timestamp: time.Now().Unix()}}}
	c := cfg("cpu/cpu/usage_idle")
	c.Selections[0].Objects = []map[string]string{{"cpu": "cpu-total"}}
	filtered := filter(b, c)
	if len(filtered.Metrics) != 1 || len(filtered.Metrics[0].Fields) != 1 {
		t.Fatalf("extra fields: %+v", filtered)
	}
	c.Selections[0].Objects[0]["cpu"] = "cpu0"
	if len(filter(b, c).Metrics) != 0 {
		t.Fatal("unselected object leaked")
	}
	c.Selections[0].Objects[0]["cpu"] = "cpu-total"
	caps := Capabilities{Categories: []Category{{Sources: []Capability{{Metrics: capabilitiesFor("cpu", b)}}}}}
	if validate(c, caps) != nil {
		t.Fatal("observed object rejected")
	}
	c.Selections[0].ID = "cpu/cpu/usage_guest"
	if validate(c, caps) == nil {
		t.Fatal("unavailable core accepted")
	}
}
func TestDeliveryRetriesAndPermanentErrors(t *testing.T) {
	m := newHelper(t, "")
	m.delivery.Lock()
	defer m.delivery.Unlock()
	q := queued{v2: &v2.SampleBatch{ID: strings.Repeat("a", 32), Samples: []v2.Sample{{}}}, revision: m.snapshot.Revision, at: time.Now()}
	m.mu.Lock()
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
	if m.Snapshot().UploadError != "upload_auth_required" || m.Snapshot().QueueDepth != 1 {
		t.Fatal("auth error not surfaced or batch dropped")
	}
	m.mu.Lock()
	if m.queue[0].next.IsZero() || !time.Now().Before(m.queue[0].next) {
		m.mu.Unlock()
		t.Fatal("auth error did not schedule a retry")
	}
	m.snapshot.UploadError = ""
	m.queue[0].next = time.Time{}
	m.mu.Unlock()
	m.finishDelivery(q, 429, http.Header{"Retry-After": []string{"0"}})
	if m.Snapshot().UploadError != "upload_retrying" || m.Snapshot().QueueDepth != 1 {
		t.Fatal("retryable error not retained")
	}
	if m.Snapshot().LastUploadAt != nil {
		t.Fatal("retry marked upload success")
	}
}
func TestQueueBoundAndExpiry(t *testing.T) {
	m := newHelper(t, "")
	m.delivery.Lock()
	defer m.delivery.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := 0; i < 1002; i++ {
		m.queue = append(m.queue, queued{v2: &v2.SampleBatch{Samples: []v2.Sample{{}}}, at: time.Now()})
	}
	m.trimQueueLocked(time.Now())
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
	if _, err := m.Apply(context.Background(), cfg("mem/mem/used")); err != nil {
		t.Fatal(err)
	}
	before := m.Snapshot()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m.Apply(ctx, cfg("cpu/cpu/usage_idle")); err == nil || m.Snapshot().Revision != before.Revision {
		t.Fatal("cancellation changed configuration")
	}
}

func TestOversizedV2BatchIsDropped(t *testing.T) {
	m := newHelper(t, "")
	m.delivery.Lock()
	defer m.delivery.Unlock()
	q := queued{v2: &v2.SampleBatch{ID: strings.Repeat("b", 32), Samples: []v2.Sample{{}}}, at: time.Now()}
	m.mu.Lock()
	m.queue = []queued{q}
	m.mu.Unlock()
	m.finishDelivery(q, 413, nil)
	if m.Snapshot().QueueDepth != 0 || m.Snapshot().DroppedMetrics != 1 {
		t.Fatal("oversized v2 batch was not dropped")
	}
}

func TestRestoreUnavailableSelectionRemainsRepairable(t *testing.T) {
	m := newHelper(t, "")
	missing := cfg("mem/mem/removed_field")
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
	repaired, err := restored.Apply(context.Background(), cfg("mem/mem/used"))
	if err != nil || repaired.State != "healthy" || repaired.Revision != 5 {
		t.Fatalf("cannot repair: %+v %v", repaired, err)
	}
}
