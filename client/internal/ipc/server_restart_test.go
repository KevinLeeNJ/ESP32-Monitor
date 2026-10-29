package ipc

import (
	"context"
	"testing"
)

func TestRestartAndLoginTaskRoutes(t *testing.T) {
	application := newTestApplication(t)
	socket := testSocketPath(t)
	var restartArgs []string
	loginTask := false
	server, err := NewServer(application, Options{
		SocketPath: socket,
		Restart: func(args []string) error {
			restartArgs = args
			return nil
		},
		ConfigureLoginTask: func(enabled bool) error {
			loginTask = enabled
			return nil
		},
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	if err := server.Listen(); err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { _ = server.Close() })
	client := newTestClient(t, socket)
	ctx := context.Background()

	args := []string{"--ipc-socket", socket, "--music"}
	if err := client.Restart(ctx, args); err != nil {
		t.Fatalf("Restart: %v", err)
	}
	if len(restartArgs) != len(args) || restartArgs[1] != socket {
		t.Fatalf("restart args = %v", restartArgs)
	}
	if err := client.SetLoginTask(ctx, true); err != nil {
		t.Fatalf("SetLoginTask: %v", err)
	}
	if !loginTask {
		t.Fatal("login task callback not invoked")
	}
	if err := client.SetLoginTask(ctx, false); err != nil {
		t.Fatalf("SetLoginTask(false): %v", err)
	}
	if loginTask {
		t.Fatal("login task disable not propagated")
	}
}

func TestRestartRouteUnsupported(t *testing.T) {
	_, socket := newTestServer(t, nil)
	client := newTestClient(t, socket)
	err := client.Restart(context.Background(), nil)
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.Status != 501 || apiErr.Code != "restart_unsupported" {
		t.Fatalf("err = %v", err)
	}
}
