package ipc

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMonitorDiscoveryRejectsManualAddress(t *testing.T) {
	for _, tc := range []struct {
		body  string
		valid bool
	}{
		{`{"timeout_ms":10000}`, true},
		{`{"timeout_ms":10000,"address":"192.168.1.20"}`, false},
	} {
		req := httptest.NewRequest("POST", "/v1/monitors/discover", strings.NewReader(tc.body))
		var decoded monitorDiscoverRequest
		if err := decodeBody(req, &decoded); (err == nil) != tc.valid {
			t.Fatalf("body %s: error = %v", tc.body, err)
		}
	}
}
