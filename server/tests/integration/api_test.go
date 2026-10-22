//go:build integration

package integration_test

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"esp32-monitor/server/internal/config"
)

func TestNativeBatchToPersistedSummary(t *testing.T) {
	h := newHarness(t)
	if got := h.summary(t); got.DeviceCount != 0 {
		t.Fatal("unreported device visible")
	}
	res := h.post(t, "a", fixture(t))
	requireStatus(t, res, 204)
	if len(res.body) != 0 {
		t.Fatal("204 must not carry a body")
	}
	if h.count(t, "metric_history") != 7 || h.count(t, "metric_latest") != 7 {
		t.Fatal("supported numeric fields not persisted")
	}
	if seen := h.lastSeen(t, "a"); !seen.Valid || seen.Int64 != initialTime {
		t.Fatal("last_seen not committed")
	}
	if h.lastSeen(t, "b").Valid {
		t.Fatal("other device changed")
	}
	got := h.summary(t)
	if got.DeviceCount != 1 || got.Devices[0].Name != "NAS" || got.Devices[0].Uptime != "12d 4h" {
		t.Fatalf("wrong card: %+v", got)
	}
	requireMetric(t, got.Devices[0], 0, "27%", integer(27))
	requireMetric(t, got.Devices[0], 1, "42%", integer(42))
	requireMetric(t, got.Devices[0], 2, "61%", integer(61))
	var tags string
	var raw float64
	if err := h.db.QueryRow("SELECT tags_json,value FROM metric_latest WHERE device_id='a' AND measurement='disk' AND field='total'").Scan(&tags, &raw); err != nil || tags != `{"device":"disk0","fstype":"ext4","path":"/data"}` || raw != 10000 {
		t.Fatal("raw field or canonical tags mismatch", err)
	}
	h.assertNoSecrets(t, res.body)
}

func TestAuthenticationIsolationAndRoutes(t *testing.T) {
	h := newHarness(t)
	before := h.dump(t)
	tests := []struct {
		name, method, path, token string
		status                    int
		code                      string
	}{
		{"missing upload Token", "POST", ingestPath, "", 401, "unauthorized"},
		{"unknown upload Token", "POST", ingestPath, strings.Repeat("0", 64), 401, "unauthorized"},
		{"read Token cannot upload", "POST", ingestPath, h.cfg.ReadToken, 403, "unauthorized"},
		{"missing read Token", "GET", summaryPath, "", 401, "unauthorized"},
		{"writer cannot read", "GET", summaryPath, h.token("a"), 403, "unauthorized"},
		{"wrong method", "GET", ingestPath, h.token("a"), 405, "method_not_allowed"},
		{"unknown route", "GET", "/v1/missing", h.cfg.ReadToken, 404, "not_found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := h.do(t, tt.method, tt.path, tt.token, bytes.NewReader(fixture(t)))
			requireError(t, res, tt.status, tt.code)
			h.assertUnchanged(t, before)
			h.assertNoSecrets(t, res.body)
		})
	}
	t.Run("duplicate Authorization", func(t *testing.T) {
		res := h.do(t, "POST", ingestPath, h.token("a"), bytes.NewReader(fixture(t)), func(r *http.Request) { r.Header.Add("Authorization", "Bearer "+h.token("b")) })
		requireError(t, res, 401, "unauthorized")
		h.assertUnchanged(t, before)
	})
	t.Run("spoofed host does not change identity", func(t *testing.T) {
		body := batch(t, wireMetric("mem", map[string]string{"host": "b", "device_id": "b"}, map[string]any{"used_percent": 17}, initialTime))
		requireStatus(t, h.post(t, "a", body), 204)
		if !h.lastSeen(t, "a").Valid || h.lastSeen(t, "b").Valid {
			t.Fatal("host changed authenticated identity")
		}
		var id string
		if err := h.db.QueryRow("SELECT device_id FROM metric_history").Scan(&id); err != nil || id != "a" {
			t.Fatal("wrong device persisted", err)
		}
	})
}

