//go:build linux

package guibridge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLinuxAutostartUsesAppImage(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	image := filepath.Join(home, "ESP 100%.AppImage")
	if err := os.WriteFile(image, []byte("test"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APPIMAGE", image)
	if err := setLaunchAtLogin(true, nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(home, ".config", "autostart", "esp32-monitor-client.desktop"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `Exec="`+strings.ReplaceAll(image, "%", "%%")+`" --login-item`) {
		t.Fatalf("unexpected launch entry: %s", data)
	}
}
