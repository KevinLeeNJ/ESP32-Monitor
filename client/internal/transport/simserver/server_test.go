package simserver

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	v2 "esp32-monitor/client/internal/collector/uploadv2"
)

func newTest(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	s := New("", nil)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return s, ts
}

func request(t *testing.T, ts *httptest.Server, method, path, token string, body any, header map[string]string) (*http.Response, map[string]any) {
	t.Helper()
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, ts.URL+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/json")
	for key, value := range header {
		req.Header.Set(key, value)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out := map[string]any{}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp, out
}

func minimalProfile() v2.Profile {
	return v2.Profile{Version: 2, IntervalSeconds: 10}
}

func TestBatteryPairProfileAndSample(t *testing.T) {
	_, ts := newTest(t)
	p := minimalProfile()
	p.Inventory.Batteries = []v2.ObjectInfo{{ID: 1}}
	p.Enabled = []v2.UploadSelection{{Metric: "battery.level", Objects: []uint16{1}}, {Metric: "battery.charging", Objects: []uint16{1}}}
	if resp, body := request(t, ts, "PUT", "/v2/collector/profile", "tok", p, map[string]string{"If-None-Match": "*"}); resp.StatusCode != 200 {
		t.Fatalf("profile: %d %v", resp.StatusCode, body)
	}
	sample := baseSample(time.Now().Unix())
	level, charging := uint16(8000), true
	sample.Batteries = []v2.BatterySample{{ID: 1, Level: &level, Charging: &charging}}
	if resp, body := request(t, ts, "POST", "/v2/collector/samples", "tok", batch(1, "0123456789abcdef0123456789abcdef", sample), nil); resp.StatusCode != 204 {
		t.Fatalf("sample: %d %v", resp.StatusCode, body)
	}
}

func baseSample(t int64) v2.Sample {
	c, m, u := uint16(1250), uint16(6200), uint32(86400)
	return v2.Sample{Timestamp: t, CPUUsage: &c, MemoryUsage: &m, Uptime: &u}
}

func batch(rev uint64, id string, samples ...v2.Sample) v2.SampleBatch {
	return v2.SampleBatch{Revision: rev, ID: id, Samples: samples}
}

func TestProfileLifecycle(t *testing.T) {
	_, ts := newTest(t)

	if resp, body := request(t, ts, "GET", "/v2/collector/profile", "tok", nil, nil); resp.StatusCode != 404 || body["error"] != "profile_required" {
		t.Fatalf("unregistered GET: %d %v", resp.StatusCode, body)
	}
	if resp, body := request(t, ts, "PUT", "/v2/collector/profile", "tok", minimalProfile(), nil); resp.StatusCode != 412 {
		t.Fatalf("PUT without If-None-Match: %d %v", resp.StatusCode, body)
	}
	resp, _ := request(t, ts, "PUT", "/v2/collector/profile", "tok", minimalProfile(), map[string]string{"If-None-Match": "*"})
	if resp.StatusCode != 200 || resp.Header.Get("ETag") != `"1"` {
		t.Fatalf("PUT create: %d %q", resp.StatusCode, resp.Header.Get("ETag"))
	}
	if resp, body := request(t, ts, "GET", "/v2/collector/profile", "tok", nil, map[string]string{"If-None-Match": `"1"`}); resp.StatusCode != 304 {
		t.Fatalf("GET conditional: %d %v", resp.StatusCode, body)
	}
	// PUT retry with the same content but the wrong condition fails; with the
	// current If-Match it is accepted and returns the same revision.
	if resp, body := request(t, ts, "PUT", "/v2/collector/profile", "tok", minimalProfile(), map[string]string{"If-None-Match": "*"}); resp.StatusCode != 412 {
		t.Fatalf("second PUT with If-None-Match: %d %v", resp.StatusCode, body)
	}
	resp, body := request(t, ts, "PUT", "/v2/collector/profile", "tok", minimalProfile(), map[string]string{"If-Match": `"1"`})
	if resp.StatusCode != 200 || body["r"] != float64(1) {
		t.Fatalf("PUT retry: %d %v", resp.StatusCode, body)
	}
	changed := minimalProfile()
	changed.IntervalSeconds = 30
	if resp, body := request(t, ts, "PUT", "/v2/collector/profile", "tok", changed, map[string]string{"If-Match": `"1"`}); resp.StatusCode != 409 {
		t.Fatalf("PUT with changed content: %d %v", resp.StatusCode, body)
	}
}

func TestAuth(t *testing.T) {
	_, ts := newTest(t)
	if resp, body := request(t, ts, "GET", "/v2/collector/profile", "", nil, nil); resp.StatusCode != 401 || body["error"] != "unauthorized" {
		t.Fatalf("no token: %d %v", resp.StatusCode, body)
	}
	s := New("secret", nil)
	ts2 := httptest.NewServer(s.Handler())
	defer ts2.Close()
	if resp, _ := request(t, ts2, "GET", "/v2/collector/profile", "wrong", nil, nil); resp.StatusCode != 401 {
		t.Fatalf("wrong token: %d", resp.StatusCode)
	}
}

func TestSampleValidation(t *testing.T) {
	_, ts := newTest(t)
	profile := minimalProfile()
	profile.Inventory.Interfaces = []v2.InterfaceInfo{{ID: 4, Medium: ptr("wifi")}}
	profile.Enabled = []v2.UploadSelection{
		{Metric: "interface.medium", Objects: []uint16{4}},
		{Metric: "interface.upload_rate", Objects: []uint16{4}},
		{Metric: "memory.swap_used"},
	}
	request(t, ts, "PUT", "/v2/collector/profile", "tok", profile, map[string]string{"If-None-Match": "*"})
	now := timeNow()
	tx := uint64(2048)
	sample := baseSample(now.Unix())
	sample.Interfaces = []v2.InterfaceSample{{ID: 4, UploadRate: &tx}}

	if resp, body := request(t, ts, "POST", "/v2/collector/samples", "tok", batch(9, "0123456789abcdef0123456789abcdef", sample), nil); resp.StatusCode != 409 || body["error"] != "profile_outdated" {
		t.Fatalf("stale revision: %d %v", resp.StatusCode, body)
	}
	ok := batch(1, "0123456789abcdef0123456789abcdef", sample)
	if resp, _ := request(t, ts, "POST", "/v2/collector/samples", "tok", ok, nil); resp.StatusCode != 204 {
		t.Fatalf("valid batch: %d", resp.StatusCode)
	}
	// Same body replays as 204 without side effects; a different body with the
	// same ID conflicts.
	if resp, _ := request(t, ts, "POST", "/v2/collector/samples", "tok", ok, nil); resp.StatusCode != 204 {
		t.Fatalf("duplicate batch: %d", resp.StatusCode)
	}
	sample2 := baseSample(now.Unix())
	sample2.MemoryUsage = ptrU16(6300)
	if resp, body := request(t, ts, "POST", "/v2/collector/samples", "tok", batch(1, "0123456789abcdef0123456789abcdef", sample2), nil); resp.StatusCode != 409 || body["error"] != "batch_id_conflict" {
		t.Fatalf("conflicting batch: %d %v", resp.StatusCode, body)
	}

	// memory.swap_used was selected, memory.app was not.
	enabled := baseSample(now.Unix())
	enabled.Memory = &v2.MemorySample{SwapUsed: ptrU64(1024)}
	if resp, _ := request(t, ts, "POST", "/v2/collector/samples", "tok", batch(1, "0123456789abcdef0123456789abcffe", enabled), nil); resp.StatusCode != 204 {
		t.Fatalf("selected optional: %d", resp.StatusCode)
	}
	disabled := baseSample(now.Unix())
	disabled.Memory = &v2.MemorySample{App: ptrU64(1024)}
	if resp, body := request(t, ts, "POST", "/v2/collector/samples", "tok", batch(1, "0123456789abcdef0123456789abcffd", disabled), nil); resp.StatusCode != 422 || body["error"] != "metric_not_enabled:memory.app" {
		t.Fatalf("unselected optional: %d %v", resp.StatusCode, body)
	}
	// Selected interface field on an undeclared object is rejected.
	ghost := baseSample(now.Unix())
	ghost.Interfaces = []v2.InterfaceSample{{ID: 9, UploadRate: &tx}}
	if resp, _ := request(t, ts, "POST", "/v2/collector/samples", "tok", batch(1, "0123456789abcdef0123456789abcffc", ghost), nil); resp.StatusCode != 422 {
		t.Fatalf("undeclared object: %d", resp.StatusCode)
	}
	// Required fields and ranges.
	missing := v2.Sample{Timestamp: now.Unix(), CPUUsage: ptrU16(1), MemoryUsage: ptrU16(1)}
	if resp, body := request(t, ts, "POST", "/v2/collector/samples", "tok", batch(1, "0123456789abcdef0123456789abcffb", missing), nil); resp.StatusCode != 422 || body["error"] != "required_metric_missing" {
		t.Fatalf("missing required: %d %v", resp.StatusCode, body)
	}
	future := baseSample(now.Unix() + 120)
	if resp, body := request(t, ts, "POST", "/v2/collector/samples", "tok", batch(1, "0123456789abcdef0123456789abcffa", future), nil); resp.StatusCode != 422 || body["error"] != "timestamp_in_future" {
		t.Fatalf("future timestamp: %d %v", resp.StatusCode, body)
	}
	old := baseSample(now.Unix() - 90000)
	if resp, body := request(t, ts, "POST", "/v2/collector/samples", "tok", batch(1, "0123456789abcdef0123456789abcff9", old), nil); resp.StatusCode != 422 || body["error"] != "timestamp_too_old" {
		t.Fatalf("old timestamp: %d %v", resp.StatusCode, body)
	}
	if resp, _ := request(t, ts, "POST", "/v2/collector/samples", "tok", batch(1, "NOTHEX"), nil); resp.StatusCode != 400 {
		t.Fatalf("bad batch id: %d", resp.StatusCode)
	}
}

func TestPatchLifecycle(t *testing.T) {
	_, ts := newTest(t)
	request(t, ts, "PUT", "/v2/collector/profile", "tok", minimalProfile(), map[string]string{"If-None-Match": "*"})

	patch := v2.ProfilePatch{{Op: "replace", Path: "/interval_seconds", Value: json.RawMessage(`30`)}}
	if resp, _ := request(t, ts, "PATCH", "/v2/collector/profile", "tok", patch, nil); resp.StatusCode != 412 {
		t.Fatalf("PATCH without If-Match: %d", resp.StatusCode)
	}
	if resp, _ := request(t, ts, "PATCH", "/v2/collector/profile", "tok", patch, map[string]string{"If-Match": `"9"`}); resp.StatusCode != 412 {
		t.Fatalf("PATCH with stale If-Match: %d", resp.StatusCode)
	}
	resp, body := request(t, ts, "PATCH", "/v2/collector/profile", "tok", patch, map[string]string{"If-Match": `"1"`})
	if resp.StatusCode != 200 || body["r"] != float64(2) || resp.Header.Get("ETag") != `"2"` {
		t.Fatalf("PATCH: %d %v %q", resp.StatusCode, body, resp.Header.Get("ETag"))
	}
	// No semantic change → same revision (doc §6.2).
	noOp := v2.ProfilePatch{{Op: "replace", Path: "/interval_seconds", Value: json.RawMessage(`30`)}}
	resp, body = request(t, ts, "PATCH", "/v2/collector/profile", "tok", noOp, map[string]string{"If-Match": `"2"`})
	if resp.StatusCode != 200 || body["r"] != float64(2) {
		t.Fatalf("no-op PATCH: %d %v", resp.StatusCode, body)
	}
	// Forbidden targets.
	for _, bad := range []v2.ProfilePatch{
		{{Op: "replace", Path: "/inventory", Value: json.RawMessage(`{}`)}},
		{{Op: "replace", Path: "/inventory/cpu", Value: json.RawMessage(`{"model":"x"}`)}},
		{{Op: "replace", Path: "/version", Value: json.RawMessage(`3`)}},
		{{Op: "remove", Path: "/interval_seconds"}},
	} {
		if resp, _ := request(t, ts, "PATCH", "/v2/collector/profile", "tok", bad, map[string]string{"If-Match": `"2"`}); resp.StatusCode != 422 {
			t.Fatalf("forbidden patch %s: %d", bad[0].Path, resp.StatusCode)
		}
	}
	// Add an object, then remove it by index.
	addGPU := v2.ProfilePatch{{Op: "add", Path: "/inventory/gpus/-", Value: json.RawMessage(`{"id":3}`)}}
	resp, body = request(t, ts, "PATCH", "/v2/collector/profile", "tok", addGPU, map[string]string{"If-Match": `"2"`})
	if resp.StatusCode != 200 || body["r"] != float64(3) {
		t.Fatalf("add gpu: %d %v", resp.StatusCode, body)
	}
	// Unknown metric selection is rejected.
	badSel := v2.ProfilePatch{{Op: "replace", Path: "/enabled", Value: json.RawMessage(`[{"metric":"volume.total"}]`)}}
	if resp, _ := request(t, ts, "PATCH", "/v2/collector/profile", "tok", badSel, map[string]string{"If-Match": `"3"`}); resp.StatusCode != 422 {
		t.Fatalf("unknown metric: %d", resp.StatusCode)
	}
	removeGPU := v2.ProfilePatch{{Op: "remove", Path: "/inventory/gpus/0"}}
	resp, body = request(t, ts, "PATCH", "/v2/collector/profile", "tok", removeGPU, map[string]string{"If-Match": `"3"`})
	if resp.StatusCode != 200 || body["r"] != float64(4) {
		t.Fatalf("remove gpu: %d %v", resp.StatusCode, body)
	}
	// Fetch the profile and verify the round trip.
	resp, body = request(t, ts, "GET", "/v2/collector/profile", "tok", nil, nil)
	if resp.StatusCode != 200 || resp.Header.Get("ETag") != `"4"` {
		t.Fatalf("GET after patch: %d", resp.StatusCode)
	}
	raw, _ := json.Marshal(body)
	var got v2.Profile
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.IntervalSeconds != 30 || len(got.Inventory.GPUs) != 0 {
		t.Fatalf("profile content after patch: %+v", got)
	}
}

func ptr[T any](v T) *T { return &v }

func ptrU16(v uint16) *uint16 { return &v }
func ptrU64(v uint64) *uint64 { return &v }
func timeNow() time.Time      { return time.Now() }
