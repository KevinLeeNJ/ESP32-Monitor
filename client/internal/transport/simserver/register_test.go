package simserver

import (
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"
)

func newRegistration(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	s := NewRegistration(nil)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return s, ts
}

func createDevice(t *testing.T, ts *httptest.Server, name string) (id, code string) {
	t.Helper()
	resp, body := request(t, ts, "POST", "/api/devices", "", map[string]string{"name": name}, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create device: %d %v", resp.StatusCode, body)
	}
	id, _ = body["id"].(string)
	code, _ = body["code"].(string)
	if id == "" || !regexp.MustCompile(`^[1-9][0-9]{5}$`).MatchString(code) {
		t.Fatalf("create device response: %v", body)
	}
	return id, code
}

func TestRegistrationFlow(t *testing.T) {
	_, ts := newRegistration(t)

	resp, body := request(t, ts, "GET", "/api/devices", "", nil, nil)
	if resp.StatusCode != http.StatusOK || len(body["devices"].([]any)) != 0 {
		t.Fatalf("empty list: %d %v", resp.StatusCode, body)
	}
	resp, _ = request(t, ts, "GET", "/", "", nil, nil)
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("webui: %d %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}

	id, code := createDevice(t, ts, "Kevin 的 MacBook")

	if resp, body := request(t, ts, "GET", "/v2/collector/profile", "tok", nil, nil); resp.StatusCode != http.StatusUnauthorized || body["error"] != "unauthorized" {
		t.Fatalf("unregistered token: %d %v", resp.StatusCode, body)
	}
	if resp, body := request(t, ts, "POST", "/v2/collector/register", "", map[string]string{"code": "000000"}, nil); resp.StatusCode != http.StatusNotFound || body["error"] != "invalid_code" {
		t.Fatalf("wrong code: %d %v", resp.StatusCode, body)
	}

	resp, body = request(t, ts, "POST", "/v2/collector/register", "", map[string]string{"code": code}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("claim: %d %v", resp.StatusCode, body)
	}
	token, _ := body["token"].(string)
	if len(token) != 64 || body["device_id"] != id || body["name"] != "Kevin 的 MacBook" {
		t.Fatalf("claim response: %v", body)
	}

	// The code is single use.
	if resp, _ := request(t, ts, "POST", "/v2/collector/register", "", map[string]string{"code": code}, nil); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("code reuse: %d", resp.StatusCode)
	}
	// The issued token authenticates collector requests and starts unregistered.
	if resp, body := request(t, ts, "GET", "/v2/collector/profile", token, nil, nil); resp.StatusCode != http.StatusNotFound || body["error"] != "profile_required" {
		t.Fatalf("registered token: %d %v", resp.StatusCode, body)
	}

	resp, body = request(t, ts, "GET", "/api/devices", "", nil, nil)
	devices := body["devices"].([]any)
	device := devices[0].(map[string]any)
	if len(devices) != 1 || device["status"] != "registered" || device["code"] != nil || device["registered"] != true {
		t.Fatalf("list after claim: %v", body)
	}

	// Deleting the device revokes its token immediately.
	if resp, _ := request(t, ts, "DELETE", "/api/devices/"+id, "", nil, nil); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: %d", resp.StatusCode)
	}
	if resp, _ := request(t, ts, "GET", "/v2/collector/profile", token, nil, nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("revoked token: %d", resp.StatusCode)
	}
}

