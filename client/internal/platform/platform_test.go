package platform

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseHTTPURL(t *testing.T) {
	valid := []string{"http://example.com", "https://example.com:8443/base", "http://127.0.0.1:8080/", "http://192.168.110.120:18081", "http://nas.local:8080"}
	for _, raw := range valid {
		u, err := ParseHTTPURL(raw)
		if err != nil || u.Hostname() == "" {
			t.Fatalf("valid URL rejected: %q (%v)", raw, err)
		}
	}
	invalid := []string{
		"", "example.com", "ftp://example.com", "https://user:pass@example.com",
		"https://example.com?q=1", "https://example.com#fragment", "https:///missing-host",
	}
	for _, raw := range invalid {
		if _, err := ParseHTTPURL(raw); !errors.Is(err, errInvalidURL) {
			t.Fatalf("invalid URL accepted: %q (%v)", raw, err)
		}
	}
}

func TestValidGitHubLogin(t *testing.T) {
	for _, login := range []string{"a", "Octocat", "a-b", "a1-b2-c3"} {
		if !ValidGitHubLogin(login) {
			t.Fatalf("valid login rejected: %q", login)
		}
	}
	for _, login := range []string{"", "-a", "a-", "a--b", "a_b", strings.Repeat("a", 40), "a\nb"} {
		if ValidGitHubLogin(login) {
			t.Fatalf("invalid login accepted: %q", login)
		}
	}
}

func TestNewHTTPClientKeepsRedirectsVisible(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("target"))
	}))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer redirect.Close()

	client := NewHTTPClient(2 * time.Second)
	response, err := client.Get(redirect.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusFound {
		t.Fatalf("redirect was followed: status %d", response.StatusCode)
	}
}

func TestNewHTTPClientHonorsTimeout(t *testing.T) {
	blocked := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write([]byte("late"))
	}))
	defer blocked.Close()

	client := NewHTTPClient(20 * time.Millisecond)
	if _, err := client.Get(blocked.URL); err == nil {
		t.Fatal("timeout was not enforced")
	}
}

func TestAppDataDirIsApplicationPrivate(t *testing.T) {
	dir, err := AppDataDir()
	if err != nil {
		t.Fatal(err)
	}
	if dir == "" || filepath.Base(dir) != "ESP32-Monitor" {
		t.Fatalf("unexpected app data dir: %q", dir)
	}
}

func TestOpenBrowserRejectsUnsafeURLs(t *testing.T) {
	// Only invalid URLs are exercised: a valid URL would launch a real browser.
	for _, raw := range []string{
		"", "not a url", "http://example.com", "file:///etc/passwd",
		"https://user:pass@example.com", "https:///missing-host",
	} {
		if err := OpenBrowser(context.Background(), raw); err == nil {
			t.Fatalf("unsafe URL accepted: %q", raw)
		}
	}
}

func TestGitHubCredentialsRejectEmptySecret(t *testing.T) {
	store := GitHubCredentials{}
	for _, secret := range []string{"", strings.Repeat("a", 16385)} {
		if err := store.Save(context.Background(), "user", secret); !errors.Is(err, ErrCredentialUnavailable) {
			t.Fatalf("unsafe secret accepted (len %d): %v", len(secret), err)
		}
	}
}
