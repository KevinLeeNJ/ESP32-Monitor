//go:build windows

package guibridge

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"esp32-monitor/client/internal/ipc"

	"golang.org/x/sys/windows"
)

func TestSupervisorLifecycleEndToEnd(t *testing.T) {
	bin := os.Getenv("MONITOR_AGENT_E2E")
	if bin == "" {
		t.Skip("MONITOR_AGENT_E2E not set")
	}
	t.Setenv("MONITOR_AGENT_BIN", bin)
	pipe := fmt.Sprintf(`\\.\pipe\esp32-monitor-sup-%d-%d`, os.Getpid(), time.Now().UnixNano())
	s, err := NewSupervisor(DefaultSettings(), pipe)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	var pid int
	if err := s.Call(func(c *ipc.Client) error {
		status, err := c.Status(ctx)
		pid = status.PID
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if pid == 0 {
		t.Fatal("no agent pid")
	}
	s.Stop()
	if _, err := ipc.Dial(pipe); err == nil {
		t.Fatal("pipe still reachable after Stop")
	}
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return // process already gone
	}
	defer windows.CloseHandle(handle)
	if status, err := windows.WaitForSingleObject(handle, 5000); err != nil || status != windows.WAIT_OBJECT_0 {
		t.Fatalf("agent pid %d still alive: %v %v", pid, status, err)
	}
}
