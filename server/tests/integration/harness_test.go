//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"esp32-monitor/server/internal/api"
	"esp32-monitor/server/internal/config"
	"esp32-monitor/server/internal/panel"
	"esp32-monitor/server/internal/storage"
)

const initialTime int64 = 1792454400
const ingestPath = "/v1/ingest/telegraf"
const summaryPath = "/v1/devices/summary"

type synchronizedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *synchronizedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}
func (b *synchronizedBuffer) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.b.String() }

type harness struct {
	cfg        config.Config
	configPath string
	clock      atomic.Int64
	server     *httptest.Server
	client     *http.Client
	repo       *storage.Store
	db         *sql.DB // Independent SQL connection: observe committed state and inject test-only faults.
	logs       synchronizedBuffer
}

func newHarness(t *testing.T, edit ...func(*config.Config)) *harness {
	t.Helper()
	dir := t.TempDir()
	c := config.Defaults()
	c.Database = filepath.Join(dir, "monitor.db")
	var err error
	c.ReadToken, err = config.GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b", "hidden"} {
		token, e := config.GenerateToken()
		if e != nil {
			t.Fatal(e)
		}
		c.Agents = append(c.Agents, config.Agent{ID: id, WriteToken: token})
	}
	c.Panel.Devices = []config.Device{config.DefaultDevice("a", "NAS", "/data"), config.DefaultDevice("b", "PC", "/")}
	for _, f := range edit {
		f(&c)
	}
	h := &harness{configPath: filepath.Join(dir, "monitor.yaml")}
	h.clock.Store(initialTime)
	t.Cleanup(h.stop)
	h.start(t, c)
	return h
}
func (h *harness) start(t *testing.T, c config.Config) {
	t.Helper()
	h.server = httptest.NewUnstartedServer(nil)
	c.Listen = h.server.Listener.Addr().String()
	c.PublicBaseURL = "http://" + c.Listen
	original, err := config.Read(h.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err = config.Save(h.configPath, c, original); err != nil {
		t.Fatal(err)
	}
	h.cfg, err = config.Load(h.configPath)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, len(h.cfg.Agents))
	for i, a := range h.cfg.Agents {
		ids[i] = a.ID
	}
	h.repo, err = storage.Open(context.Background(), h.cfg.Database, ids, h.clock.Load())
	if err != nil {
		t.Fatal(err)
	}
	u := url.URL{Scheme: "file", Path: h.cfg.Database}
	u.RawQuery = url.Values{"_pragma": {"busy_timeout(5000)", "foreign_keys(1)"}}.Encode()
	h.db, err = sql.Open("sqlite", u.String())
	if err != nil {
		t.Fatal(err)
	}
	h.db.SetMaxOpenConns(1)
	if err = h.db.Ping(); err != nil {
		t.Fatal(err)
	}
	h.server.Config = api.NewServer(h.cfg, h.repo, func() time.Time { return time.Unix(h.clock.Load(), 0) }, slog.New(slog.NewJSONHandler(&h.logs, nil)))
	h.server.Start()
	h.client = h.server.Client()
	h.client.Timeout = 12 * time.Second
}
func (h *harness) stop() {
	if h.client != nil {
		h.client.CloseIdleConnections()
		h.client = nil
	}
	if h.server != nil {
		h.server.Close()
		h.server = nil
	}
	if h.db != nil {
		h.db.Close()
		h.db = nil
	}
	if h.repo != nil {
		h.repo.Close()
		h.repo = nil
	}
}
func (h *harness) restart(t *testing.T, c config.Config) { t.Helper(); h.stop(); h.start(t, c) }
func (h *harness) token(id string) string {
	for _, a := range h.cfg.Agents {
		if a.ID == id {
			return a.WriteToken
		}
	}
	panic("unknown test agent")
}
func (h *harness) advance(seconds int64) { h.clock.Add(seconds) }

type response struct {
	status int
	header http.Header
	body   []byte
}

