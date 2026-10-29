//go:build darwin

package guibridge

import (
	"errors"
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"
)

const macLoginItemLabel = "com.esp32monitor.client.login"

// loginItemLaunchArgs starts the app in background mode without a visible window.
var loginItemLaunchArgs = []string{"--login-item"}

func macLoginItemPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", macLoginItemLabel+".plist"), nil
}

// setLaunchAtLogin writes a per-user LaunchAgent. The app path is resolved
// from the running bundle, so moving or rebuilding requires toggling it again.
func setLaunchAtLogin(enabled bool, _ func(bool) error) error {
	path, err := macLoginItemPath()
	if err != nil {
		return err
	}
	if !enabled {
		// Unload a job that this session may already have loaded; absence is fine.
		_ = exec.Command("launchctl", "bootout", fmt.Sprintf("gui/%d/%s", os.Getuid(), macLoginItemLabel)).Run()
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}

	executable, err := os.Executable()
	if err != nil {
		return err
	}
	bundle := filepath.Dir(filepath.Dir(filepath.Dir(executable)))
	if filepath.Ext(bundle) != ".app" {
		return errors.New("launch_at_login_requires_app_bundle")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
		<string>%s</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>LimitLoadToSessionType</key>
	<string>Aqua</string>
</dict>
</plist>
`, macLoginItemLabel, html.EscapeString(executable), loginItemLaunchArgs[0])
	return os.WriteFile(path, []byte(plist), 0o644)
}
