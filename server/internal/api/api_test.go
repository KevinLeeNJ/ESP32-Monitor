package api

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"esp32-monitor/server/internal/config"
	"esp32-monitor/server/internal/metrics"
	"esp32-monitor/server/internal/storage"
)

const now int64 = 1792454400
const validBody = `{"metrics":[{"name":"mem","tags":{"host":"device-b"},"fields":{"used_percent":42},"timestamp":1792454400}]}`

type fakeRepo struct {
	calls      int
	id         string
	samples    []metrics.Sample
	receivedAt int64
	err        error
	snapshot   storage.Snapshot
}

func (f *fakeRepo) Ingest(_ context.Context, id string, s []metrics.Sample, at int64) error {
	f.calls++
	f.id = id
	f.samples = s
	f.receivedAt = at
	return f.err
}
func (f *fakeRepo) Snapshot(context.Context) (storage.Snapshot, error) {
	f.calls++
	return f.snapshot, f.err
}
func (f *fakeRepo) Ready(context.Context) error { f.calls++; return f.err }
func setup(t *testing.T) (*Handler, *fakeRepo, *bytes.Buffer) {
	t.Helper()
	c := config.Defaults()
	c.ReadToken = strings.Repeat("1", 64)
	c.Agents = []config.Agent{{ID: "a", WriteToken: strings.Repeat("2", 64)}, {ID: "b", WriteToken: strings.Repeat("3", 64)}}
	c.Panel.Devices = []config.Device{config.DefaultDevice("a", "NAS", "/")}
	repo := &fakeRepo{}
	logs := &bytes.Buffer{}
	return New(c, repo, func() time.Time { return time.Unix(now, 0) }, slog.New(slog.NewJSONHandler(logs, nil))), repo, logs
}
func request(h *Handler, method, path, token, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func TestAuthScopeAndDeviceBinding(t *testing.T) {
	for _, tt := range []struct {
		name, path, token string
		status            int
	}{{"missing", "/v1/devices/summary", "", 401}, {"bad", "/v1/devices/summary", strings.Repeat("4", 64), 401}, {"writer cannot read", "/v1/devices/summary", strings.Repeat("2", 64), 403}, {"reader cannot write", "/v1/ingest/telegraf", strings.Repeat("1", 64), 403}} {
		t.Run(tt.name, func(t *testing.T) {
			h, repo, logs := setup(t)
			method := "GET"
			if tt.path == "/v1/ingest/telegraf" {
				method = "POST"
			}
			w := request(h, method, tt.path, tt.token, validBody)
			if w.Code != tt.status || repo.calls != 0 || strings.Contains(logs.String(), tt.token) && tt.token != "" {
				t.Fatal(w.Code, repo.calls, logs.String())
			}
		})
	}
	h, repo, logs := setup(t)
	w := request(h, "POST", "/v1/ingest/telegraf", strings.Repeat("2", 64), validBody)
	if w.Code != 204 || w.Body.Len() != 0 || repo.id != "a" || repo.receivedAt != now || repo.samples[0].TagsJSON != `{}` {
		t.Fatal(w.Code, repo)
	}
	for _, secret := range []string{strings.Repeat("1", 64), strings.Repeat("2", 64), "device-b", "Authorization"} {
		if strings.Contains(logs.String(), secret) {
			t.Fatal("secret or payload leaked")
		}
	}
	r := httptest.NewRequest("GET", "/v1/devices/summary", nil)
	r.Header.Add("Authorization", "Bearer "+strings.Repeat("1", 64))
	r.Header.Add("Authorization", "Bearer "+strings.Repeat("1", 64))
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("multiple auth accepted")
	}
}
func TestUploadValidationAndStorageErrors(t *testing.T) {
	for _, tt := range []struct {
		name, body, contentType, encoding string
		status                            int
		repoErr                           error
		calls                             int
	}{
		{name: "json", body: `{`, contentType: "application/json", status: 400},
		{name: "structure", body: `{"metrics":[]}`, contentType: "application/json", status: 422},
		{name: "type", body: validBody, contentType: "text/plain", status: 415},
		{name: "encoding", body: validBody, contentType: "application/json", encoding: "gzip", status: 415},
		{name: "size", body: strings.Repeat(" ", metrics.MaxBody+1), contentType: "application/json", status: 413},
		{name: "db", body: validBody, contentType: "application/json", status: 503, repoErr: errors.New("secret db failure"), calls: 1},
		{name: "series", body: validBody, contentType: "application/json", status: 422, repoErr: storage.ErrSeriesLimit, calls: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h, repo, logs := setup(t)
			repo.err = tt.repoErr
			r := httptest.NewRequest("POST", "/v1/ingest/telegraf", strings.NewReader(tt.body))
			r.Header.Set("Authorization", "Bearer "+strings.Repeat("2", 64))
			r.Header.Set("Content-Type", tt.contentType)
			r.Header.Set("Content-Encoding", tt.encoding)
			r.ContentLength = -1 // exercise actual chunk/body bound
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tt.status || repo.calls != tt.calls || strings.Contains(w.Body.String()+logs.String(), "secret db failure") {
				t.Fatal(w.Code, repo.calls, w.Body.String())
			}
		})
	}
}
func TestRateLimitPerAgentAndRefill(t *testing.T) {
	h, repo, _ := setup(t)
	for i := 0; i < 10; i++ {
		if w := request(h, "POST", "/v1/ingest/telegraf", strings.Repeat("2", 64), validBody); w.Code != 204 {
			t.Fatal(i, w.Code)
		}
	}
	w := request(h, "POST", "/v1/ingest/telegraf", strings.Repeat("2", 64), validBody)
	if w.Code != 429 || repo.calls != 10 || w.Header().Get("Retry-After") == "" {
		t.Fatal(w.Code, repo.calls)
	}
	if w = request(h, "POST", "/v1/ingest/telegraf", strings.Repeat("3", 64), validBody); w.Code != 204 {
		t.Fatal("agents share limiter")
	}
	h.now = func() time.Time { return time.Unix(now+1, 0) }
	if w = request(h, "POST", "/v1/ingest/telegraf", strings.Repeat("2", 64), validBody); w.Code != 204 {
		t.Fatal("no refill")
	}
}
func TestSummaryProbesAndRouting(t *testing.T) {
	h, repo, _ := setup(t)
	w := request(h, "GET", "/v1/devices/summary", strings.Repeat("1", 64), "")
	if w.Code != 200 || w.Body.String() != `{"device_count":0,"devices":[]}` || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Content-Encoding") != "" {
		t.Fatal(w.Code, w.Body.String())
	}
	repo.err = errors.New("sensitive database details")
	w = request(h, "GET", "/v1/devices/summary", strings.Repeat("1", 64), "")
	if w.Code != 503 || strings.Contains(w.Body.String(), "sensitive") {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, tt := range []struct {
		method, path string
		status       int
	}{{"GET", "/healthz", 200}, {"GET", "/readyz", 503}, {"POST", "/v1/devices/summary", 405}, {"GET", "/v1/ingest/telegraf", 405}, {"GET", "/missing", 404}, {"GET", "/v1/devices/summary/", 404}} {
		if w = request(h, tt.method, tt.path, "", ""); w.Code != tt.status {
			t.Fatal(tt, w.Code)
		}
	}
	repo.err = nil
	if w = request(h, "GET", "/readyz", "", ""); w.Code != 200 {
		t.Fatal(w.Code)
	}
	seen := now
	repo.snapshot = storage.Snapshot{"a": {LastSeen: &seen, Samples: []metrics.Sample{{Measurement: "mem", TagsJSON: `{}`, Field: "used_percent", SampledAt: now, Value: 42}}}}
	w = request(h, "GET", "/v1/devices/summary", strings.Repeat("1", 64), "")
	b, _ := io.ReadAll(w.Result().Body)
	if w.Code != 200 || !bytes.Contains(b, []byte(`"value":"42%"`)) {
		t.Fatal(w.Code, string(b))
	}
}
