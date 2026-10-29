//go:build darwin || linux

package agent

import "syscall"

// restartSysProcAttr detaches the replacement Agent from this process's
// session so it survives the predecessor exiting.
func restartSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
