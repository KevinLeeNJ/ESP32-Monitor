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

// setLaunchAtLogin registers the GUI background start (HKCU Run) and asks the
// running elevated Agent to create the highest-privilege logon task. The Agent
// already holds an elevated token, so this setting shows no UAC prompt.
func setLaunchAtLogin(enabled bool, configureAgentTask func(bool) error) error {
	if enabled {
		if configureAgentTask == nil {
			return errors.New("agent_unavailable")
		}
		if err := configureAgentTask(true); err != nil {
			return err
		}
	}
	key, _, err := registry.CreateKey(registry.CURRENT_USER, windowsRunKey, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		if enabled {
			_ = configureAgentTask(false)
		}
		return err
	}
	defer key.Close()
	if !enabled {
		if configureAgentTask != nil {
			if err := configureAgentTask(false); err != nil {
				return err
			}
		}
		if err := key.DeleteValue(windowsRunValue); err != nil && !errors.Is(err, registry.ErrNotExist) {
			return err
		}
		return nil
	}
	executable, err := os.Executable()
	if err != nil {
		_ = configureAgentTask(false)
		return err
	}
	command := fmt.Sprintf("\"%s\" %s", executable, loginItemLaunchArgs[0])
	if err := key.SetStringValue(windowsRunValue, command); err != nil {
		_ = configureAgentTask(false)
		return err
	}
	return nil
}
