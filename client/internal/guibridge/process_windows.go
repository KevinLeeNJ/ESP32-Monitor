//go:build windows

package guibridge

import (
	"context"
	"os"
	"time"

	"esp32-monitor/client/internal/ipc"

	"golang.org/x/sys/windows"
)

func agentBinaryName() string { return "monitor-agent.exe" }

// terminateAgent asks the Agent to exit over IPC because Windows has no
// SIGTERM. The caller escalates to Kill after the grace period.
func (s *Supervisor) terminateAgent(process *os.Process) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client, err := ipc.Dial(s.socket)
	if err != nil {
		return process.Kill()
	}
	defer client.Close()
	return client.Quit(ctx)
}

func processAlive(process *os.Process) bool {
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(process.Pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(handle)
	status, err := windows.WaitForSingleObject(handle, 0)
	return err == nil && status == uint32(windows.WAIT_TIMEOUT)
}
