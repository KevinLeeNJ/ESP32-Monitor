//go:build linux

package transport

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSerialPermissionDenied(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file permissions")
	}
	path := filepath.Join(t.TempDir(), "serial")
	if err := os.WriteFile(path, nil, 0000); err != nil {
		t.Fatal(err)
	}
	_, err := OpenSerialPort(path)
	if err == nil || err.Error() != "serial_permission_denied" {
		t.Fatalf("got %v", err)
	}
}

func TestSerialPermissionReachesProbeAndConnect(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file permissions")
	}
	path := filepath.Join(t.TempDir(), "serial")
	if err := os.WriteFile(path, nil, 0000); err != nil {
		t.Fatal(err)
	}
	backend := &Backend{serialPorts: map[string]bool{}}
	_, probeErr := backend.ProbeSerial(context.Background(), path)
	connectErr := backend.ConnectSerial(context.Background(), path, SerialSelection{})
	for _, err := range []error{probeErr, connectErr} {
		if err == nil || err.Error() != "serial_permission_denied" {
			t.Fatalf("got %v", err)
		}
	}
}
