package collector

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// Helper processes exercise the real exec/HTTP/lifecycle path deterministically.
// The opt-in end-to-end suite separately uses an installed, real Telegraf binary.
func TestMain(m *testing.M) {
	if os.Getenv("ESP32_COLLECTOR_TEST_HELPER") == "1" {
		os.Exit(helper())
	}
	os.Exit(m.Run())
}
func helper() int {
	if len(os.Args) == 2 && os.Args[1] == "version" {
		fmt.Println("Telegraf " + TelegrafVersion)
		return 0
	}
	if len(os.Args) < 3 {
		return 2
	}
	if !slices.Contains(os.Args, "--strict-env-handling") {
		return 7
	}
	path := os.Args[2]
	data, err := os.ReadFile(path)
	if err != nil {
		return 3
	}
	var doc struct {
		Inputs  map[string][]map[string]any `toml:"inputs"`
		Outputs struct {
			HTTP []struct {
				URL     string            `toml:"url"`
				Headers map[string]string `toml:"headers"`
			} `toml:"http"`
		} `toml:"outputs"`
	}
	if toml.Unmarshal(data, &doc) != nil {
		return 4
	}
	once := (len(os.Args) > 3 && os.Args[3] == "--once") || len(doc.Outputs.HTTP) == 0
	b := Batch{Metrics: []Metric{}}
	for id := range doc.Inputs {
		field := "value"
		switch id {
		case "mem":
			field = "used"
		case "cpu":
			field = "usage_idle"
		case "system":
			field = "uptime"
		}
		b.Metrics = append(b.Metrics, Metric{id, map[string]string{}, map[string]float64{field: 42}, time.Now().Unix()})
		if !once && id == "system" {
			if _, e := os.Stat(filepath.Join(filepath.Dir(path), "fail-system")); e == nil {
				return 6
			}
		}
	}
	if once {
		_ = json.NewEncoder(os.Stdout).Encode(b)
		return 0
	}
	if len(doc.Outputs.HTTP) != 1 {
		return 5
	}
	out := doc.Outputs.HTTP[0]
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	body, _ := json.Marshal(b)
	for {
		req, _ := http.NewRequest("POST", out.URL, bytes.NewReader(body))
		for k, v := range out.Headers {
			req.Header.Set(k, v)
		}
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			resp.Body.Close()
		}
		select {
		case <-ctx.Done():
			return 0
		case <-time.After(25 * time.Millisecond):
		}
	}
}
func newHelper(t *testing.T, uploadURL string) *Manager {
	t.Helper()
	t.Setenv("ESP32_COLLECTOR_TEST_HELPER", "1")
	t.Setenv("GORACE", "atexit_sleep_ms=0")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err = os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	m, err := New(Options{Executable: exe, StateDirectory: dir, UploadURL: uploadURL, UploadToken: func(context.Context) (string, error) { return strings.Repeat("a", 64), nil }, ApplyTimeout: 3 * time.Second, HTTPClient: &http.Client{Timeout: time.Second}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Close() })
	return m
}
func cfg(id string, upload bool) Config { return Config{5, []Selection{{ID: id, Upload: upload}}} }
func TestConfigurationTransactionAndRollback(t *testing.T) {
	m := newHelper(t, "")
	ctx := context.Background()
	caps, err := m.Discover(ctx)
	if err != nil || len(caps.Categories) != 6 {
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
		t.Fatal("invalid upload changed running process")
	}
	if err = os.WriteFile(filepath.Join(m.opts.StateDirectory, "fail-system"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	rolled, err := m.Apply(ctx, cfg("system/system/uptime", false))
	if err == nil || rolled.State != "healthy" || rolled.Revision != 1 || rolled.PID == first.PID || rolled.Config.Selections[0].ID != "mem/mem/used" {
		t.Fatalf("rollback: %+v %v", rolled, err)
	}
	after, _ := os.ReadFile(snapshotPath)
	if !bytes.Equal(before, after) {
		t.Fatal("failed candidate committed")
	}
	second, err := m.Apply(ctx, cfg("cpu/cpu/usage_idle", false))
	if err != nil || second.Revision != 2 || second.PID == rolled.PID || len(second.Preview.Metrics) != 1 || second.Preview.Metrics[0].Name != "cpu" {
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

func TestReceiverSecurityAndStopWithoutExecutable(t *testing.T) {
	m := newHelper(t, "")
	ctx := context.Background()
	if _, err := m.Apply(ctx, cfg("mem/mem/used", false)); err != nil {
		t.Fatal(err)
	}
	paths, _ := filepath.Glob(filepath.Join(m.opts.StateDirectory, "telegraf-*.conf"))
	data, _ := os.ReadFile(paths[0])
	var doc struct {
		Outputs struct {
			HTTP []struct {
				URL     string            `toml:"url"`
				Headers map[string]string `toml:"headers"`
			} `toml:"http"`
		} `toml:"outputs"`
	}
	if err := toml.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	out := doc.Outputs.HTTP[0]
	send := func(body string, auth string) int {
		r, _ := http.NewRequest("POST", out.URL, strings.NewReader(body))
		r.Header.Set("Authorization", auth)
		response, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		_, _ = io.Copy(io.Discard, response.Body)
		return response.StatusCode
	}
	if send(`{}`, "") != 401 {
		t.Fatal("collector receiver accepted absent token")
	}
	if send(`{}`, out.Headers["Authorization"]) != 400 {
		t.Fatal("collector receiver accepted malformed batch")
	}
	if send(strings.Repeat("x", maxBody+1), out.Headers["Authorization"]) != 413 {
		t.Fatal("collector receiver accepted oversized body")
	}
	before := m.Snapshot()
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := m.Apply(canceled, cfg("cpu/cpu/usage_idle", false)); err == nil || m.Snapshot().PID != before.PID {
		t.Fatal("cancellation changed running configuration")
	}
	m.exe.path = filepath.Join(m.opts.StateDirectory, "missing")
	if stopped, err := m.Apply(ctx, DefaultConfig()); err != nil || stopped.PID != 0 {
		t.Fatalf("cannot stop without executable: %+v %v", stopped, err)
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

func TestGeneratedConfigUsesCurrentOptions(t *testing.T) {
	b, err := renderConfig(cfg("mem/mem/used", false), "http://127.0.0.1:12345/v1/collect", "local-token")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err = toml.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["agent"].(map[string]any)["skip_processors_after_aggregators"] != true {
		t.Fatal("processor behavior relies on a changing default")
	}
	for _, old := range []string{"fieldpass", "fielddrop", "tagpass", "tagdrop", "ignore_protocol_stats"} {
		if strings.Contains(string(b), old) {
			t.Fatalf("legacy option emitted: %s", old)
		}
	}
	if !strings.Contains(string(b), "fieldinclude") {
		t.Fatal("field filter not generated")
	}
}
