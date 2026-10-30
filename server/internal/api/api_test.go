package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"esp32-monitor/server/internal/config"
	"esp32-monitor/server/internal/storage"
)

const testNow int64 = 1793232000
const permanentPassword = "correct horse battery staple"

type harness struct {
	t              *testing.T
	h              *Handler
	store          *storage.Store
	cfg            config.Config
	now            int64
	password, csrf string
	cookie         *http.Cookie
	logs           bytes.Buffer
}

func setup(t *testing.T) *harness {
	t.Helper()
	c := config.Defaults()
	c.Database = filepath.Join(t.TempDir(), "monitor.db")
	c.ReadToken = strings.Repeat("1", 64)
	repo, err := storage.Open(context.Background(), c.Database, nil, testNow)
	if err != nil {
		t.Fatal(err)
	}
	x := &harness{t: t, cfg: c, store: repo, now: testNow}
	t.Cleanup(func() { repo.Close() })
	x.restart()
	return x
}
func (x *harness) restart() {
	x.t.Helper()
	var output bytes.Buffer
	h, err := New(x.cfg, x.store, func() time.Time { return time.Unix(x.now, 0) }, slog.New(slog.NewTextHandler(&x.logs, nil)), &output)
	if err != nil {
		x.t.Fatal(err)
	}
	x.h = h
	if output.Len() > 0 {
		re := regexp.MustCompile(`WebUI 临时密码: ([0-9a-f]+)`)
		m := re.FindStringSubmatch(output.String())
		if m == nil {
			x.t.Fatal("missing bootstrap")
		}
		x.password = m[1]
	} else {
		x.password = ""
	}
}
func (x *harness) req(method, path, token, body string, headers ...string) *httptest.ResponseRecorder {
	x.t.Helper()
	r := httptest.NewRequest(method, "http://127.0.0.1:8080"+path, strings.NewReader(body))
	r.RemoteAddr = "127.0.0.1:10000"
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "http://127.0.0.1:8080")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	if x.cookie != nil {
		r.AddCookie(x.cookie)
	}
	if x.csrf != "" {
		r.Header.Set("X-CSRF-Token", x.csrf)
	}
	for i := 0; i < len(headers); i += 2 {
		r.Header.Set(headers[i], headers[i+1])
	}
	w := httptest.NewRecorder()
	x.h.ServeHTTP(w, r)
	return w
}
func requireStatus(t *testing.T, w *httptest.ResponseRecorder, want int) {
	t.Helper()
	if w.Code != want {
		t.Fatalf("got %d want %d: %s", w.Code, want, w.Body.String())
	}
}