func TestRejectedUploadsAreAtomic(t *testing.T) {
	h := newHarness(t)
	requireStatus(t, h.post(t, "a", mem(t, initialTime, 42)), 204)
	good := wireMetric("mem", nil, map[string]any{"used_percent": 55}, initialTime+1)
	many := make([]map[string]any, 101)
	for i := range many {
		many[i] = good
	}
	tags := map[string]string{}
	for i := 0; i < 17; i++ {
		tags[fmt.Sprint(i)] = "v"
	}
	fields := map[string]any{}
	for i := 0; i < 65; i++ {
		fields[fmt.Sprint(i)] = 1
	}
	tests := []struct {
		name   string
		body   []byte
		status int
		code   string
		edit   func(*http.Request)
	}{
		{name: "malformed JSON", body: []byte(`{"metrics":`), status: 400, code: "invalid_json"},
		{name: "trailing JSON", body: []byte(`{"metrics":[]} {}`), status: 400, code: "invalid_json"},
		{name: "wrong root", body: []byte(`[]`), status: 422, code: "invalid_payload"},
		{name: "empty batch", body: []byte(`{"metrics":[]}`), status: 422, code: "invalid_payload"},
		{name: "unknown measurement only", body: batch(t, wireMetric("net", nil, map[string]any{"bytes": 1}, initialTime)), status: 422, code: "invalid_payload"},
		{name: "nonnumeric only", body: batch(t, wireMetric("mem", nil, map[string]any{"used_percent": "42"}, initialTime)), status: 422, code: "invalid_payload"},
		{name: "partial batch invalid timestamp", body: batch(t, good, wireMetric("cpu", nil, map[string]any{"usage_active": 1}, initialTime+100)), status: 422, code: "invalid_payload"},
		{name: "old timestamp", body: batch(t, good, wireMetric("mem", nil, map[string]any{"x": 1}, initialTime-86401)), status: 422, code: "invalid_payload"},
		{name: "millisecond timestamp", body: batch(t, wireMetric("mem", nil, map[string]any{"x": 1}, initialTime*1000)), status: 422, code: "invalid_payload"},
		{name: "nonfinite number", body: []byte(`{"metrics":[{"name":"mem","tags":{},"fields":{"x":1e999},"timestamp":1792454400}]}`), status: 422, code: "invalid_payload"},
		{name: "null tag", body: []byte(`{"metrics":[{"name":"mem","tags":{"host":null},"fields":{"x":1},"timestamp":1792454400}]}`), status: 422, code: "invalid_payload"},
		{name: "too many tags", body: batch(t, wireMetric("cpu", tags, map[string]any{"x": 1}, initialTime)), status: 422, code: "invalid_payload"},
		{name: "too many fields", body: batch(t, wireMetric("cpu", nil, fields, initialTime)), status: 422, code: "invalid_payload"},
		{name: "long name", body: batch(t, wireMetric(strings.Repeat("x", 129), nil, map[string]any{"x": 1}, initialTime)), status: 422, code: "invalid_payload"},
		{name: "long UTF8 tag", body: batch(t, wireMetric("mem", map[string]string{"tag": strings.Repeat("中", 43)}, map[string]any{"x": 1}, initialTime)), status: 422, code: "invalid_payload"},
		{name: "101 metrics", body: batch(t, many...), status: 413, code: "payload_too_large"},
		{name: "oversize content length", body: bytes.Repeat([]byte(" "), 256*1024+1), status: 413, code: "payload_too_large"},
		{name: "oversize chunked", body: bytes.Repeat([]byte(" "), 256*1024+1), status: 413, code: "payload_too_large", edit: func(r *http.Request) { r.ContentLength = -1; r.TransferEncoding = []string{"chunked"} }},
		{name: "unsupported content type", body: mem(t, initialTime, 1), status: 415, code: "unsupported_media_type", edit: func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }},
		{name: "compressed upload", body: mem(t, initialTime, 1), status: 415, code: "unsupported_media_type", edit: func(r *http.Request) { r.Header.Set("Content-Encoding", "gzip") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h.advance(2)
			before := h.dump(t)
			var edit []func(*http.Request)
			if tt.edit != nil {
				edit = append(edit, tt.edit)
			}
			res := h.do(t, "POST", ingestPath, h.token("a"), bytes.NewReader(tt.body), edit...)
			requireError(t, res, tt.status, tt.code)
			h.assertUnchanged(t, before)
			h.assertNoSecrets(t, res.body)
		})
	}
}

