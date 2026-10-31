//go:build linux

package guibridge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureDesktopIntegrationWritesEntryAndIcon(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")
	image := filepath.Join(home, "ESP 100%.AppImage")
	if err := os.WriteFile(image, []byte("test"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APPIMAGE", image)
	if err := EnsureDesktopIntegration(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(home, ".local", "share", "applications", "esp32-monitor.desktop"))
	if err != nil {
		t.Fatal(err)
	}
	entry := string(data)
	if !strings.Contains(entry, `Exec="`+strings.ReplaceAll(image, "%", "%%")+`"`) {
		t.Fatalf("unexpected desktop entry: %s", entry)
	}
	if !strings.Contains(entry, "StartupWMClass=esp32-monitor") {
		t.Fatalf("missing StartupWMClass: %s", entry)
	}
	icon, err := os.ReadFile(filepath.Join(home, ".local", "share", "icons", "hicolor", "256x256", "apps", "esp32-monitor.png"))
	if err != nil {
		t.Fatal(err)
	}
	if string(icon) != string(AppIcon()) {
		t.Fatal("icon does not match embedded app icon")
	}
}

func TestEnsureDesktopIntegrationSkipsWithoutAppImage(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("APPIMAGE", "")
	if err := EnsureDesktopIntegration(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".local", "share", "applications", "esp32-monitor.desktop")); !os.IsNotExist(err) {
		t.Fatalf("desktop entry written without APPIMAGE: %v", err)
	}
}

func TestEnsureDesktopIntegrationHonoursOptOut(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")
	image := filepath.Join(home, "ESP32-Monitor.AppImage")
	if err := os.WriteFile(image, []byte("test"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APPIMAGE", image)
	t.Setenv("ESP32_MONITOR_SKIP_DESKTOP_INTEGRATION", "1")
	if err := EnsureDesktopIntegration(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".local", "share", "applications", "esp32-monitor.desktop")); !os.IsNotExist(err) {
		t.Fatalf("desktop entry written despite opt-out: %v", err)
	}
}
