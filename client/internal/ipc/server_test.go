package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"esp32-monitor/client/internal/app"
	"esp32-monitor/client/internal/config"
)

func newTestApplication(t *testing.T) *app.Application {
	t.Helper()
	application, err := app.New(config.Config{}, "")
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	t.Cleanup(func() { _ = application.Close() })
	return application
}

func newTestServer(t *testing.T, shutdown func()) (*Server, string) {
	t.Helper()
	application := newTestApplication(t)
	socket := testSocketPath(t)
	server, err := NewServer(application, Options{SocketPath: socket, Shutdown: shutdown})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	if err := server.Listen(); err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { _ = server.Close() })
	return server, socket
}

func newTestClient(t *testing.T, socket string) *Client {
	t.Helper()
	client, err := Dial(socket)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestStatusRoundTrip(t *testing.T) {
	server, socket := newTestServer(t, nil)
	client := newTestClient(t, socket)
	status, err := client.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.SchemaVersion != SchemaVersion {
		t.Fatalf("schema_version = %d, want %d", status.SchemaVersion, SchemaVersion)
	}
	if status.PID != os.Getpid() {
		t.Fatalf("pid = %d, want %d", status.PID, os.Getpid())
	}
	if status.SocketPath != server.opts.SocketPath {
		t.Fatalf("socket_path = %q", status.SocketPath)
	}
	if status.CollectorEnabled || status.CompanionEnabled {
		t.Fatalf("disabled config must report disabled features: %+v", status)
	}
}

func TestRejectsBrowserOrigin(t *testing.T) {
	_, socket := newTestServer(t, nil)
	conn, err := dialIPC(context.Background(), socket)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	request, err := http.NewRequest(http.MethodGet, "http://ipc/v1/app/status", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	request.Header.Set("Origin", "http://evil.example")
	if err := request.Write(conn); err != nil {
		t.Fatalf("write: %v", err)
	}
	response, err := http.ReadResponse(bufio.NewReader(conn), request)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", response.StatusCode)
	}
	var reply errorReply
	if err := json.NewDecoder(response.Body).Decode(&reply); err != nil || reply.ErrorCode != "unauthorized" {
		t.Fatalf("error_code = %q (err %v)", reply.ErrorCode, err)
	}
}

func TestQuitCallsShutdownOnce(t *testing.T) {
	called := make(chan struct{}, 2)
	_, socket := newTestServer(t, func() { called <- struct{}{} })
	client := newTestClient(t, socket)
	if err := client.Quit(context.Background()); err != nil {
		t.Fatalf("Quit: %v", err)
	}
	select {
	case <-called:
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown not called")
	}
	select {
	case <-called:
		t.Fatal("shutdown called twice")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestServerTestRoute(t *testing.T) {
	_, socket := newTestServer(t, nil)
	client := newTestClient(t, socket)
	ctx := context.Background()

	result, err := client.ServerTest(ctx, "ftp://example.com")
	if err != nil {
		t.Fatalf("ServerTest: %v", err)
	}
	if result.OK || result.Error != "invalid_server_url" {
		t.Fatalf("unsupported scheme must be rejected: %+v", result)
	}

	health := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer health.Close()
	result, err = client.ServerTest(ctx, health.URL)
	if err != nil {
		t.Fatalf("ServerTest: %v", err)
	}
	if !result.OK || result.StatusCode != http.StatusOK {
		t.Fatalf("result = %+v", result)
	}
}

func TestCollectorDisabledConflict(t *testing.T) {
	_, socket := newTestServer(t, nil)
	client := newTestClient(t, socket)
	_, err := client.CollectorStatus(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusConflict || apiErr.Code != "collector_disabled" {
		t.Fatalf("err = %v, want collector_disabled 409", err)
	}
}

func TestNotFoundRoute(t *testing.T) {
	_, socket := newTestServer(t, nil)
	client := newTestClient(t, socket)
	err := client.do(context.Background(), http.MethodGet, "/v1/unknown", nil, nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusNotFound || apiErr.Code != "not_found" {
		t.Fatalf("err = %v, want not_found 404", err)
	}
}

func TestInvalidBodyRejected(t *testing.T) {
	_, socket := newTestServer(t, nil)
	client := newTestClient(t, socket)
	err := client.do(context.Background(), http.MethodPost, "/v1/github/logout", map[string]any{"nope": 1}, nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "invalid_request" {
		t.Fatalf("err = %v, want invalid_request", err)
	}
}

func TestListenTwiceFailsAndSecondServerRejected(t *testing.T) {
	server, socket := newTestServer(t, nil)
	if err := server.Listen(); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("second Listen = %v, want ErrAlreadyRunning", err)
	}
	application, err := app.New(config.Config{}, "")
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	defer func() { _ = application.Close() }()
	second, err := NewServer(application, Options{SocketPath: socket})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer func() { _ = second.Close() }()
	if err := second.Listen(); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("second server Listen = %v, want ErrAlreadyRunning", err)
	}
}

func TestDiagnosticsReportRedacted(t *testing.T) {
	_, socket := newTestServer(t, nil)
	client := newTestClient(t, socket)
	report, err := client.DiagnosticsReport(context.Background())
	if err != nil {
		t.Fatalf("DiagnosticsReport: %v", err)
	}
	if report.SchemaVersion != 1 || report.PID != os.Getpid() {
		t.Fatalf("report = %+v", report)
	}
	if report.Companion.GitHub.State == "" || report.Companion.Music.State == "" || report.Companion.Codex.State == "" {
		t.Fatalf("companion health states must be present: %+v", report.Companion)
	}
}