func TestAcceptedRequestBoundaries(t *testing.T) {
	for _, tt := range []struct {
		name    string
		records int
		pad     bool
	}{{"100 metrics", 100, false}, {"256KiB body", 1, true}} {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			records := make([]map[string]any, tt.records)
			for i := range records {
				records[i] = wireMetric("mem", nil, map[string]any{"used_percent": float64(i)}, initialTime)
			}
			body := batch(t, records...)
			if tt.pad {
				body = append(body, bytes.Repeat([]byte(" "), 256*1024-len(body))...)
			}
			requireStatus(t, h.post(t, "a", body), 204)
			if h.count(t, "metric_history") != 1 {
				t.Fatal("duplicate keys were not idempotent")
			}
			requireMetric(t, h.summary(t).Devices[0], 1, "0%", integer(0))
		})
	}
	for _, offset := range []int64{-86400, 0, 30} {
		t.Run(fmt.Sprintf("timestamp %+d", offset), func(t *testing.T) {
			h := newHarness(t)
			requireStatus(t, h.post(t, "a", mem(t, initialTime+offset, 42)), 204)
			got := h.summary(t)
			if offset != 0 {
				requireMetric(t, got.Devices[0], 1, "--", nil)
			} else {
				requireMetric(t, got.Devices[0], 1, "42%", integer(42))
			}
		})
	}
}

func TestRetriesOutOfOrderAndCanonicalTags(t *testing.T) {
	h := newHarness(t)
	first := batch(t, wireMetric("cpu", map[string]string{"host": "old", "cpu": "cpu-total", "region": "home"}, map[string]any{"usage_active": 42}, initialTime))
	requireStatus(t, h.post(t, "a", first), 204)
	h.advance(10)
	// Different host, different JSON key order and conflicting retry value must retain the first value.
	duplicate := []byte(`{"metrics":[{"timestamp":1792454400,"fields":{"usage_active":99},"tags":{"region":"home","cpu":"cpu-total","host":"renamed"},"name":"cpu"}]}`)
	requireStatus(t, h.post(t, "a", duplicate), 204)
	var v float64
	var received int64
	if err := h.db.QueryRow("SELECT value,received_at FROM metric_history").Scan(&v, &received); err != nil || v != 42 || received != initialTime {
		t.Fatal("retry changed first historical value", err)
	}
	if err := h.db.QueryRow("SELECT value,received_at FROM metric_latest").Scan(&v, &received); err != nil || v != 42 || received != initialTime {
		t.Fatal("retry refreshed latest", err)
	}
	if seen := h.lastSeen(t, "a"); seen.Int64 != initialTime+10 {
		t.Fatal("valid retry did not prove reachability")
	}
	old := batch(t, wireMetric("cpu", map[string]string{"cpu": "cpu-total", "region": "home"}, map[string]any{"usage_active": 1}, initialTime-10))
	requireStatus(t, h.post(t, "a", old), 204)
	if h.count(t, "metric_history") != 2 || h.count(t, "metric_latest") != 1 {
		t.Fatal("old history or series dedupe wrong")
	}
	requireMetric(t, h.summary(t).Devices[0], 0, "22%", integer(22))
	newer := batch(t, wireMetric("cpu", map[string]string{"cpu": "cpu-total", "region": "home"}, map[string]any{"usage_active": 55}, initialTime+10))
	requireStatus(t, h.post(t, "a", newer), 204)
	requireMetric(t, h.summary(t).Devices[0], 0, "33%", integer(33))
}

