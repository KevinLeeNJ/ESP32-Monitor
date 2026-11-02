package transport

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// QWeather returns gzip even when the request asks for identity. The proxy
// must let Go negotiate and decompress instead of pinning Accept-Encoding.
func TestProxyFetchForcedGzip(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		zw := gzip.NewWriter(w)
		_ = json.NewEncoder(zw).Encode(map[string]string{"code": "200"})
		_ = zw.Close()
	}))
	defer server.Close()
	p := &proxyWorker{client: server.Client()}
	var out struct {
		Code string `json:"code"`
	}
	if err := p.fetch(context.Background(), server.URL, &out, 4096, nil); err != nil {
		t.Fatalf("forced gzip upstream rejected: %v", err)
	}
	if out.Code != "200" {
		t.Fatalf("code = %q", out.Code)
	}
}
