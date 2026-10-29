package collector

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"esp32-monitor/client/internal/platform/proxy"
)

// errPublicIPPending means a background refresh is in flight; callers omit the
// optional observation for this tick instead of blocking the whole batch.
var errPublicIPPending = errors.New("public_ip_pending")

// publicIPv4 is only called while previewing or after the user selects the
// public IP metric; a capability scan never triggers an external request.
func publicIPv4(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	dialer := &net.Dialer{Timeout: 3 * time.Second}
	resolver := proxy.Default()
	transport := &http.Transport{
		// Respect the user's system proxy (the same one the GUI providers use);
		// a detection failure falls back to a direct connection.
		Proxy: func(req *http.Request) (*url.URL, error) {
			u, err := resolver.Proxy(req)
			if err != nil {
				return nil, nil
			}
			return u, nil
		},
		DialContext: func(ctx context.Context, _, address string) (net.Conn, error) {
			return dialer.DialContext(ctx, "tcp4", address)
		},
		ForceAttemptHTTP2: true,
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for _, endpoint := range []string{"https://api.mac-stats.com/ip", "https://api4.ipify.org?format=json"} {
		ip, err := fetchPublicIPv4(ctx, client, endpoint)
		if err == nil {
			return ip, nil
		}
	}
	return "", io.ErrUnexpectedEOF
}

func fetchPublicIPv4(ctx context.Context, client *http.Client, endpoint string) (string, error) {
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if e != nil {
		return "", e
	}
	resp, e := client.Do(req)
	if e != nil {
		return "", e
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", io.ErrUnexpectedEOF
	}
	b, e := io.ReadAll(io.LimitReader(resp.Body, 4097))
	if e != nil {
		return "", e
	}
	if len(b) > 4096 {
		return "", io.ErrUnexpectedEOF
	}
	return parsePublicIPv4(b)
}

func parsePublicIPv4(b []byte) (string, error) {
	var result struct {
		IPv4 string `json:"ipv4"`
		IP   string `json:"ip"`
	}
	if err := json.Unmarshal(b, &result); err != nil {
		return "", err
	}
	address := result.IPv4
	if address == "" {
		address = result.IP
	}
	ip := net.ParseIP(strings.TrimSpace(address))
	if ip == nil || ip.To4() == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() {
		return "", io.ErrUnexpectedEOF
	}
	return ip.To4().String(), nil
}
