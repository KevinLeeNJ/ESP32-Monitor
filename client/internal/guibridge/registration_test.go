package guibridge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestServerBase(t *testing.T) {
	cases := map[string]string{
		"https://nas.example.com":                      "https://nas.example.com",
		"https://nas.example.com/":                     "https://nas.example.com",
		"https://nas.example.com/monitor":              "https://nas.example.com/monitor",
		"https://nas.example.com/v2/collector/samples": "https://nas.example.com",
		"https://nas.example.com/v1/ingest/telegraf":   "https://nas.example.com/v1/ingest/telegraf",
		" http://127.0.0.1:18080/ ":                    "http://127.0.0.1:18080",
		"http://localhost:18080/v2/collector/samples":  "http://localhost:18080",
	}
	for input, want := range cases {
		if got := ServerBase(input); got != want {
			t.Fatalf("ServerBase(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestNormalizeServerBase(t *testing.T) {
	valid := map[string]string{
		"https://nas.example.com":                      "https://nas.example.com",
		"https://nas.example.com:8443/monitor/":        "https://nas.example.com:8443/monitor",
		"http://127.0.0.1:18080":                       "http://127.0.0.1:18080",
		"http://localhost:18080/":                      "http://localhost:18080",
		"http://nas.example.com":                       "http://nas.example.com",
		"http://[::1]:18080":                           "http://[::1]:18080",
		"https://nas.example.com/v2/collector/samples": "https://nas.example.com",
	}
	for input, want := range valid {
		if got, err := normalizeServerBase(input); err != nil || got != want {
			t.Fatalf("normalizeServerBase(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
	invalid := map[string]string{
		"ftp://nas.example.com":                      "invalid_server_url",
		"not a url":                                  "invalid_server_url",
		"https://user:pass@nas.local":                "invalid_server_url",
		"https://nas.example.com?x=1":                "invalid_server_url",
		"https://nas.example.com#frag":               "invalid_server_url",
		"https://nas.example.com/v1/ingest/telegraf": "v1_server_unsupported",
	}
	for input, want := range invalid {
		if _, err := normalizeServerBase(input); err == nil || err.Error() != want {
			t.Fatalf("normalizeServerBase(%q) error = %v, want %q", input, err, want)
		}
	}
}

func registrationServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/collector/register" || r.Method != http.MethodPost {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Fatalf("content type: %q", ct)
		}
		var request struct {
			Code string `json:"code"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.Code != "123456" {
			t.Fatalf("request body: %v %q", err, request.Code)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server
}

func TestExchangeRegistrationCode(t *testing.T) {
	token := strings.Repeat("a", 64)
	server := registrationServer(t, http.StatusOK, `{"device_id":"dev-1","name":"Kevin 的 MacBook","token":"`+token+`"}`)
	got, err := exchangeRegistrationCode(context.Background(), server.URL, "123456")
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if got.DeviceID != "dev-1" || got.Name != "Kevin 的 MacBook" || got.Token != token {
		t.Fatalf("response: %+v", got)
	}
}

func TestExchangeRegistrationErrors(t *testing.T) {
	cases := []struct {
		status int
		body   string
		want   string
	}{
		{http.StatusNotFound, `{"error":"invalid_code"}`, "invalid_code"},
		{http.StatusNotFound, `not found`, "registration_unsupported"},
		{http.StatusGone, `{"error":"code_expired"}`, "code_expired"},
		{http.StatusTooManyRequests, `{}`, "rate_limited"},
		{http.StatusInternalServerError, `{}`, "server_unreachable"},
		{http.StatusOK, `{"device_id":"d","name":"n","token":"short"}`, "invalid_server_response"},
		{http.StatusOK, `not json`, "invalid_server_response"},
	}
	for _, tc := range cases {
		server := registrationServer(t, tc.status, tc.body)
		_, err := exchangeRegistrationCode(context.Background(), server.URL, "123456")
		if err == nil || err.Error() != tc.want {
			t.Fatalf("status %d: error = %v, want %q", tc.status, err, tc.want)
		}
	}
}

func TestWritePrivateToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server-token")
	token := strings.Repeat("b", 64)
	if err := writePrivateToken(path, token); err != nil {
		t.Fatalf("write token: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// Windows has no POSIX mode bits; privacy comes from the user profile ACL.
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
	data, _ := os.ReadFile(path)
	if strings.TrimSpace(string(data)) != token {
		t.Fatalf("content = %q", data)
	}
}

func TestApplyRegistration(t *testing.T) {
	current := DefaultSettings()
	current.Collector.Enabled = true
	current.Collector.UploadURL = "https://old.example/v2/collector/samples"
	current.Companion.Enabled = true
	next := applyRegistration(current, "https://nas.example.com", registrationResponse{DeviceID: "dev-1", Name: "Mac"}, "/tmp/server-token")
	if next.Collector.UploadURL != "https://nas.example.com/v2/collector/samples" ||
		next.Collector.UploadTokenFile != "/tmp/server-token" ||
		next.Collector.DeviceID != "dev-1" ||
		next.Collector.DeviceName != "Mac" {
		t.Fatalf("settings: %+v", next.Collector)
	}
	if !next.Collector.Enabled || !next.Companion.Enabled {
		t.Fatalf("unrelated settings changed: %+v", next)
	}
}