func TestLocalHostAccess(t *testing.T) {
	x := setup(t)
	for _, tc := range []struct {
		host, remote string
		want         int
	}{
		{"127.0.0.1:8080", "127.0.0.1:1234", 200},
		{"localhost:8080", "127.0.0.1:1234", 200},
		{"192.168.110.2:8080", "192.168.110.3:1234", 200},
		{"192.168.110.2:8080", "172.18.0.1:1234", 200},
		{"[::1]:8080", "[::1]:1234", 200},
		{"evil.example:8080", "192.168.110.3:1234", 403},
		{"192.168.110.2:8080", "8.8.8.8:1234", 403},
	} {
		t.Run(tc.host+"/"+tc.remote, func(t *testing.T) {
			r := httptest.NewRequest("GET", "http://"+tc.host+"/healthz", nil)
			r.RemoteAddr = tc.remote
			w := httptest.NewRecorder()
			x.h.ServeHTTP(w, r)
			requireStatus(t, w, tc.want)
		})
	}
	r := httptest.NewRequest("POST", "http://192.168.110.2:8080/api/auth/login", strings.NewReader(fmt.Sprintf(`{"password":%q}`, x.password)))
	r.RemoteAddr = "192.168.110.3:1234"
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "http://192.168.110.2:8080")
	w := httptest.NewRecorder()
	x.h.ServeHTTP(w, r)
	requireStatus(t, w, 200)
	if cookies := w.Result().Cookies(); len(cookies) != 1 || cookies[0].Secure {
		t.Fatal("local HTTP login must issue a usable non-Secure session cookie")
	}
}
func (x *harness) state(w *httptest.ResponseRecorder) {
	x.t.Helper()
	requireStatus(x.t, w, 200)
	var state struct {
		CSRF string `json:"csrf_token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &state); err != nil {
		x.t.Fatal(err)
	}
	x.csrf = state.CSRF
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		x.t.Fatal("missing cookie")
	}
	x.cookie = cookies[0]
}
func (x *harness) login() {
	x.t.Helper()
	x.state(x.req("POST", "/api/auth/login", "", fmt.Sprintf(`{"password":%q}`, x.password)))
}
func (x *harness) ready() {
	x.login()
	x.state(x.req("POST", "/api/auth/password", "", fmt.Sprintf(`{"current_password":%q,"new_password":%q}`, x.password, permanentPassword)))
}
func TestReadTokenRequiresAuthenticatedSession(t *testing.T) {
	x := setup(t)
	requireStatus(t, x.req("POST", "/api/read-token", "", `{}`), 401)
	x.login()
	requireStatus(t, x.req("POST", "/api/read-token", "", `{}`), 403)
	x.state(x.req("POST", "/api/auth/password", "", fmt.Sprintf(`{"current_password":%q,"new_password":%q}`, x.password, permanentPassword)))
	requireStatus(t, x.req("POST", "/api/read-token", "", `{}`, "X-CSRF-Token", "invalid"), 403)
	requireStatus(t, x.req("GET", "/api/read-token", "", ""), 405)
	w := x.req("POST", "/api/read-token", "", `{}`)
	requireStatus(t, w, 200)
	var body struct {
		ReadToken string `json:"read_token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.ReadToken != x.cfg.ReadToken {
		t.Fatal("incorrect read token response")
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("token response must not be cached")
	}
}
func (x *harness) device() (string, string) {
	x.t.Helper()
	w := x.req("POST", "/api/devices", "", `{"name":"NAS"}`)
	requireStatus(x.t, w, 201)
	var d struct{ ID, Code string }
	_ = json.Unmarshal(w.Body.Bytes(), &d)
	w = x.req("POST", "/v2/collector/register", "", fmt.Sprintf(`{"code":%q}`, d.Code))
	requireStatus(x.t, w, 200)
	var result struct{ Token string }
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	return d.ID, result.Token
}

const basicProfile = `{"version":2,"interval_seconds":10,"enabled":[],"inventory":{}}`