func TestRegistrationExpiryAndReissue(t *testing.T) {
	s, ts := newRegistration(t)
	t0 := time.Now()
	s.now = func() time.Time { return t0 }
	_, code := createDevice(t, ts, "Mac mini")

	s.now = func() time.Time { return t0.Add(pairingTTL + time.Second) }
	if resp, body := request(t, ts, "POST", "/v2/collector/register", "", map[string]string{"code": code}, nil); resp.StatusCode != http.StatusGone || body["error"] != "code_expired" {
		t.Fatalf("expired code: %d %v", resp.StatusCode, body)
	}
	_, body := request(t, ts, "GET", "/api/devices", "", nil, nil)
	if devices := body["devices"].([]any); devices[0].(map[string]any)["status"] != "expired" {
		t.Fatalf("expired status: %v", body)
	}

	id := body["devices"].([]any)[0].(map[string]any)["id"].(string)
	resp, updated := request(t, ts, "POST", "/api/devices/"+id+"/code", "", nil, nil)
	if resp.StatusCode != http.StatusOK || updated["status"] != "pending" || updated["code"] == "" {
		t.Fatalf("reissue: %d %v", resp.StatusCode, updated)
	}
	fresh, _ := updated["code"].(string)
	if resp, body := request(t, ts, "POST", "/v2/collector/register", "", map[string]string{"code": fresh}, nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("claim reissued code: %d %v", resp.StatusCode, body)
	}
}

func TestRegistrationRotatesToken(t *testing.T) {
	_, ts := newRegistration(t)
	id, code := createDevice(t, ts, "Studio")
	_, body := request(t, ts, "POST", "/v2/collector/register", "", map[string]string{"code": code}, nil)
	first := body["token"].(string)

	resp, updated := request(t, ts, "POST", "/api/devices/"+id+"/code", "", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("reissue for registered device: %d", resp.StatusCode)
	}
	_, body = request(t, ts, "POST", "/v2/collector/register", "", map[string]string{"code": updated["code"].(string)}, nil)
	second, _ := body["token"].(string)
	if second == "" || second == first {
		t.Fatalf("token not rotated: %q -> %q", first, second)
	}
	if resp, _ := request(t, ts, "GET", "/v2/collector/profile", first, nil, nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("old token still accepted: %d", resp.StatusCode)
	}
	if resp, _ := request(t, ts, "GET", "/v2/collector/profile", second, nil, nil); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("new token rejected: %d", resp.StatusCode)
	}
}

func TestRegistrationValidation(t *testing.T) {
	_, ts := newRegistration(t)
	for _, name := range []string{"", "   ", strings.Repeat("长", 65), "bad\x01name"} {
		resp, body := request(t, ts, "POST", "/api/devices", "", map[string]string{"name": name}, nil)
		if resp.StatusCode != http.StatusBadRequest || body["error"] != "invalid_name" {
			t.Fatalf("name %q: %d %v", name, resp.StatusCode, body)
		}
	}
	resp, body := request(t, ts, "GET", "/api/devices/missing", "", nil, nil)
	if resp.StatusCode != http.StatusNotFound || body["error"] != "device_not_found" {
		t.Fatalf("unknown id: %d %v", resp.StatusCode, body)
	}
	id, _ := createDevice(t, ts, "Method check")
	if resp, _ := request(t, ts, "GET", "/api/devices/"+id, "", nil, nil); resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("unsupported method: %d", resp.StatusCode)
	}
}

func TestWebUIDisabledInLegacyModes(t *testing.T) {
	legacy := New("", nil)
	ts := httptest.NewServer(legacy.Handler())
	defer ts.Close()
	for _, path := range []string{"/api/devices", "/v2/collector/register"} {
		resp, _ := request(t, ts, "GET", path, "", nil, nil)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s in legacy mode: %d", path, resp.StatusCode)
		}
	}
	if resp, _ := request(t, ts, "GET", "/", "", nil, nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("webui in legacy mode: %d", resp.StatusCode)
	}
}

func TestWebUIHasAddFlow(t *testing.T) {
	s, _ := newRegistration(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	page, _ := io.ReadAll(rec.Result().Body)
	for _, want := range []string{"添加设备", "设备名称", "验证码", "copy-button"} {
		if !strings.Contains(string(page), want) {
			t.Fatalf("webui missing %q", want)
		}
	}
}

func TestCodeIsNotLogged(t *testing.T) {
	var logs strings.Builder
	s := NewRegistration(log.New(&logs, "", 0))
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	_, code := createDevice(t, ts, "Log check")
	request(t, ts, "POST", "/v2/collector/register", "", map[string]string{"code": code}, nil)
	if strings.Contains(logs.String(), code) {
		t.Fatalf("verification code leaked to logs: %s", logs.String())
	}
}
