package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func resolver(s Settings, env ...string) Resolver {
	return Resolver{System: func(context.Context) (Settings, error) { return s, nil }, Environ: func() []string { return env }}
}
func TestOSProxyParsing(t *testing.T) {
	mac, err := parseMac(`<dictionary> {
  ExceptionsList : <array> {
    0 : *.local
    1 : 127.0.0.1
  }
  HTTPEnable : 1
  HTTPPort : 7890
  HTTPProxy : 127.0.0.1
  HTTPSEnable : 1
  HTTPSPort : 7891
  HTTPSProxy : ::1
  SOCKSEnable : 1
  SOCKSPort : 7892
  SOCKSProxy : localhost
  __SCOPED__ : <dictionary> {
    HTTPProxy : wrong
  }
}`)
	if err != nil || mac.HTTP != "http://127.0.0.1:7890" || mac.HTTPS != "http://[::1]:7891" || mac.SOCKS != "socks5h://localhost:7892" || len(mac.Bypass) != 2 {
		t.Fatalf("mac: %+v %v", mac, err)
	}
	windows, err := parseWindows([]byte(`{"enabled":true,"server":"http=127.0.0.1:7890;https=127.0.0.1:7891;socks=127.0.0.1:7892","bypass":"*.local;<local>"}`))
	if err != nil || windows.HTTPS != "127.0.0.1:7891" || windows.SOCKS != "socks5h://127.0.0.1:7892" {
		t.Fatalf("windows: %+v %v", windows, err)
	}
	single, _ := parseWindows([]byte(`{"enabled":true,"server":"127.0.0.1:7890"}`))
	if single.HTTP != single.HTTPS || single.HTTP == "" {
		t.Fatal("single Windows proxy lost")
	}
	gnome, err := parseGnome(`org.gnome.system.proxy ignore-hosts ['localhost', '*.local']
org.gnome.system.proxy.http host '127.0.0.1'
org.gnome.system.proxy.http port 7890
org.gnome.system.proxy.https host '127.0.0.1'
org.gnome.system.proxy.https port 7891`)
	if err != nil || gnome.HTTP != "http://127.0.0.1:7890" || gnome.HTTPS != "http://127.0.0.1:7891" || len(gnome.Bypass) != 2 {
		t.Fatalf("gnome: %+v %v", gnome, err)
	}
	if _, err := parseMac("HTTPEnable : 1\nHTTPProxy : 127.0.0.1\nHTTPPort : 0"); err == nil {
		t.Fatal("invalid enabled proxy accepted")
	}
}
func TestSystemPrecedenceAndChildEnvironment(t *testing.T) {
	r := resolver(Settings{HTTP: "http://127.0.0.1:7890", HTTPS: "http://127.0.0.1:7891", Bypass: []string{"*.local"}}, "https_proxy=http://wrong:9", "HTTPS_PROXY=http://wrong2:8", "PATH=/bin", "NO_PROXY=*")
	req, _ := http.NewRequest("GET", "https://api.github.com/graphql", nil)
	u, err := r.Proxy(req)
	if err != nil || u.String() != "http://127.0.0.1:7891" {
		t.Fatal(u, err)
	}
	env, err := r.Environment(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(env, "\n")
	for _, want := range []string{"https_proxy=http://127.0.0.1:7891", "HTTPS_PROXY=http://127.0.0.1:7891", "NO_PROXY=*.local", "PATH=/bin"} {
		if !strings.Contains(joined, want) {
			t.Error(want)
		}
	}
	if strings.Contains(joined, "wrong") {
		t.Fatal("stale proxy inherited")
	}
}
func TestEnvironmentFallbackSocksAndBypass(t *testing.T) {
	r := resolver(Settings{}, "ALL_PROXY=socks5://127.0.0.1:1080", "NO_PROXY=.example.com,10.0.0.0/8")
	for _, tc := range []struct {
		host   string
		direct bool
	}{{"api.github.com", false}, {"api.example.com", true}, {"localhost", true}, {"127.0.0.1", true}, {"10.1.2.3", true}} {
		req, _ := http.NewRequest("GET", "https://"+tc.host, nil)
		u, err := r.Proxy(req)
		if err != nil || (u == nil) != tc.direct {
			t.Fatalf("%s: %v %v", tc.host, u, err)
		}
	}
	req, _ := http.NewRequest("GET", "https://api.github.com", nil)
	u, _ := r.Proxy(req)
	if u.Scheme != "socks5" {
		t.Fatal("SOCKS ignored")
	}
}

func TestBypassRespectsPort(t *testing.T) {
	r := resolver(Settings{HTTPS: "http://proxy:8080", Bypass: []string{"example.com:8080"}})
	for _, tc := range []struct {
		endpoint string
		direct   bool
	}{
		{"https://example.com:8080", true},
		{"https://example.com", false},
		{"https://example.com:443", false},
	} {
		req, _ := http.NewRequest("GET", tc.endpoint, nil)
		u, err := r.Proxy(req)
		if err != nil || (u == nil) != tc.direct {
			t.Fatalf("%s: %v %v", tc.endpoint, u, err)
		}
	}
}
func TestAutomaticAndDetectionErrorsNeverFallBackDirect(t *testing.T) {
	req, _ := http.NewRequest("GET", "https://api.github.com", nil)
	for _, r := range []Resolver{resolver(Settings{Automatic: true}), {System: func(context.Context) (Settings, error) { return Settings{}, ErrDetection }, Environ: func() []string { return nil }}} {
		if _, err := r.Proxy(req); err == nil {
			t.Fatal("unsupported proxy went direct")
		}
		if _, err := r.Environment(context.Background()); err == nil {
			t.Fatal("child would go direct")
		}
	}
	for _, s := range []string{"ftp://proxy:9", "http://proxy:9/path", "http://"} {
		if _, err := proxyURL(s); !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid proxy accepted", s)
		}
	}
}