func (x *harness) profile(token string) {
	x.t.Helper()
	w := x.req("PUT", "/v2/collector/profile", token, basicProfile, "If-None-Match", "*")
	requireStatus(x.t, w, 200)
	if w.Header().Get("ETag") != `"1"` {
		x.t.Fatal(w.Header())
	}
}
func batch(id string, rev uint64, ts int64, cpu int) string {
	return fmt.Sprintf(`{"r":%d,"id":%q,"s":[{"t":%d,"c":%d,"m":6200,"u":86400}]}`, rev, id, ts, cpu)
}
func (x *harness) count(table string) int {
	x.t.Helper()
	var n int
	if err := x.store.Database().QueryRow("SELECT count(*) FROM " + table).Scan(&n); err != nil {
		x.t.Fatal(err)
	}
	return n
}
func TestPasswordLifecycleAndSessionSecurity(t *testing.T) {
	x := setup(t)
	first := x.password
	if len(first) != 32 {
		t.Fatal("weak bootstrap")
	}
	x.login()
	requireStatus(t, x.req("GET", "/api/devices", "", ""), 403)
	requireStatus(t, x.req("POST", "/api/auth/password", "", fmt.Sprintf(`{"current_password":%q,"new_password":"short"}`, first)), 422)
	oldCookie := x.cookie
	oldCSRF := x.csrf
	x.state(x.req("POST", "/api/auth/password", "", fmt.Sprintf(`{"current_password":%q,"new_password":%q}`, first, permanentPassword)))
	if x.cookie.Value == oldCookie.Value || !x.cookie.HttpOnly || x.cookie.SameSite != http.SameSiteStrictMode || x.cookie.MaxAge != 604800 {
		t.Fatal("cookie security or rotation")
	}
	validCookie, validCSRF := x.cookie, x.csrf
	x.cookie = oldCookie
	x.csrf = oldCSRF
	requireStatus(t, x.req("GET", "/api/devices", "", ""), 401)
	x.cookie = validCookie
	x.csrf = validCSRF
	x.restart()
	if x.password != "" {
		t.Fatal("generated password after completed change")
	}
	requireStatus(t, x.req("GET", "/api/devices", "", ""), 200)
	requireStatus(t, x.req("POST", "/api/devices", "", `{"name":"a"}`, "X-CSRF-Token", ""), 403)
	requireStatus(t, x.req("POST", "/api/devices", "", `{"name":"a"}`, "Origin", "https://evil.test"), 403)
	requireStatus(t, x.req("POST", "/api/auth/logout", "", `{}`), 204)
	requireStatus(t, x.req("GET", "/api/devices", "", ""), 401)
	if strings.Contains(x.logs.String(), first) || strings.Contains(x.logs.String(), permanentPassword) || strings.Contains(x.logs.String(), validCookie.Value) {
		t.Fatal("secret in audit logs")
	}
}
func TestBootstrapRestartAndRateLimits(t *testing.T) {
	x := setup(t)
	old := x.password
	x.login()
	x.restart()
	if x.password == old {
		t.Fatal("bootstrap not rotated")
	}
	requireStatus(t, x.req("GET", "/api/auth/state", "", ""), 401)
	x.cookie = nil
	x.csrf = ""
	for i := 0; i < 5; i++ {
		requireStatus(t, x.req("POST", "/api/auth/login", "", `{"password":"incorrect"}`), 401)
	}
	w := x.req("POST", "/api/auth/login", "", `{"password":"incorrect"}`)
	requireStatus(t, w, 429)
	if w.Header().Get("Retry-After") == "" {
		t.Fatal("no retry-after")
	}
}
func TestReverseProxyCookieAndOrigin(t *testing.T) {
	x := setup(t)
	request := func(origin, host string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "http://"+host+"/api/auth/login", strings.NewReader(fmt.Sprintf(`{"password":%q}`, x.password)))
		r.RemoteAddr = "127.0.0.1:1234"
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", origin)
		r.Header.Set("X-Forwarded-Proto", "http")
		r.Header.Set("X-Forwarded-For", "203.0.113.1")
		w := httptest.NewRecorder()
		x.h.ServeHTTP(w, r)
		return w
	}
	w := request("https://monitor.example.com", "monitor.example.com")
	requireStatus(t, w, 200)
	c := w.Result().Cookies()[0]
	if !c.Secure || !c.HttpOnly || c.Name != "__Host-monitor_session" || c.Domain != "" {
		t.Fatal("unsafe proxy cookie")
	}
	requireStatus(t, request("http://monitor.example.com", "monitor.example.com"), 403)
	requireStatus(t, request("http://evil.test", "evil.test"), 403)
	requireStatus(t, x.req("POST", "/api/auth/login", "", fmt.Sprintf(`{"password":%q}`, x.password), "X-Forwarded-Proto", "https"), 200)
}
func TestRegistrationExpiryRotationDeletion(t *testing.T) {
	x := setup(t)
	x.ready()
	id, token := x.device()
	x.profile(token)
	w := x.req("POST", "/api/devices/"+id+"/code", "", `{}`)
	requireStatus(t, w, 200)
	var d struct{ Code string }
	_ = json.Unmarshal(w.Body.Bytes(), &d)
	x.now += 601
	requireStatus(t, x.req("POST", "/v2/collector/register", "", fmt.Sprintf(`{"code":%q}`, d.Code)), 410)
	w = x.req("POST", "/api/devices/"+id+"/code", "", `{}`)
	requireStatus(t, w, 200)
	_ = json.Unmarshal(w.Body.Bytes(), &d)
	w = x.req("POST", "/v2/collector/register", "", fmt.Sprintf(`{"code":%q}`, d.Code))
	requireStatus(t, w, 200)
	var result struct{ Token string }
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	requireStatus(t, x.req("GET", "/v2/collector/profile", token, ""), 401)
	requireStatus(t, x.req("GET", "/v2/collector/profile", result.Token, ""), 200)
	requireStatus(t, x.req("POST", "/v2/collector/register", "", fmt.Sprintf(`{"code":%q}`, d.Code)), 404)
	requireStatus(t, x.req("DELETE", "/api/devices/"+id, "", ""), 204)
	requireStatus(t, x.req("GET", "/v2/collector/profile", result.Token, ""), 401)
	if x.count("v2_profiles") != 0 {
		t.Fatal("orphan profiles")
	}
}
func TestAtomicBatchDedupRevisionAndRestart(t *testing.T) {
	x := setup(t)
	x.ready()
	id, token := x.device()
	x.profile(token)
	body := batch(strings.Repeat("a", 32), 1, x.now, 1250)
	requireStatus(t, x.req("POST", "/v2/collector/samples", token, body), 204)
	if x.count("v2_batches") != 1 || x.count("v2_values") != 3 {
		t.Fatal("missing atomic data")
	}
	x.now += 10
	requireStatus(t, x.req("POST", "/v2/collector/samples", token, body), 204)
	if x.count("v2_values") != 3 {
		t.Fatal("duplicate values")
	}
	changed := strings.Replace(body, "1250", "1251", 1)
	requireStatus(t, x.req("POST", "/v2/collector/samples", token, changed), 409)
	patch := `[{"op":"replace","path":"/interval_seconds","value":15}]`
	w := x.req("PATCH", "/v2/collector/profile", token, patch, "If-Match", `"1"`)
	requireStatus(t, w, 200)
	if w.Header().Get("ETag") != `"2"` {
		t.Fatal("revision")
	}
	requireStatus(t, x.req("POST", "/v2/collector/samples", token, body), 204)
	requireStatus(t, x.req("POST", "/v2/collector/samples", token, batch(strings.Repeat("b", 32), 1, x.now, 1250)), 409)
	x.restart()
	requireStatus(t, x.req("GET", "/api/devices", "", ""), 200)
	requireStatus(t, x.req("POST", "/v2/collector/samples", token, body), 204)
	requireStatus(t, x.req("GET", "/v2/collector/profile", token, ""), 200)
	w = x.req("PATCH", "/v2/collector/profile", token, patch, "If-Match", `"2"`)
	requireStatus(t, w, 200)
	if w.Header().Get("ETag") != `"2"` {
		t.Fatal("no-op bumped revision")
	}
	// A bad second sample rolls back both the values and idempotency record.
	bad := fmt.Sprintf(`{"r":2,"id":%q,"s":[{"t":%d,"c":1000,"m":1000,"u":100},{"t":%d,"c":10001,"m":0,"u":0}]}`, strings.Repeat("c", 32), x.now, x.now+1)
	requireStatus(t, x.req("POST", "/v2/collector/samples", token, bad), 422)
	if x.count("v2_values") != 3 || x.count("v2_batches") != 1 {
		t.Fatal("partial commit")
	}
	requireStatus(t, x.req("POST", "/v2/collector/samples", token, batch(strings.Repeat("d", 32), 2, testNow, 1251)), 409)
	x.now += 60
	w = x.req("GET", "/v2/devices/summary", x.cfg.ReadToken, "")
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), `"device_count":0`) {
		t.Fatal("duplicate refreshed online")
	}
	w = x.req("GET", "/api/devices/"+id+"/metrics", "", "")
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), `"latest":{}`) {
		t.Fatal("stale values exposed as fresh")
	}
}
func TestStrictValidationAndV1Removed(t *testing.T) {
	x := setup(t)
	x.ready()
	_, token := x.device()
	x.profile(token)
	for _, path := range []string{"/v1/ingest/telegraf", "/v1/devices/summary", "/v1/enroll", "/v1/samples"} {
		requireStatus(t, x.req("POST", path, token, `{}`), 404)
	}
	cases := []string{
		`{"r":1,"r":1,"id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","s":[]}`,
		`{"r":1,"id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","s":[{"t":1793232000,"c":0,"m":0,"u":0,"mem":{"u":null}}]}`,
		`{"r":1,"id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","s":[{"t":1793232000,"c":0,"m":0,"u":0,"extra":1}]}`,
		`{"r":1,"id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","s":[{"t":1793232000,"c":0,"m":0,"u":0,"mem":{"u":42}}]}`,
		`{"r":1,"id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","s":[{"t":1793232000,"c":0,"m":0,"u":0,"mem":{"p":0}}]}`,
	}
	for _, body := range cases {
		w := x.req("POST", "/v2/collector/samples", token, body)
		if w.Code < 400 {
			t.Fatal("accepted invalid:", body)
		}
	}
	requireStatus(t, x.req("POST", "/v2/collector/samples", token, batch(strings.Repeat("b", 32), 1, x.now+31, 0)), 422)
	requireStatus(t, x.req("POST", "/v2/collector/samples", token, batch(strings.Repeat("c", 32), 1, x.now-86401, 0)), 422)
	requireStatus(t, x.req("POST", "/v2/collector/samples", token, strings.Repeat(" ", maxBody+1)), 413)
	requireStatus(t, x.req("POST", "/v2/collector/samples", token, batch(strings.Repeat("d", 32), 1, x.now, 0)+`{}`), 400)
	if x.count("v2_batches") != 0 {
		t.Fatal("bad batch persisted")
	}
}
func TestDiskOnlyZeroAndProfileProjectionPruning(t *testing.T) {
	x := setup(t)
	x.ready()
	id, token := x.device()
	profile := `{"version":2,"interval_seconds":10,"enabled":[{"metric":"drive.total","objects":[3]},{"metric":"drive.used","objects":[3]}],"inventory":{"drives":[{"id":3,"total":"1000"}]}}`
	requireStatus(t, x.req("PUT", "/v2/collector/profile", token, profile, "If-None-Match", "*"), 200)
	requireStatus(t, x.req("POST", "/v2/collector/samples", token, fmt.Sprintf(`{"r":1,"id":%q,"s":[{"t":%d,"c":0,"m":0,"u":0,"disk":[{"i":3,"u":"0"}]}]}`, strings.Repeat("a", 32), x.now)), 204)
	requireStatus(t, x.req("PUT", "/api/devices/"+id+"/panel", "", `{"enabled":true,"name":"NAS","disk_id":3}`), 200)
	w := x.req("GET", "/v2/devices/summary", x.cfg.ReadToken, "")
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), `"name":"DISK","value":"0%","percent":0`) {
		t.Fatal("disk zero lost:", w.Body.String())
	}
	patch := `[{"op":"replace","path":"/enabled","value":[]},{"op":"remove","path":"/inventory/drives/0"}]`
	requireStatus(t, x.req("PATCH", "/v2/collector/profile", token, patch, "If-Match", `"1"`), 200)
	w = x.req("GET", "/api/devices/"+id+"/metrics", "", "")
	if strings.Contains(w.Body.String(), "drive.used/") {
		t.Fatal("disabled projection survived")
	}
	if x.count("v2_profiles") != 2 || x.count("v2_values") != 4 {
		t.Fatal("lost historical revision")
	}
}
func TestConcurrentUploadAndDatabaseFailure(t *testing.T) {
	x := setup(t)
	x.ready()
	_, token := x.device()
	x.profile(token)
	body := batch(strings.Repeat("a", 32), 1, x.now, 100)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); requireStatus(t, x.req("POST", "/v2/collector/samples", token, body), 204) }()
	}
	wg.Wait()
	if x.count("v2_batches") != 1 {
		t.Fatal("concurrent duplicate")
	}
	_, err := x.store.Database().Exec(`CREATE TRIGGER fail_batch BEFORE INSERT ON v2_batches BEGIN SELECT RAISE(ABORT,'injected secret'); END`)
	if err != nil {
		t.Fatal(err)
	}
	w := x.req("POST", "/v2/collector/samples", token, batch(strings.Repeat("b", 32), 1, x.now+1, 200))
	requireStatus(t, w, 503)
	if strings.Contains(w.Body.String()+x.logs.String(), "injected secret") || x.count("v2_values") != 3 {
		t.Fatal("failed write leaked or committed")
	}
}