func TestOnlineBoundaryOldReplayAndIndependentTTL(t *testing.T) {
	h := newHarness(t)
	requireStatus(t, h.post(t, "a", fixture(t)), 204)
	h.advance(45)
	if h.summary(t).DeviceCount != 1 {
		t.Fatal("45s boundary should be online")
	}
	h.advance(1)
	if h.summary(t).DeviceCount != 0 {
		t.Fatal("offline device was retained")
	}
	requireStatus(t, h.post(t, "a", fixture(t)), 204)
	got := h.summary(t)
	if got.DeviceCount != 1 || got.Devices[0].Uptime != "--" {
		t.Fatal("old-only replay reachability or uptime wrong")
	}
	for i := 0; i < 3; i++ {
		requireMetric(t, got.Devices[0], i, "--", nil)
	}
	h.advance(1)
	requireStatus(t, h.post(t, "a", mem(t, h.clock.Load(), 33)), 204)
	got = h.summary(t)
	requireMetric(t, got.Devices[0], 0, "--", nil)
	requireMetric(t, got.Devices[0], 1, "38%", integer(38))
	requireMetric(t, got.Devices[0], 2, "--", nil)
	if got.Devices[0].Uptime != "--" {
		t.Fatal("expired uptime displayed")
	}
}

func TestCompleteSnapshotsMembershipAndHiddenAgent(t *testing.T) {
	h := newHarness(t)
	requireStatus(t, h.post(t, "hidden", mem(t, initialTime, 70)), 204)
	if h.summary(t).DeviceCount != 0 || !h.lastSeen(t, "hidden").Valid {
		t.Fatal("hidden agent not accepted/filtered")
	}
	requireStatus(t, h.post(t, "a", mem(t, initialTime, 42)), 204)
	if got := h.summary(t); got.DeviceCount != 1 || got.Devices[0].Name != "NAS" {
		t.Fatal("missing first member")
	}
	h.advance(46)
	requireStatus(t, h.post(t, "b", mem(t, h.clock.Load(), 25)), 204)
	if got := h.summary(t); got.DeviceCount != 1 || got.Devices[0].Name != "PC" {
		t.Fatal("same-count membership replacement failed")
	}
	requireStatus(t, h.post(t, "a", mem(t, h.clock.Load(), 44)), 204)
	if got := h.summary(t); got.DeviceCount != 2 || got.Devices[0].Name != "NAS" || got.Devices[1].Name != "PC" {
		t.Fatal("restored order wrong")
	}
	h.advance(46)
	if got := h.summary(t); got.DeviceCount != 0 {
		t.Fatal("all-offline snapshot not empty")
	}
}

func TestProjectionMissingAmbiguousAndOverflow(t *testing.T) {
	h := newHarness(t)
	body := batch(t,
		wireMetric("cpu", map[string]string{"cpu": "cpu0"}, map[string]any{"usage_active": 70}, initialTime),
		wireMetric("mem", nil, map[string]any{"used_percent": 1e30}, initialTime),
		wireMetric("disk", map[string]string{"path": "/other"}, map[string]any{"used_percent": 50}, initialTime),
		wireMetric("system", nil, map[string]any{"uptime": 1.5}, initialTime))
	requireStatus(t, h.post(t, "a", body), 204)
	card := h.summary(t).Devices[0]
	requireMetric(t, card, 0, "--", nil)
	requireMetric(t, card, 1, "OVR", nil)
	requireMetric(t, card, 2, "--", nil)
	if card.Uptime != "--" {
		t.Fatal("fractional uptime accepted")
	}
	h.advance(1)
	body = batch(t, wireMetric("disk", map[string]string{"path": "/data", "device": "a"}, map[string]any{"used_percent": 1}, initialTime), wireMetric("disk", map[string]string{"path": "/data", "device": "b"}, map[string]any{"used_percent": 2}, h.clock.Load()))
	requireStatus(t, h.post(t, "a", body), 204)
	requireMetric(t, h.summary(t).Devices[0], 2, "--", nil)
	h.advance(45)
	requireStatus(t, h.post(t, "a", mem(t, h.clock.Load(), 7)), 204)
	requireMetric(t, h.summary(t).Devices[0], 2, "--", nil) // Only one candidate is fresh; selection is still ambiguous.
}

