package github

import (
	"context"
	"encoding/json"
	"errors"
	"esp32-monitor/client/internal/providers"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

const calendarJSON = `{"data":{"user":{"login":"octocat","contributionsCollection":{"contributionCalendar":{"weeks":[{"contributionDays":[{"date":"2026-10-25","contributionCount":3,"contributionLevel":"SECOND_QUARTILE"},{"date":"2026-10-24","contributionCount":0,"contributionLevel":"NONE"},{"date":"2026-10-25","contributionCount":3,"contributionLevel":"SECOND_QUARTILE"}]}]}}},"rateLimit":{"remaining":100,"resetAt":"2026-10-25T13:00:00Z"}}}`

func fixture(t *testing.T) (*Provider, *time.Time, *int, *int) {
	t.Helper()
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	calls, status := 0, 200
	p, err := New(Options{Enabled: true, Login: "octocat", Now: func() time.Time { return now }, Token: func(context.Context, string) (string, error) { return "secret-do-not-export", nil }, HTTPClient: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.String() != endpoint || r.Method != "POST" || r.Header.Get("Authorization") != "Bearer secret-do-not-export" {
			t.Error("invalid authenticated GraphQL request")
		}
		var body struct {
			Query     string            `json:"query"`
			Variables map[string]string `json:"variables"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || body.Variables["login"] != "octocat" || !strings.Contains(body.Query, "contributionCalendar") {
			t.Error("invalid GraphQL payload")
		}
		return response(status, calendarJSON), nil
	})}})
	if err != nil {
		t.Fatal(err)
	}
	return p, &now, &calls, &status
}
func TestCalendarSchedulingAndPrivacy(t *testing.T) {
	p, now, calls, _ := fixture(t)
	s := p.Refresh(context.Background(), false)
	if s.Health.State != providers.Healthy || s.Data == nil || len(s.Data.Days) != 2 || s.Data.Total != 3 || *s.Data.Today != 3 || s.Data.Days[0].Date != "2026-10-24" {
		t.Fatalf("bad calendar: %+v", s)
	}
	p.Refresh(context.Background(), true)
	if *calls != 1 {
		t.Fatal("manual throttle bypassed")
	}
	*now = now.Add(time.Minute)
	p.Refresh(context.Background(), true)
	if *calls != 2 {
		t.Fatal("manual refresh not allowed")
	}
	*now = now.Add(14 * time.Minute)
	p.Refresh(context.Background(), false)
	if *calls != 2 {
		t.Fatal("auto refresh too soon")
	}
	*now = now.Add(time.Minute)
	p.Refresh(context.Background(), false)
	if *calls != 3 {
		t.Fatal("auto refresh not scheduled")
	}
	encoded, _ := json.Marshal(s)
	if strings.Contains(string(encoded), "secret") {
		t.Fatal("credential leak")
	}
	s.Data.Days[0].Count = 100
	if p.Snapshot().Data.Days[0].Count != 0 {
		t.Fatal("snapshot aliases provider state")
	}
}
func TestCacheAgeAndFailure(t *testing.T) {
	p, now, _, status := fixture(t)
	p.opts.CachePath = filepath.Join(t.TempDir(), "cache.json")
	p.Refresh(context.Background(), false)
	info, err := os.Stat(p.opts.CachePath)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("cache must be 0600")
	}
	bytes, _ := os.ReadFile(p.opts.CachePath)
	if strings.Contains(string(bytes), "secret") {
		t.Fatal("credential cached")
	}
	restored, err := New(p.opts)
	if err != nil || restored.Snapshot().Data == nil || restored.Snapshot().Health.State != providers.Degraded {
		t.Fatal("cache not restored honestly")
	}
	*now = now.Add(25 * time.Hour)
	*status = 503
	s := p.Refresh(context.Background(), false)
	if s.Data == nil || !s.Stale || s.Data.Today != nil || s.Health.State != providers.Degraded {
		t.Fatalf("stale cache lost: %+v", s)
	}
	*now = now.Add(7 * 24 * time.Hour)
	if p.Snapshot().Data != nil {
		t.Fatal("expired cache exposed")
	}
	other := p.opts
	other.Login = "different-user"
	otherP, _ := New(other)
	if otherP.Snapshot().Data != nil {
		t.Fatal("cross-account cache leak")
	}
}
func TestErrorsNeverCreateEmptyCalendar(t *testing.T) {
	for _, tc := range []struct {
		status     int
		body, code string
		state      providers.State
	}{
		{401, "", "auth_required", providers.AuthRequired}, {403, "", "permission_denied", providers.PermissionDenied}, {503, "", "upstream_error", providers.Error},
		{200, `{"data":{"user":null}}`, "user_not_found", providers.Error},
		{200, `{"errors":[{"type":"FORBIDDEN","message":"private-secret"}]}`, "permission_denied", providers.PermissionDenied},
		{200, `{"data":{"user":{"login":"octocat"}}}`, "invalid_response", providers.Error},
		{200, `not json`, "invalid_response", providers.Error},
	} {
		t.Run(tc.code+string(rune(tc.status)), func(t *testing.T) {
			p, _, _, _ := fixture(t)
			p.client.Transport = roundTrip(func(*http.Request) (*http.Response, error) { return response(tc.status, tc.body), nil })
			s := p.Refresh(context.Background(), false)
			if s.Data != nil || s.Health.ErrorCode != tc.code || s.Health.State != tc.state {
				t.Fatalf("unexpected error: %+v", s)
			}
		})
	}
}
func TestRateLimitResetAppliesToManualRefresh(t *testing.T) {
	p, now, calls, _ := fixture(t)
	p.client.Transport = roundTrip(func(*http.Request) (*http.Response, error) {
		*calls++
		r := response(429, "")
		r.Header.Set("Retry-After", "3600")
		return r, nil
	})
	p.Refresh(context.Background(), false)
	*now = now.Add(30 * time.Minute)
	p.Refresh(context.Background(), true)
	if *calls != 1 {
		t.Fatal("rate limit bypassed")
	}
	*now = now.Add(30 * time.Minute)
	p.Refresh(context.Background(), true)
	if *calls != 2 {
		t.Fatal("rate limit never cleared")
	}
}
func TestExhaustedSuccessfulQueryHonorsReset(t *testing.T) {
	p, now, calls, _ := fixture(t)
	p.client.Transport = roundTrip(func(*http.Request) (*http.Response, error) {
		*calls++
		return response(200, strings.Replace(calendarJSON, `"remaining":100`, `"remaining":0`, 1)), nil
	})
	p.Refresh(context.Background(), false)
	*now = now.Add(20 * time.Minute)
	p.Refresh(context.Background(), true)
	if *calls != 1 {
		t.Fatal("successful query reset ignored")
	}
}
func TestConcurrentRefreshCoalesces(t *testing.T) {
	p, _, calls, _ := fixture(t)
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() { defer wg.Done(); p.Refresh(context.Background(), false) }()
	}
	wg.Wait()
	if *calls != 1 {
		t.Fatal("duplicate refreshes")
	}
}
func TestSnapshotDoesNotWaitForNetwork(t *testing.T) {
	p, _, _, _ := fixture(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	p.client.Transport = roundTrip(func(*http.Request) (*http.Response, error) {
		close(entered)
		<-release
		return response(200, calendarJSON), nil
	})
	done := make(chan struct{})
	go func() { p.Refresh(context.Background(), false); close(done) }()
	<-entered
	read := make(chan struct{})
	go func() { p.Snapshot(); close(read) }()
	select {
	case <-read:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("snapshot blocked on network")
	}
	close(release)
	<-done
}
func TestInputValidationAndDisabled(t *testing.T) {
	for _, login := range []string{"../token", "-bad", "bad-", "bad--name", "a/b", strings.Repeat("a", 40)} {
		if _, err := New(Options{Enabled: true, Login: login}); err == nil {
			t.Errorf("accepted %q", login)
		}
	}
	p, _ := New(Options{})
	s := p.Refresh(context.Background(), false)
	if s.Health.State != providers.Disabled || s.Data != nil {
		t.Fatal("disabled provider read")
	}
	p, _ = New(Options{Enabled: true, Login: "octocat"})
	if p.Refresh(context.Background(), false).Health.State != providers.AuthRequired {
		t.Fatal("missing token")
	}
}

func TestOAuthRefreshNetworkFailureDoesNotRequireLogin(t *testing.T) {
	p, _, _, _ := fixture(t)
	p.opts.Token = func(context.Context, string) (string, error) { return "", errors.New("auth_network_error") }
	s := p.Refresh(context.Background(), false)
	if s.Health.State == providers.AuthRequired || s.Health.ErrorCode != "network_error" {
		t.Fatal(s)
	}
}
