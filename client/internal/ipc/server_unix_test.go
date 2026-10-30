//go:build darwin || linux

package ipc

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func testSocketPath(t *testing.T) string {
	t.Helper()
	// t.TempDir() embeds the test name and would exceed the darwin 104-byte
	// unix socket path limit for long test names.
	dir, err := os.MkdirTemp("", "ipc-")
	if err != nil {
		t.Fatalf("tempdir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, "agent.sock")
}

func TestStaleSocketFileRebinds(t *testing.T) {
	socket := testSocketPath(t)
	file, err := os.Create(socket)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	_ = file.Close()
	server, err := NewServer(newTestApplication(t), Options{SocketPath: socket})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer func() { _ = server.Close() }()
	if err := server.Listen(); err != nil {
		t.Fatalf("rebind after stale socket: %v", err)
	}
	client, err := Dial(socket)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	if _, err := client.Status(context.Background()); err != nil {
		t.Fatalf("Status after rebind: %v", err)
	}
}

func TestUnixPathValidation(t *testing.T) {
	if err := validateIPCPath("relative.sock"); err == nil {
		t.Fatal("relative socket path accepted")
	}
	long := "/tmp/"
	for len(long) < 101 {
		long += "x"
	}
	if err := validateIPCPath(long); err == nil {
		t.Fatal("overlong socket path accepted")
	}
	if err := validateIPCPath("/tmp/agent.sock"); err != nil {
		t.Fatalf("valid socket path rejected: %v", err)
	}
}