func TestClientDisableRemovesEmptyScalarGroups(t *testing.T) {
	x := setup(t)
	x.ready()
	_, token := x.device()
	x.profile(token)
	patch := `[{"op":"replace","path":"/enabled","value":[{"metric":"system.os"},{"metric":"cpu.model"}]},{"op":"add","path":"/inventory/os/family","value":"darwin"},{"op":"add","path":"/inventory/os/name","value":"macOS"},{"op":"add","path":"/inventory/os/version","value":"test"},{"op":"add","path":"/inventory/cpu/model","value":"CPU"},{"op":"replace","path":"/inventory/memory_total","value":"18446744073709551615"}]`
	requireStatus(t, x.req("PATCH", "/v2/collector/profile", token, patch, "If-Match", `"1"`), 200)
	patch = `[{"op":"replace","path":"/enabled","value":[]},{"op":"remove","path":"/inventory/os/family"},{"op":"remove","path":"/inventory/os/name"},{"op":"remove","path":"/inventory/os/version"},{"op":"remove","path":"/inventory/cpu/model"},{"op":"remove","path":"/inventory/memory_total"}]`
	requireStatus(t, x.req("PATCH", "/v2/collector/profile", token, patch, "If-Match", `"2"`), 200)
	w := x.req("GET", "/v2/collector/profile", token, "")
	requireStatus(t, w, 200)
	if strings.Contains(w.Body.String(), `"os"`) || strings.Contains(w.Body.String(), `"cpu"`) {
		t.Fatal("empty metadata container survived")
	}
}

