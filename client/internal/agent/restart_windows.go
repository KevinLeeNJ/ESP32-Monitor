//go:build windows

package agent

import (
	"syscall"

	"golang.org/x/sys/windows"
)

// restartSysProcAttr hides the replacement Agent's console window so a
// settings change does not flash a terminal on screen.
func restartSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
}
