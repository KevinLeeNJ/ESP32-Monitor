//go:build !darwin && !linux && !windows

package guibridge

import "os"

func agentBinaryName() string { return "monitor-agent" }

func (s *Supervisor) terminateAgent(process *os.Process) error {
	return process.Kill()
}

func processAlive(process *os.Process) bool {
	return process.Signal(os.Signal(nil)) == nil
}