// A real HTTP proxy round trip proves transport use; the target hostname cannot
// be resolved, so this test fails if the request ever bypasses the proxy.
func TestTransportRoutesThroughProxy(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	seen := make(chan string, 1)
	server := &http.Server{ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { seen <- r.URL.String(); fmt.Fprint(w, "proxied") })}
	go server.Serve(listener)
	defer server.Close()
	r := resolver(Settings{HTTP: "http://" + listener.Addr().String()})
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = r.Proxy
	defer transport.CloseIdleConnections()
	client := http.Client{Transport: transport, Timeout: time.Second}
	response, err := client.Get("http://must-use-proxy.invalid/calendar")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if string(body) != "proxied" || <-seen != "http://must-use-proxy.invalid/calendar" {
		t.Fatal("request did not traverse proxy")
	}
	// A configured but dead proxy must fail; no direct network retry is allowed.
	dead, _ := url.Parse("http://127.0.0.1:1")
	transport.Proxy = http.ProxyURL(dead)
	if _, err := client.Get("http://must-use-proxy.invalid/calendar"); err == nil {
		t.Fatal("dead proxy silently bypassed")
	}
}

func TestHTTPSTransportUsesConnectProxy(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	seen := make(chan string, 1)
	server := &http.Server{ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.Method + " " + r.Host
		w.WriteHeader(http.StatusBadGateway)
	})}
	go server.Serve(listener)
	defer server.Close()
	r := resolver(Settings{HTTPS: "http://" + listener.Addr().String()})
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = r.Proxy
	defer transport.CloseIdleConnections()
	client := http.Client{Transport: transport, Timeout: time.Second}
	if _, err := client.Get("https://must-use-proxy.invalid/calendar"); err == nil {
		t.Fatal("proxy failure ignored")
	}
	select {
	case got := <-seen:
		if got != "CONNECT must-use-proxy.invalid:443" {
			t.Fatal(got)
		}
	case <-time.After(time.Second):
		t.Fatal("HTTPS request did not use CONNECT proxy")
	}
}
