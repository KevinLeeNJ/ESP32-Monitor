//go:build darwin || linux

package agent

import "syscall"

func predecessorAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}
