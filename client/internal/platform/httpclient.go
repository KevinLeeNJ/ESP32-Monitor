package platform

import (
	"net/http"
	"time"

	"esp32-monitor/client/internal/platform/proxy"
)

// NewHTTPClient returns a client that follows the system proxy and never
// follows redirects. Callers with custom transport needs may replace it.
func NewHTTPClient(timeout time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = proxy.Default().Proxy
	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}
