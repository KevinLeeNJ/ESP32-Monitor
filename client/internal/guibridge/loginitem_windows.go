//go:build windows

package guibridge

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows/registry"
)

const windowsRunKey = `Software\Microsoft\Windows\CurrentVersion\Run`
const windowsRunValue = "ESP32Monitor"

// loginItemLaunchArgs starts the app in background mode without a visible window.
var loginItemLaunchArgs = []string{"--login-item"}

func setLaunchAtLogin(enabled bool) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, windowsRunKey, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	if !enabled {
		if err := key.DeleteValue(windowsRunValue); err != nil && !errors.Is(err, registry.ErrNotExist) {
			return err
		}
		return nil
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	command := fmt.Sprintf("\"%s\" %s", executable, loginItemLaunchArgs[0])
	return key.SetStringValue(windowsRunValue, command)
}
