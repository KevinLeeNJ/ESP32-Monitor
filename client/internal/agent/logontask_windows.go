//go:build windows

package agent

import (
	"errors"
	"os"

	"esp32-monitor/client/internal/elevatedtask"
	"esp32-monitor/client/internal/ipc"

	"golang.org/x/sys/windows"
)

// configureLoginTask wires the highest-privilege logon task management route.
// The Agent already runs elevated, so the GUI never needs its own UAC prompt.
func configureLoginTask(opts *ipc.Options) {
	opts.ConfigureLoginTask = setLoginTask
}

func setLoginTask(enabled bool) error {
	if !agentElevated() {
		return errors.New("administrator_permission_required")
	}
	if !enabled {
		return elevatedtask.Delete()
	}
	executable, err := os.Executable()
	if err != nil {
		return errors.New("agent_executable_unavailable")
	}
	return elevatedtask.Create(executable, os.Args[1:])
}

// refreshLoginTask keeps an existing logon task aligned with the arguments
// this Agent was started with, including after a settings-driven restart.
func refreshLoginTask() {
	if !agentElevated() || !elevatedtask.Exists() {
		return
	}
	executable, err := os.Executable()
	if err != nil {
		return
	}
	_ = elevatedtask.Create(executable, os.Args[1:])
}

func agentElevated() bool {
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		return false
	}
	defer token.Close()
	return token.IsElevated()
}
