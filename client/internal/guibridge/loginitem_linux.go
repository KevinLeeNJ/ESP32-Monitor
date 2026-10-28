//go:build linux

package guibridge

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// loginItemLaunchArgs starts the app in background mode without a visible window.
var loginItemLaunchArgs = []string{"--login-item"}

func linuxAutostartPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "autostart", "esp32-monitor-client.desktop"), nil
}

func setLaunchAtLogin(enabled bool) error {
	path, err := linuxAutostartPath()
	if err != nil {
		return err
	}
	if !enabled {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	escaped := strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "`", "\\`", "$", "\\$").Replace(executable)
	quoted := `"` + escaped + `"`
	entry := fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=ESP32 Monitor
Comment=ESP32 Monitor background client
Exec=%s %s
Terminal=false
X-GNOME-Autostart-enabled=true
`, quoted, loginItemLaunchArgs[0])
	return os.WriteFile(path, []byte(entry), 0o644)
}
