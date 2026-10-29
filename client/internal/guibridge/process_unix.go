//go:build darwin || linux

package guibridge

import (
	"os"
	"syscall"
)

func agentBinaryName() string { return "monitor-agent" }

func (s *Supervisor) terminateAgent(process *os.Process) error {
	return process.Signal(syscall.SIGTERM)
}

func processAlive(process *os.Process) bool {
	return process.Signal(syscall.Signal(0)) == nil
}