func TestFourCardsSizeAndConfigReload(t *testing.T) {
	h := newHarness(t, func(c *config.Config) {
		token, err := config.GenerateToken()
		if err != nil {
			t.Fatal(err)
		}
		c.Agents = append(c.Agents, config.Agent{ID: "d", WriteToken: token})
		c.Panel.Devices = nil
		for i, id := range []string{"a", "b", "hidden", "d"} {
			c.Panel.Devices = append(c.Panel.Devices, config.DefaultDevice(id, strings.Repeat(string(rune('A'+i)), 8), "/data"))
		}
	})
	for _, id := range []string{"a", "b", "hidden", "d"} {
		body := batch(t, wireMetric("cpu", map[string]string{"cpu": "cpu-total"}, map[string]any{"usage_active": 26.5}, initialTime), wireMetric("mem", nil, map[string]any{"used_percent": 42.1}, initialTime), wireMetric("disk", map[string]string{"path": "/data"}, map[string]any{"used_percent": 61}, initialTime), wireMetric("system", nil, map[string]any{"uptime": 999*86400 + 23*3600}, initialTime))
		requireStatus(t, h.post(t, id, body), 204)
	}
	got := h.summary(t)
	if got.DeviceCount != 4 {
		t.Fatal("four-card limit wrong")
	}
	for i, d := range got.Devices {
		if d.Name != strings.Repeat(string(rune('A'+i)), 8) || d.Uptime != "999d 23h" {
			t.Fatal("maximum card labels wrong")
		}
	}
	c := h.cfg
	c.Panel.Devices = []config.Device{config.DefaultDevice("d", "LAST", "/missing"), config.DefaultDevice("a", "FIRST", "/data")}
	h.restart(t, c)
	got = h.summary(t)
	if got.DeviceCount != 2 || got.Devices[0].Name != "LAST" || got.Devices[1].Name != "FIRST" {
		t.Fatal("display configuration not reloaded")
	}
	requireMetric(t, got.Devices[0], 2, "--", nil)
	requireMetric(t, got.Devices[1], 2, "61%", integer(61))
}

func TestStorageFailureRollbackAndRecovery(t *testing.T) {
	h := newHarness(t)
	requireStatus(t, h.post(t, "a", mem(t, initialTime, 42)), 204)
	before := h.dump(t)
	// AFTER trigger fails the second record after the first record and last_seen were written.
	h.exec(t, `CREATE TRIGGER injected_failure AFTER INSERT ON metric_history WHEN NEW.field='fail' BEGIN SELECT RAISE(ABORT,'private injected storage failure'); END`)
	h.advance(5)
	body := batch(t, wireMetric("mem", nil, map[string]any{"used_percent": 99}, h.clock.Load()), wireMetric("system", nil, map[string]any{"fail": 1}, h.clock.Load()))
	res := h.post(t, "a", body)
	requireError(t, res, 503, "storage_unavailable")
	h.assertUnchanged(t, before)
	requireMetric(t, h.summary(t).Devices[0], 1, "42%", integer(42))
	if strings.Contains(string(res.body)+h.logs.String(), "private injected storage failure") {
		t.Fatal("storage detail leaked")
	}
	h.exec(t, "DROP TRIGGER injected_failure")
	requireStatus(t, h.post(t, "a", body), 204)
	requireMetric(t, h.summary(t).Devices[0], 1, "71%", integer(71))
	if seen := h.lastSeen(t, "a"); seen.Int64 != h.clock.Load() {
		t.Fatal("recovered request not committed")
	}
}