func TestClientWireContractParity(t *testing.T) {
	client, err := os.ReadFile("../../../client/internal/collector/uploadv2/types.go")
	if err != nil {
		t.Fatal(err)
	}
	server, err := os.ReadFile("../uploadv2/types.go")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(client, server) {
		t.Fatal("Server wire definitions diverged from current Client")
	}
}
func TestStringIntegersAndObservationBounds(t *testing.T) {
	x := setup(t)
	x.ready()
	_, token := x.device()
	x.profile(token)
	for i, value := range []string{"01", "+1", "-1", "1e3", "18446744073709551616"} {
		body := fmt.Sprintf(`{"r":1,"id":"%032x","s":[{"t":%d,"c":0,"m":0,"u":0,"mem":{"u":%q}}]}`, i+1, x.now, value)
		requireStatus(t, x.req("POST", "/v2/collector/samples", token, body), 422)
	}
	body := fmt.Sprintf(`{"r":1,"id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","s":[{"t":%d,"c":0,"m":0,"u":0,"mem":{"u":"18446744073709551615"}}]}`, x.now)
	requireStatus(t, x.req("POST", "/v2/collector/samples", token, body), 204)
	w := x.req("GET", "/v2/collector/profile", token, "")
	requireStatus(t, w, 200)
	if x.count("v2_values") != 4 {
		t.Fatal("uint64 rejected or rounded")
	}
}
func TestCleanupPreservesLateBatchAndExpiresOldRecords(t *testing.T) {
	x := setup(t)
	x.ready()
	_, token := x.device()
	x.profile(token)
	oldBatch := batch(strings.Repeat("a", 32), 1, x.now-86390, 1250)
	requireStatus(t, x.req("POST", "/v2/collector/samples", token, oldBatch), 204)
	x.now += 60
	if err := x.h.Cleanup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if x.count("v2_batches") != 1 || x.count("v2_values") != 3 {
		t.Fatal("late-upload dedup removed too soon")
	}
	requireStatus(t, x.req("POST", "/v2/collector/samples", token, oldBatch), 204)
	x.now += 86400
	if err := x.h.Cleanup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if x.count("v2_batches") != 0 || x.count("v2_values") != 0 || x.count("v2_profiles") != 1 {
		t.Fatal("retention cleanup")
	}
}

