package ipc

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSerialPermissionErrorThroughIPC(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/monitors/serial/probe" && r.URL.Path != "/v1/monitors/serial/connect" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		writeAppError(w, errors.New("serial_permission_denied"))
	}))
	defer server.Close()
	address := server.Listener.Addr().String()
	client := &Client{http: &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", address)
	}}}}
	_, probeErr := client.ProbeSerial(context.Background(), "/dev/ttyACM0")
	connectErr := client.ConnectSerial(context.Background(), "/dev/ttyACM0", "board", true)
	for _, err := range []error{probeErr, connectErr} {
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.Code != "serial_permission_denied" || apiErr.Status != http.StatusUnprocessableEntity {
			t.Fatalf("got %v", err)
		}
	}
}