func TestStorageUnavailableNeverBecomesEmptySuccess(t *testing.T) {
	h := newHarness(t)
	requireStatus(t, h.post(t, "a", mem(t, initialTime, 42)), 204)
	before := h.dump(t)
	if err := h.repo.Close(); err != nil {
		t.Fatal(err)
	}
	requireError(t, h.do(t, "GET", summaryPath, h.cfg.ReadToken, nil), 503, "storage_unavailable")
	requireError(t, h.do(t, "GET", "/readyz", "", nil), 503, "storage_unavailable")
	requireStatus(t, h.do(t, "GET", "/healthz", "", nil), 200)
	requireError(t, h.post(t, "a", mem(t, initialTime+1, 99)), 503, "storage_unavailable")
	h.assertUnchanged(t, before)
	h.restart(t, h.cfg)
	requireStatus(t, h.do(t, "GET", "/readyz", "", nil), 200)
	requireMetric(t, h.summary(t).Devices[0], 1, "42%", integer(42))
}

func TestSeriesLimitRejectsWholeRequest(t *testing.T) {
	h := newHarness(t)
	for start := 0; start < 256; start += 100 {
		var records []map[string]any
		for i := start; i < start+100 && i < 256; i++ {
			records = append(records, wireMetric("disk", map[string]string{"path": fmt.Sprintf("/mount/%d", i)}, map[string]any{"used_percent": float64(i % 100)}, initialTime))
		}
		requireStatus(t, h.post(t, "a", batch(t, records...)), 204)
	}
	if h.count(t, "metric_latest") != 256 {
		t.Fatal("failed to seed 256 series")
	}
	before := h.dump(t)
	h.advance(2)
	body := batch(t, wireMetric("disk", map[string]string{"path": "/mount/0"}, map[string]any{"used_percent": 99}, h.clock.Load()), wireMetric("disk", map[string]string{"path": "/new"}, map[string]any{"used_percent": 1}, h.clock.Load()))
	requireError(t, h.post(t, "a", body), 422, "series_limit_exceeded")
	h.assertUnchanged(t, before)
	// Reusing an existing series stays permitted at capacity.
	requireStatus(t, h.post(t, "a", batch(t, wireMetric("disk", map[string]string{"path": "/mount/0"}, map[string]any{"used_percent": 99}, h.clock.Load()))), 204)
}

func TestConcurrentRetriesRateLimitAndIsolation(t *testing.T) {
	h := newHarness(t)
	body := mem(t, initialTime, 42)
	const requests = 16
	results := make(chan int, requests)
	errs := make(chan error, requests)
	var wg sync.WaitGroup
	for i := 0; i < requests; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, err := http.NewRequest("POST", h.server.URL+ingestPath, bytes.NewReader(body))
			if err != nil {
				errs <- err
				return
			}
			req.Header.Set("Authorization", "Bearer "+h.token("a"))
			req.Header.Set("Content-Type", "application/json")
			res, err := h.client.Do(req)
			if err != nil {
				errs <- err
				return
			}
			_, err = io.Copy(io.Discard, res.Body)
			res.Body.Close()
			if err != nil {
				errs <- err
				return
			}
			results <- res.StatusCode
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	accepted, limited := 0, 0
	for status := range results {
		switch status {
		case 204:
			accepted++
		case 429:
			limited++
		default:
			t.Fatalf("unexpected concurrent status %d", status)
		}
	}
	if accepted != 10 || limited != 6 || h.count(t, "metric_history") != 1 || h.count(t, "metric_latest") != 1 {
		t.Fatalf("concurrent retry counts: accepted=%d limited=%d", accepted, limited)
	}
	before := h.dump(t)
	res := h.post(t, "a", mem(t, initialTime+1, 99))
	requireError(t, res, 429, "rate_limited")
	if res.header.Get("Retry-After") != "1" {
		t.Fatal("no retry hint")
	}
	h.assertUnchanged(t, before)
	requireStatus(t, h.post(t, "b", mem(t, initialTime, 25)), 204)
	h.advance(1)
	requireStatus(t, h.post(t, "a", mem(t, h.clock.Load(), 44)), 204)
	requireMetric(t, h.summary(t).Devices[0], 1, "43%", integer(43))
}

