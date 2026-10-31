//go:build linux

package guibridge

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// EnsureDesktopIntegration registers the running AppImage with the desktop so
// the window manager can resolve its icon. GTK4 removed gtk_window_set_icon, and
// Wayland compositors look up <app_id>.desktop instead; without the entry KDE
// falls back to its generic Wayland icon.
func EnsureDesktopIntegration() error {
	if os.Getenv("ESP32_MONITOR_SKIP_DESKTOP_INTEGRATION") == "1" {
		return nil
	}
	image := os.Getenv("APPIMAGE")
	if !filepath.IsAbs(image) {
		return nil
	}
	info, err := os.Stat(image)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return nil
	}
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		dataHome = filepath.Join(home, ".local", "share")
	}
	escaped := strings.NewReplacer("%", "%%", "\\", "\\\\", "\"", "\\\"", "`", "\\`", "$", "\\$").Replace(image)
	entry := fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=ESP32 Monitor
Comment=Desktop client for ESP32 Monitor
Exec="%s"
Icon=esp32-monitor
Terminal=false
Categories=System;Monitor;
StartupWMClass=esp32-monitor
`, escaped)
	if err := writeIfChanged(filepath.Join(dataHome, "applications", "esp32-monitor.desktop"), []byte(entry), 0o644); err != nil {
		return err
	}
	return writeIfChanged(filepath.Join(dataHome, "icons", "hicolor", "256x256", "apps", "esp32-monitor.png"), AppIcon(), 0o644)
}

// writeIfChanged keeps an existing entry that is already current, but rewrites
// it after the AppImage path changes.
func writeIfChanged(path string, data []byte, perm os.FileMode) error {
	if current, err := os.ReadFile(path); err == nil && bytes.Equal(current, data) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, perm)
}
