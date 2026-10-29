//go:build windows

package ipc

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestAgentPipeEndToEnd(t *testing.T) {
	bin := os.Getenv("MONITOR_AGENT_E2E")
	if bin == "" {
		t.Skip("MONITOR_AGENT_E2E not set")
	}
	pipe := testSocketPath(t)
	var stderr bytes.Buffer
	cmd := exec.Command(bin, "--ipc-socket", pipe)
	cmd.Stderr = &stderr
	cmd.Stdout = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}()
	deadline := time.Now().Add(15 * time.Second)
	var client *Client
	var err error
	for time.Now().Before(deadline) {
		if client, err = Dial(pipe); err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if client == nil {
		t.Fatalf("agent pipe unreachable: %v\n%s", err, stderr.String())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	status, err := client.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if status.PID != cmd.Process.Pid || status.SocketPath != pipe {
		t.Fatalf("status = %+v (agent pid %d)", status, cmd.Process.Pid)
	}
	if err := client.Quit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("agent exit = %v\n%s", err, stderr.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("agent did not exit after quit")
	}
	if _, err := Dial(pipe); err == nil {
		t.Fatal("pipe still reachable after agent exit")
	}
}
