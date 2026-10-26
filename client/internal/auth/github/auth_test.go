package github

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

type memoryStore struct {
	mu     sync.Mutex
	values map[string]string
	fail   bool
}

func (s *memoryStore) Load(_ context.Context, login string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.values[login]
	if !ok {
		return "", errors.New("not found")
	}
	return value, nil
}
func (s *memoryStore) Save(_ context.Context, login, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail {
		return errors.New("private credential error")
	}
	s.values[login] = value
	return nil
}
func (s *memoryStore) Delete(_ context.Context, login string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.values, login)
	return nil
}

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func authResponse(body string) *http.Response {
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
func fixture(t *testing.T) (*Client, *time.Time, *memoryStore, *string, *int, *int) {
	t.Helper()
	now := time.Date(2026, 10, 26, 8, 0, 0, 0, time.UTC)
	store := &memoryStore{values: map[string]string{}}
	reply := `{"access_token":"secret-access","token_type":"bearer","scope":"read:user"}`
	polls, opened := 0, 0
	client := New(Options{ClientID: "public-client-id", Store: store, Now: func() time.Time { return now }, OpenBrowser: func(_ context.Context, uri string) error {
		if uri != "https://github.com/login/device" {
			t.Error("unsafe browser URI")
		}
		opened++
		return nil
	}, HTTPClient: &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/login/device/code":
			r.ParseForm()
			if r.Form.Get("client_id") != "public-client-id" || r.Form.Get("scope") != "read:user" || r.Form.Get("client_secret") != "" {
				t.Error("incorrect device request")
			}
			return authResponse(`{"device_code":"secret-device","user_code":"ABCD-EFGH","verification_uri":"https://github.com/login/device","expires_in":900,"interval":5}`), nil
		case "/login/oauth/access_token":
			polls++
			r.ParseForm()
			if r.Form.Get("client_secret") != "" {
				t.Error("embedded client secret")
			}
			if r.Form.Get("grant_type") == "urn:ietf:params:oauth:grant-type:device_code" && r.Form.Get("device_code") != "secret-device" {
				t.Error("missing device code")
			}
			return authResponse(reply), nil
		case "/user":
			if r.Header.Get("Authorization") != "Bearer secret-access" {
				t.Error("identity not validated with issued token")
			}
			return authResponse(`{"login":"octocat","email":"private@example.com"}`), nil
		default:
			t.Errorf("unexpected auth request %s", r.URL)
			return authResponse(`{}`), nil
		}
	})}})
	return client, &now, store, &reply, &polls, &opened
}
func TestDefaultBrowserLoginAndCredentialHandoff(t *testing.T) {
	c, now, store, _, polls, opened := fixture(t)
	session, err := c.StartLogin(context.Background())
	if err != nil || session.State != AwaitingAuthorization || *opened != 1 {
		t.Fatalf("start: %+v %v", session, err)
	}
	if _, err := c.StartLogin(context.Background()); err == nil {
		t.Fatal("duplicate login accepted")
	}
	c.PollLogin(context.Background(), session.ID)
	if *polls != 0 {
		t.Fatal("polled before interval")
	}
	*now = now.Add(5 * time.Second)
	session, err = c.PollLogin(context.Background(), session.ID)
	if err != nil || session.State != Authorized || session.Login != "octocat" {
		t.Fatalf("finish: %+v %v", session, err)
	}
	token, err := c.Token(context.Background(), "octocat")
	if err != nil || token != "secret-access" {
		t.Fatal("provider cannot use persisted token")
	}
	bytes, _ := json.Marshal(session)
	if strings.Contains(string(bytes), "secret") || strings.Contains(string(bytes), "private") || session.UserCode != "" {
		t.Fatal("login snapshot leaks secret")
	}
	if store.values["octocat"] == "" {
		t.Fatal("credential saved under wrong identity")
	}
	if err := c.Logout(context.Background(), "octocat"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Token(context.Background(), "octocat"); err == nil || c.Status().State != SignedOut {
		t.Fatal("local logout failed")
	}
}
func TestPendingSlowdownDenialAndExpiry(t *testing.T) {
	c, now, _, reply, polls, _ := fixture(t)
	session, _ := c.StartLogin(context.Background())
	*reply = `{"error":"authorization_pending"}`
	*now = now.Add(5 * time.Second)
	c.PollLogin(context.Background(), session.ID)
	if c.Status().State != AwaitingAuthorization {
		t.Fatal("pending treated as failure")
	}
	*reply = `{"error":"slow_down","interval":10}`
	*now = now.Add(5 * time.Second)
	s, err := c.PollLogin(context.Background(), session.ID)
	if err != nil || s.NextPollAt.Sub(*now) != 10*time.Second {
		t.Fatal("slowdown interval ignored")
	}
	*now = now.Add(5 * time.Second)
	c.PollLogin(context.Background(), session.ID)
	if *polls != 2 {
		t.Fatal("slowdown bypassed")
	}
	*reply = `{"error":"access_denied"}`
	*now = now.Add(5 * time.Second)
	s, _ = c.PollLogin(context.Background(), session.ID)
	if s.State != Denied || s.UserCode != "" {
		t.Fatal("denial not terminal")
	}
	session, _ = c.StartLogin(context.Background())
	*now = now.Add(901 * time.Second)
	if c.Status().State != Expired || c.deviceCode != "" {
		t.Fatal("expired device code retained")
	}
	if _, err = c.PollLogin(context.Background(), "another-session"); err == nil {
		t.Fatal("wrong session accepted")
	}
}
func TestCancellationAndFailedStorageCannotAuthorize(t *testing.T) {
	c, now, store, _, _, _ := fixture(t)
	session, _ := c.StartLogin(context.Background())
	c.CancelLogin(session.ID)
	*now = now.Add(5 * time.Second)
	s, _ := c.PollLogin(context.Background(), session.ID)
	if s.State != Cancelled || len(store.values) != 0 {
		t.Fatal("cancelled login authorized")
	}
	session, _ = c.StartLogin(context.Background())
	store.fail = true
	*now = now.Add(5 * time.Second)
	s, err := c.PollLogin(context.Background(), session.ID)
	if err == nil || s.State != Failed || s.ErrorCode != "credential_save_failed" {
		t.Fatal("claimed success without secure storage")
	}
}
func TestBrowserFailureAndUnconfiguredApp(t *testing.T) {
	c, _, _, _, _, _ := fixture(t)
	c.opts.OpenBrowser = func(context.Context, string) error { return errors.New("private browser error") }
	s, err := c.StartLogin(context.Background())
	if err == nil || s.State != Failed || s.ErrorCode != "browser_open_failed" || s.UserCode != "" {
		t.Fatal("browser launch failure ignored")
	}
	empty := New(Options{})
	if _, err := empty.StartLogin(context.Background()); err == nil {
		t.Fatal("unregistered app pretended to log in")
	}
}
func TestRefreshRotationAndPATCompatibility(t *testing.T) {
	c, now, store, reply, _, _ := fixture(t)
	past := now.Add(-time.Minute)
	future := now.Add(time.Hour)
	record := secretRecord{AccessToken: "old", RefreshToken: "secret-refresh", ExpiresAt: &past, RefreshExpiresAt: &future}
	data, _ := json.Marshal(record)
	store.values["octocat"] = string(data)
	*reply = `{"access_token":"secret-access","token_type":"bearer","refresh_token":"rotated-refresh","expires_in":28800,"refresh_token_expires_in":15897600}`
	token, err := c.Token(context.Background(), "octocat")
	if err != nil || token != "secret-access" {
		t.Fatal(err)
	}
	var updated secretRecord
	json.Unmarshal([]byte(store.values["octocat"]), &updated)
	if updated.RefreshToken != "rotated-refresh" || updated.ExpiresAt == nil || !updated.ExpiresAt.After(*now) {
		t.Fatal("refresh not rotated atomically")
	}
	store.values["manual"] = "legacy-pat"
	if value, err := c.Token(context.Background(), "manual"); err != nil || value != "legacy-pat" {
		t.Fatal("manual credential migration broken")
	}
}
func TestCancellationDuringIdentityReadCannotSaveLateToken(t *testing.T) {
	c, now, store, _, _, _ := fixture(t)
	session, _ := c.StartLogin(context.Background())
	*now = now.Add(5 * time.Second)
	original := c.http.Transport
	entered, release := make(chan struct{}), make(chan struct{})
	c.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/user" {
			close(entered)
			<-release
		}
		return original.RoundTrip(r)
	})
	done := make(chan struct{})
	go func() { c.PollLogin(context.Background(), session.ID); close(done) }()
	<-entered
	c.CancelLogin(session.ID)
	close(release)
	<-done
	if c.Status().State != Cancelled || len(store.values) != 0 {
		t.Fatal("late authorization survived cancellation")
	}
}