func (h *harness) do(t *testing.T, method, path, token string, body io.Reader, edit ...func(*http.Request)) response {
	t.Helper()
	req, err := http.NewRequest(method, h.server.URL+path, body)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if method == "POST" {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, f := range edit {
		f(req)
	}
	res, err := h.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response{res.StatusCode, res.Header, b}
}
func (h *harness) post(t *testing.T, id string, body []byte) response {
	t.Helper()
	return h.do(t, "POST", ingestPath, h.token(id), bytes.NewReader(body))
}
func requireStatus(t *testing.T, res response, want int) {
	t.Helper()
	if res.status != want {
		t.Fatalf("HTTP status: got %d want %d; body=%s", res.status, want, res.body)
	}
}
func requireError(t *testing.T, res response, status int, code string) {
	t.Helper()
	requireStatus(t, res, status)
	var got map[string]string
	if err := json.Unmarshal(res.body, &got); err != nil || len(got) != 1 || got["error"] != code || res.header.Get("Content-Type") != "application/json" {
		t.Fatalf("invalid error response: %s (%v)", res.body, err)
	}
}
func (h *harness) summary(t *testing.T) panel.Summary {
	t.Helper()
	res := h.do(t, "GET", summaryPath, h.cfg.ReadToken, nil)
	requireStatus(t, res, 200)
	if len(res.body) > 4096 || res.header.Get("Content-Type") != "application/json" || res.header.Get("Cache-Control") != "no-store" || res.header.Get("Content-Encoding") != "" {
		t.Fatal("invalid summary headers or size")
	}
	var out panel.Summary
	dec := json.NewDecoder(bytes.NewReader(res.body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Devices == nil || out.DeviceCount != len(out.Devices) || out.DeviceCount > 4 {
		t.Fatal("invalid device count/array")
	}
	for _, d := range out.Devices {
		if len(d.Name) > 8 || len(d.Uptime) > 8 || d.MetricCount != 3 || len(d.Metrics) != 3 {
			t.Fatal("invalid card schema")
		}
		for i, m := range d.Metrics {
			if m.Name != []string{"CPU", "MEM", "DISK"}[i] || len(m.Value) > 8 || m.Percent != nil && (*m.Percent < 0 || *m.Percent > 100) {
				t.Fatal("invalid metric schema")
			}
		}
	}
	return out
}
func (h *harness) count(t *testing.T, table string) int {
	t.Helper()
	switch table {
	case "metric_history", "metric_latest", "devices":
	default:
		t.Fatal("invalid test table")
	}
	var n int
	if err := h.db.QueryRow("SELECT count(*) FROM " + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
func (h *harness) lastSeen(t *testing.T, id string) sql.NullInt64 {
	t.Helper()
	var seen sql.NullInt64
	if err := h.db.QueryRow("SELECT last_seen_at FROM devices WHERE device_id=?", id).Scan(&seen); err != nil {
		t.Fatal(err)
	}
	return seen
}

// dump reads all persisted values, including timestamps, to detect any partial mutation.
func (h *harness) dump(t *testing.T) string {
	t.Helper()
	var tables [][][]any
	for _, query := range []string{"SELECT device_id,last_seen_at,created_at FROM devices ORDER BY device_id", "SELECT * FROM metric_history ORDER BY device_id,measurement,tags_json,field,sampled_at", "SELECT * FROM metric_latest ORDER BY device_id,measurement,tags_json,field"} {
		rows, err := h.db.Query(query)
		if err != nil {
			t.Fatal(err)
		}
		cols, err := rows.Columns()
		if err != nil {
			rows.Close()
			t.Fatal(err)
		}
		var data [][]any
		for rows.Next() {
			values := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range values {
				ptrs[i] = &values[i]
			}
			if err = rows.Scan(ptrs...); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			data = append(data, values)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			t.Fatal(err)
		}
		tables = append(tables, data)
	}
	b, err := json.Marshal(tables)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
func (h *harness) exec(t *testing.T, statement string, args ...any) {
	t.Helper()
	if _, err := h.db.Exec(statement, args...); err != nil {
		t.Fatal(err)
	}
}
func fixture(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/telegraf-batch.json")
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func wireMetric(name string, tags map[string]string, fields map[string]any, ts int64) map[string]any {
	if tags == nil {
		tags = map[string]string{}
	}
	return map[string]any{"name": name, "tags": tags, "fields": fields, "timestamp": ts}
}
func batch(t *testing.T, records ...map[string]any) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]any{"metrics": records})
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func mem(t *testing.T, ts int64, v float64) []byte {
	t.Helper()
	return batch(t, wireMetric("mem", nil, map[string]any{"used_percent": v}, ts))
}
func requireMetric(t *testing.T, card panel.Device, index int, text string, percent *int) {
	t.Helper()
	m := card.Metrics[index]
	if m.Value != text || (m.Percent == nil) != (percent == nil) || m.Percent != nil && *m.Percent != *percent {
		t.Fatalf("%s: got %s/%v want %s/%v", m.Name, m.Value, m.Percent, text, percent)
	}
}
func integer(n int) *int { return &n }
func (h *harness) assertUnchanged(t *testing.T, before string) {
	t.Helper()
	if h.dump(t) != before {
		t.Fatal("rejected request changed persisted data")
	}
}
func (h *harness) assertNoSecrets(t *testing.T, body []byte) {
	t.Helper()
	content := h.logs.String() + string(body)
	for _, token := range append([]string{h.cfg.ReadToken}, agentTokens(h.cfg)...) {
		if bytes.Contains([]byte(content), []byte(token)) {
			t.Fatal("Token leaked into response/logs")
		}
	}
}
func agentTokens(c config.Config) []string {
	out := make([]string, len(c.Agents))
	for i, a := range c.Agents {
		out[i] = a.WriteToken
	}
	return out
}
