package simserver

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	v2 "esp32-monitor/client/internal/collector/uploadv2"
)

func postSampleBatch(t *testing.T, ts *httptest.Server, token, id string) *http.Response {
	t.Helper()
	resp, _ := request(t, ts, "POST", "/v2/collector/samples", token, batch(1, id, baseSample(timeNow().Unix())), nil)
	return resp
}

func feedOf(t *testing.T, ts *httptest.Server, query string) (int, []map[string]any, map[string]any) {
	t.Helper()
	resp, body := request(t, ts, "GET", "/api/batches"+query, "", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/batches%s: %d %v", query, resp.StatusCode, body)
	}
	list, _ := body["batches"].([]any)
	entries := make([]map[string]any, 0, len(list))
	for _, e := range list {
		entries = append(entries, e.(map[string]any))
	}
	return resp.StatusCode, entries, body
}

func TestBatchesFeed(t *testing.T) {
	_, ts := newTest(t)

	if _, entries, _ := feedOf(t, ts, ""); len(entries) != 0 {
		t.Fatalf("empty feed: %v", entries)
	}
	token := "tok-0123456789abcdef"
	request(t, ts, "PUT", "/v2/collector/profile", token, minimalProfile(), map[string]string{"If-None-Match": "*"})
	if resp := postSampleBatch(t, ts, token, "00000000000000000000000000000001"); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("batch: %d", resp.StatusCode)
	}

	_, entries, _ := feedOf(t, ts, "")
	if len(entries) != 1 || entries[0]["revision"] != float64(1) || entries[0]["samples"] != float64(1) {
		t.Fatalf("feed entry: %v", entries)
	}
	if entries[0]["device"] == "" {
		t.Fatalf("device label missing: %v", entries[0])
	}
	var got v2.SampleBatch
	if err := json.Unmarshal([]byte(entries[0]["raw"].(string)), &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != "00000000000000000000000000000001" || len(got.Samples) != 1 || *got.Samples[0].CPUUsage != 1250 {
		t.Fatalf("raw round trip: %+v", got)
	}

	// A duplicate replay keeps the feed stable.
	if resp := postSampleBatch(t, ts, token, "00000000000000000000000000000001"); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("duplicate: %d", resp.StatusCode)
	}
	if _, entries, _ := feedOf(t, ts, ""); len(entries) != 1 {
		t.Fatalf("feed after duplicate: %v", entries)
	}

	// Incremental fetch by cursor.
	if resp := postSampleBatch(t, ts, token, "00000000000000000000000000000002"); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("second batch: %d", resp.StatusCode)
	}
	_, entries, body := feedOf(t, ts, "?after=1")
	if len(entries) != 1 || entries[0]["seq"] != float64(2) || body["latest"] != float64(2) {
		t.Fatalf("incremental fetch: %v %v", entries, body)
	}
	if _, entries, _ = feedOf(t, ts, "?after=2"); len(entries) != 0 {
		t.Fatalf("no entries after latest: %v", entries)
	}

	// A cursor newer than the simulator sequence means restart: full resend.
	if _, entries, body = feedOf(t, ts, "?after=999"); body["reset"] != true || len(entries) != 2 {
		t.Fatalf("reset fetch: %v %v", entries, body)
	}
	if resp, body := request(t, ts, "GET", "/api/batches?after=abc", "", nil, nil); resp.StatusCode != http.StatusBadRequest || body["error"] != "invalid_after" {
		t.Fatalf("invalid cursor: %d %v", resp.StatusCode, body)
	}
}

func TestBatchesFeedRegistrationLabel(t *testing.T) {
	s := NewRegistration(nil)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	id, code := createDevice(t, ts, "测试机")
	resp, body := request(t, ts, "POST", "/v2/collector/register", "", map[string]string{"code": code}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("claim: %d %v", resp.StatusCode, body)
	}
	token, _ := body["token"].(string)
	request(t, ts, "PUT", "/v2/collector/profile", token, minimalProfile(), map[string]string{"If-None-Match": "*"})
	if resp := postSampleBatch(t, ts, token, "0000000000000000000000000000000a"); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("batch: %d", resp.StatusCode)
	}

	_, entries, _ := feedOf(t, ts, "")
	if len(entries) != 1 || entries[0]["device"] != "测试机" || entries[0]["device_id"] != id {
		t.Fatalf("registration feed entry: %v", entries)
	}
}

func TestBatchesFeedRingLimit(t *testing.T) {
	s, ts := newTest(t)
	request(t, ts, "PUT", "/v2/collector/profile", "tok", minimalProfile(), map[string]string{"If-None-Match": "*"})
	for i := 1; i <= maxFeedEntries+5; i++ {
		if resp := postSampleBatch(t, ts, "tok", fmt.Sprintf("%032x", i)); resp.StatusCode != http.StatusNoContent {
			t.Fatalf("batch %d: %d", i, resp.StatusCode)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// 105 entries posted, the ring keeps the newest 100 (seq 6..105).
	if len(s.feed) != maxFeedEntries || s.feed[0].Seq != 6 {
		t.Fatalf("ring state: len=%d first=%d", len(s.feed), s.feed[0].Seq)
	}
}
