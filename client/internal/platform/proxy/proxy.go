// Package proxy shares system-proxy selection between Go HTTP and Codex subprocesses.
package proxy

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path"
	"strings"
)

var ErrAutomatic = errors.New("automatic_proxy_unsupported")
var ErrDetection = errors.New("system_proxy_detection_failed")
var ErrInvalid = errors.New("invalid_proxy_configuration")

type Settings struct {
	HTTP      string
	HTTPS     string
	SOCKS     string
	Bypass    []string
	Automatic bool
}

func (s Settings) enabled() bool {
	return s.HTTP != "" || s.HTTPS != "" || s.SOCKS != "" || s.Automatic
}

// Resolver rereads OS configuration on each provider request. System settings take
// precedence; environment variables are used when no system proxy is configured.
type Resolver struct {
	System  func(context.Context) (Settings, error)
	Environ func() []string
}

func Default() Resolver { return Resolver{System: readSystem, Environ: os.Environ} }
func (r Resolver) settings(ctx context.Context) (Settings, error) {
	s, err := r.System(ctx)
	if err != nil {
		return Settings{}, err
	}
	if s.Automatic {
		return Settings{}, ErrAutomatic
	}
	if s.enabled() {
		return s, nil
	}
	values := map[string]string{}
	for _, entry := range r.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			values[key] = value
		}
	}
	get := func(key string) string {
		if v := values[key]; v != "" {
			return v
		}
		return values[strings.ToLower(key)]
	}
	return Settings{HTTP: get("HTTP_PROXY"), HTTPS: get("HTTPS_PROXY"), SOCKS: get("ALL_PROXY"), Bypass: strings.Split(get("NO_PROXY"), ",")}, nil
}
func proxyURL(value string) (*url.URL, error) {
	if value == "" {
		return nil, nil
	}
	if !strings.Contains(value, "://") {
		value = "http://" + value
	}
	u, err := url.Parse(value)
	if err != nil || u.Hostname() == "" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return nil, ErrInvalid
	}
	switch u.Scheme {
	case "http", "https", "socks5", "socks5h":
		return u, nil
	default:
		return nil, ErrInvalid
	}
}
func bypass(host, port string, patterns []string) bool {
	host = strings.ToLower(host)
	if host == "localhost" {
		return true
	}
	if ip, err := netip.ParseAddr(host); err == nil && ip.IsLoopback() {
		return true
	}
	for _, pattern := range patterns {
		pattern = strings.ToLower(strings.TrimSpace(pattern))
		if pattern == "" {
			continue
		}
		if pattern == "*" {
			return true
		}
		if pattern == "<local>" && !strings.Contains(host, ".") && !strings.Contains(host, ":") {
			return true
		}
		if prefix, err := netip.ParsePrefix(pattern); err == nil {
			if ip, err := netip.ParseAddr(host); err == nil && prefix.Contains(ip) {
				return true
			}
			continue
		}
		if h, p, err := net.SplitHostPort(pattern); err == nil {
			if p != port {
				continue
			}
			pattern = h
		}
		base := strings.TrimPrefix(strings.TrimPrefix(pattern, "*."), ".")
		if host == base || strings.HasSuffix(host, "."+base) {
			return true
		}
		if ok, _ := path.Match(pattern, host); ok {
			return true
		}
	}
	return false
}
func (r Resolver) Proxy(req *http.Request) (*url.URL, error) {
	s, err := r.settings(req.Context())
	if err != nil {
		return nil, err
	}
	port := req.URL.Port()
	if port == "" {
		if req.URL.Scheme == "https" {
			port = "443"
		} else if req.URL.Scheme == "http" {
			port = "80"
		}
	}
	if bypass(req.URL.Hostname(), port, s.Bypass) {
		return nil, nil
	}
	value := s.HTTP
	if req.URL.Scheme == "https" {
		value = s.HTTPS
	}
	if value == "" {
		value = s.SOCKS
	}
	return proxyURL(value)
}

// Environment maps the same selected settings to reqwest-compatible variables.
// Both cases are replaced, so an inherited lowercase variable cannot override it.
// PAC/WPAD is rejected before launching Codex instead of silently going direct.
func (r Resolver) Environment(ctx context.Context) ([]string, error) {
	s, err := r.settings(ctx)
	if err != nil {
		return nil, err
	}
	result := []string{}
	for _, entry := range r.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		switch strings.ToLower(key) {
		case "http_proxy", "https_proxy", "all_proxy", "no_proxy":
			continue
		}
		result = append(result, entry)
	}
	values := map[string]string{"HTTP_PROXY": s.HTTP, "HTTPS_PROXY": s.HTTPS, "ALL_PROXY": s.SOCKS, "NO_PROXY": strings.Join(s.Bypass, ",")}
	for key, value := range values {
		if key != "NO_PROXY" {
			u, err := proxyURL(value)
			if err != nil {
				return nil, err
			}
			if u != nil {
				value = u.String()
			}
		}
		result = append(result, key+"="+value, strings.ToLower(key)+"="+value)
	}
	return result, nil
}