func TestWireFieldNamesAreCaseSensitive(t *testing.T) {
	x := setup(t)
	x.ready()
	_, token := x.device()
	x.profile(token)
	body := fmt.Sprintf(`{"r":1,"R":1,"id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","s":[{"t":%d,"c":0,"m":0,"u":0}]}`, x.now)
	requireStatus(t, x.req("POST", "/v2/collector/samples", token, body), 422)
}

func TestObjectDirectoryAcceptsUnselectedMetadataAndPatchesNames(t *testing.T) {
	x := setup(t)
	x.ready()
	id, token := x.device()
	profile := `{"version":2,"interval_seconds":10,"enabled":[],"inventory":{"objects":[{"id":3,"kind":"drive","name":"Samsung SSD","model":"990 PRO"},{"id":5,"kind":"interface","name":"以太网 2","model":"Intel Ethernet Controller"},{"id":7,"kind":"gpu","name":"NVIDIA GPU","model":"RTX 5080"}]}}`
	requireStatus(t, x.req("PUT", "/v2/collector/profile", token, profile, "If-None-Match", "*"), 200)
	w := x.req("GET", "/api/devices/"+id+"/metrics", "", "")
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), "以太网 2") || !strings.Contains(w.Body.String(), "RTX 5080") {
		t.Fatal("metadata not exposed before first sample")
	}
	patch := `[{"op":"replace","path":"/inventory/objects/1/name","value":"Ethernet renamed"},{"op":"remove","path":"/inventory/objects/2"},{"op":"add","path":"/inventory/objects/-","value":{"id":8,"kind":"gpu","name":"Apple M4 Pro"}}]`
	requireStatus(t, x.req("PATCH", "/v2/collector/profile", token, patch, "If-Match", `"1"`), 200)
	x.restart()
	w = x.req("GET", "/v2/collector/profile", token, "")
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), "Ethernet renamed") || !strings.Contains(w.Body.String(), "Apple M4 Pro") || strings.Contains(w.Body.String(), "NVIDIA GPU") {
		t.Fatal("metadata PATCH or restart lost directory")
	}
	if x.count("v2_batches") != 0 {
		t.Fatal("metadata created samples")
	}
}
func TestObjectDirectoryValidation(t *testing.T) {
	for _, objects := range []string{
		`[{"id":0,"kind":"gpu","name":"GPU"}]`,
		`[{"id":1,"kind":"gpu","name":"GPU"},{"id":1,"kind":"interface","name":"en0"}]`,
		`[{"id":1,"kind":"unknown","name":"x"}]`,
		`[{"id":1,"kind":"gpu","name":""}]`,
		`[{"id":1,"kind":"gpu","name":"bad\u0000name"}]`,
		`[{"id":1,"kind":"gpu","name":"GPU","model":""}]`,
	} {
		t.Run(objects, func(t *testing.T) {
			x := setup(t)
			x.ready()
			_, token := x.device()
			body := fmt.Sprintf(`{"version":2,"interval_seconds":10,"enabled":[],"inventory":{"objects":%s}}`, objects)
			requireStatus(t, x.req("PUT", "/v2/collector/profile", token, body, "If-None-Match", "*"), 422)
			if x.count("v2_profiles") != 0 {
				t.Fatal("invalid directory persisted")
			}
		})
	}
	x := setup(t)
	x.ready()
	_, token := x.device()
	body := `{"version":2,"interval_seconds":10,"enabled":[{"metric":"gpu.usage","objects":[1]}],"inventory":{"gpus":[{"id":1}],"objects":[{"id":1,"kind":"interface","name":"en0"}]}}`
	requireStatus(t, x.req("PUT", "/v2/collector/profile", token, body, "If-None-Match", "*"), 422)
}
