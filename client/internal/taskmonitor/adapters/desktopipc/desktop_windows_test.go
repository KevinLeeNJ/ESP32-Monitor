//go:build windows

package desktopipc

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"esp32-monitor/client/internal/providers"
	"esp32-monitor/client/internal/taskmonitor"

	"github.com/Microsoft/go-winio"
)

func TestDesktopEndpointWindowsUsesCodexPipe(t *testing.T) {
	path, code := desktopEndpoint(`C:\Users\example\.codex`)
	if code != "" || path != `\\.\pipe\codex-ipc` {
		t.Fatalf("endpoint = %q, %q", path, code)
	}
}

func TestDesktopWindowsPipeAutoSelectsLatest(t *testing.T) {
	home := t.TempDir()
	index := `{"id":"old","updated_at":"2026-10-28T10:00:00Z"}` + "\n" + `{"id":"latest","updated_at":"2026-10-28T11:00:00Z"}` + "\n"
	if err := os.WriteFile(filepath.Join(home, "session_index.jsonl"), []byte(index), 0o600); err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf(`\\.\pipe\esp32-monitor-test-%d-%d`, os.Getpid(), time.Now().UnixNano())
	listener, err := winio.ListenPipe(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	d := New(home, nil, time.Now)
	d.path = path
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	running := make(chan struct{})
	go func() { defer close(running); d.Run(ctx) }()
	serverErr := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			serverErr <- err
			return
		}
		defer conn.Close()
		raw, err := readDesktopFrame(conn)
		if err != nil {
			serverErr <- err
			return
		}
		var init struct {
			Type      string `json:"type"`
			Method    string `json:"method"`
			RequestID string `json:"requestId"`
		}
		if json.Unmarshal(raw, &init) != nil || init.Type != "request" || init.Method != "initialize" {
			serverErr <- fmt.Errorf("unexpected registration: %s", raw)
			return
		}
		if err := writeDesktopFrame(conn, map[string]any{"type": "response", "requestId": init.RequestID, "resultType": "success"}); err != nil {
			serverErr <- err
			return
		}
		raw, err = readDesktopFrame(conn)
		if err != nil {
			serverErr <- err
			return
		}
		var follow struct {
			Type   string `json:"type"`
			Method string `json:"method"`
			Params struct {
				ConversationID string `json:"conversationId"`
			} `json:"params"`
		}
		if json.Unmarshal(raw, &follow) != nil || follow.Type != "broadcast" || follow.Method != "thread-stream-following-changed" || follow.Params.ConversationID != "latest" {
			serverErr <- fmt.Errorf("task ownership operation: %s", raw)
			return
		}
		var params any
		json.Unmarshal(desktopMessage("latest", "snapshot", "", 1, rawState("latest", "active", "inProgress", "t"), nil, 0), &params)
		serverErr <- writeDesktopFrame(conn, map[string]any{"type": "broadcast", "method": "thread-stream-state-changed", "version": 11, "sourceClientId": "owner", "params": params})
		<-ctx.Done()
	}()
	select {
	case err := <-serverErr:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("fake desktop did not register")
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && d.Snapshot().State != taskmonitor.Running {
		time.Sleep(20 * time.Millisecond)
	}
	if s := d.Snapshot(); s.State != taskmonitor.Running || s.Health.State != providers.Healthy {
		t.Fatal(s)
	}
	cancel()
	listener.Close()
	<-running
	if d.Snapshot().State != taskmonitor.Unknown {
		t.Fatal("cancelled connection retained running state")
	}
}
