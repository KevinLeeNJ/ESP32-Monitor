//go:build windows

package guibridge

import (
	"context"
	"fmt"
	"unsafe"

	"esp32-monitor/client/internal/elevatedtask"

	"golang.org/x/sys/windows"
)

var (
	shell32       = windows.NewLazySystemDLL("shell32.dll")
	shellExecuteW = shell32.NewProc("ShellExecuteW")
)

// startPlatformAgent keeps the GUI at medium integrity. A configured logon
// task is used first; otherwise Windows asks consent only for monitor-agent.
func (s *Supervisor) startPlatformAgent(ctx context.Context) (bool, error) {
	if s.CurrentSettings().Launch.LaunchAtLogin {
		if err := elevatedtask.Run(); err == nil {
			return true, nil
		}
	}
	return true, shellExecuteRunAs(s.agentPath, s.agentArgs())
}

func shellExecuteRunAs(path string, args []string) error {
	verb, _ := windows.UTF16PtrFromString("runas")
	file, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	parameters, err := windows.UTF16PtrFromString(elevatedtask.CommandLine(args))
	if err != nil {
		return err
	}
	result, _, callErr := shellExecuteW.Call(0, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(file)), uintptr(unsafe.Pointer(parameters)), 0, 0)
	if result <= 32 {
		return fmt.Errorf("agent_elevation_failed: %w", callErr)
	}
	return nil
}
