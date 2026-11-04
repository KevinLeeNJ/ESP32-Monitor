//go:build linux

package proxy

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadKDEProxy(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CURRENT_DESKTOP", "KDE")
	t.Setenv("XDG_CONFIG_HOME", dir)
	if err := os.WriteFile(filepath.Join(dir, "kioslaverc"), []byte("[Proxy Settings]\nProxyType=1\nhttpsProxy=http://127.0.0.1:7890\n"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := readSystem(context.Background())
	if err != nil || got.HTTPS != "http://127.0.0.1:7890" {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestKDEProxyAutomaticallyFollowsChanges(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CURRENT_DESKTOP", "KDE")
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HTTP_PROXY", "")
	t.Setenv("http_proxy", "")
	t.Setenv("HTTPS_PROXY", "")
	t.Setenv("https_proxy", "")
	t.Setenv("ALL_PROXY", "")
	t.Setenv("all_proxy", "")
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "proxied") }))
	defer proxy.Close()
	config := filepath.Join(dir, "kioslaverc")
	if err := os.WriteFile(config, []byte("[Proxy Settings]\nProxyType=1\nhttpProxy="+proxy.URL+"\nhttpsProxy="+proxy.URL+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	resolver := Default()
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = resolver.Proxy
	defer transport.CloseIdleConnections()
	response, err := (&http.Client{Transport: transport}).Get("http://must-use-proxy.invalid/")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	env, err := resolver.Environment(context.Background())
	if err != nil || !strings.Contains(strings.Join(env, "\n"), "HTTPS_PROXY="+proxy.URL) {
		t.Fatalf("child proxy not set: %v", err)
	}
	if err := os.WriteFile(config, []byte("[Proxy Settings]\nProxyType=0\n"), 0600); err != nil {
		t.Fatal(err)
	}
	request, _ := http.NewRequest("GET", "https://must-use-proxy.invalid/", nil)
	selected, err := resolver.Proxy(request)
	if err != nil || selected != nil {
		t.Fatalf("disabled proxy: %v %v", selected, err)
	}
}
