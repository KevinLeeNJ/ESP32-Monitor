//go:build windows

package guibridge

import (
	"strings"
	"testing"
)

func TestDefaultSocketPathWindowsUsesPerUserPipe(t *testing.T) {
	path, err := DefaultSocketPath()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(path, `\\.\pipe\ESP32-Monitor-S-1-5-`) {
		t.Fatalf("default endpoint = %q", path)
	}
	if strings.Contains(path[len(`\\.\pipe\`):], `\`) {
		t.Fatalf("pipe name must be a single component: %q", path)
	}
}
