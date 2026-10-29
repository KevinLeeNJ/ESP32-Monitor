//go:build !darwin && !linux && !windows

package agent

import "syscall"

func restartSysProcAttr() *syscall.SysProcAttr { return nil }