func TestRestartPersistenceTokenRotationAndRevocation(t *testing.T) {
	h := newHarness(t)
	requireStatus(t, h.post(t, "a", fixture(t)), 204)
	before := h.dump(t)
	oldRead, oldWrite := h.cfg.ReadToken, h.token("a")
	c := h.cfg
	var err error
	c.ReadToken, err = config.GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	c.Agents = append([]config.Agent(nil), c.Agents...)
	c.Agents[0].WriteToken, err = config.GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	h.restart(t, c)
	h.assertUnchanged(t, before)
	requireError(t, h.do(t, "GET", summaryPath, oldRead, nil), 401, "unauthorized")
	requireError(t, h.do(t, "POST", ingestPath, oldWrite, bytes.NewReader(fixture(t))), 401, "unauthorized")
	if h.summary(t).DeviceCount != 1 {
		t.Fatal("restart lost current data")
	}
	h.advance(1)
	requireStatus(t, h.post(t, "a", mem(t, h.clock.Load(), 50)), 204)
	revoked := h.token("a")
	c = h.cfg
	c.Agents = append([]config.Agent(nil), c.Agents[1:]...)
	c.Panel.Devices = []config.Device{config.DefaultDevice("b", "PC", "/")}
	before = h.dump(t)
	h.restart(t, c)
	requireError(t, h.do(t, "POST", ingestPath, revoked, bytes.NewReader(mem(t, h.clock.Load(), 99))), 401, "unauthorized")
	h.assertUnchanged(t, before)
	if h.summary(t).DeviceCount != 0 {
		t.Fatal("removed device still exposed")
	}
	var original int64
	if err = h.db.QueryRow("SELECT created_at FROM devices WHERE device_id='a'").Scan(&original); err != nil || original != initialTime {
		t.Fatal("historical device changed", err)
	}
}

func TestRollingAveragesWindowIsolationAndExpiry(t *testing.T) {
	h := newHarness(t)
	// The oldest point is outside the window; the exact cutoff is included.
	for i, point := range []struct {
		offset         int64
		cpu, mem, disk int
	}{{-301, 100, 100, 99}, {-300, 10, 20, 80}, {-10, 30, 40, 60}, {0, 80, 90, 50}} {
		body := batch(t,
			wireMetric("cpu", map[string]string{"cpu": "cpu-total"}, map[string]any{"usage_active": point.cpu}, initialTime+point.offset),
			wireMetric("cpu", map[string]string{"cpu": "cpu0"}, map[string]any{"usage_active": 100}, initialTime+point.offset),
			wireMetric("mem", nil, map[string]any{"used_percent": point.mem, "free": 999}, initialTime+point.offset),
			wireMetric("disk", map[string]string{"path": "/data"}, map[string]any{"used_percent": point.disk}, initialTime+point.offset))
		requireStatus(t, h.post(t, "a", body), 204)
		if i == 0 {
			requireMetric(t, h.summary(t).Devices[0], 0, "--", nil)
		}
	}
	requireStatus(t, h.post(t, "b", mem(t, initialTime, 7)), 204)
	got := h.summary(t)
	requireMetric(t, got.Devices[0], 0, "40%", integer(40))
	requireMetric(t, got.Devices[0], 1, "50%", integer(50))
	requireMetric(t, got.Devices[0], 2, "50%", integer(50))
	requireMetric(t, got.Devices[1], 1, "7%", integer(7))

	// Advancing the clock drops the boundary point without any new CPU/MEM.
	h.advance(1)
	got = h.summary(t)
	requireMetric(t, got.Devices[0], 0, "55%", integer(55))
	requireMetric(t, got.Devices[0], 1, "65%", integer(65))

	// Fresh disk proves the device is online, but the old CPU/MEM averages expire.
	h.advance(45)
	requireStatus(t, h.post(t, "a", batch(t, wireMetric("disk", map[string]string{"path": "/data"}, map[string]any{"used_percent": 51}, h.clock.Load()))), 204)
	got = h.summary(t)
	requireMetric(t, got.Devices[0], 0, "--", nil)
	requireMetric(t, got.Devices[0], 1, "--", nil)
	requireMetric(t, got.Devices[0], 2, "51%", integer(51))
}
