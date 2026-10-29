//go:build darwin || linux

package desktopipc

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"esp32-monitor/client/internal/taskmonitor"
)

// Uses the actual framed socket transport; the fake desktop rejects any operation
// outside passive registration/following/discovery responses.
func TestDesktopSocketRegistrationReadOnlyAndReconnect(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("Unix desktop IPC only")
	}
	// Short path fits macOS sockaddr_un (104 bytes).
	home, err := os.MkdirTemp("/tmp", "esp32-ipc-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(home)
	ipc := filepath.Join(home, "ipc")
	if os.Mkdir(ipc, 0700) != nil {
		t.Fatal("mkdir")
	}
	listener, err := net.Listen("unix", filepath.Join(ipc, "ipc.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	os.Chmod(filepath.Join(ipc, "ipc.sock"), 0600)
	d := New(home, []string{"a"}, time.Now)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	running := make(chan struct{})
	go func() { defer close(running); d.Run(ctx) }()
	var wg sync.WaitGroup
	handled := make(chan struct{}, 2)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range 2 {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			bytes, err := readDesktopFrame(conn)
			if err != nil {
				t.Error(err)
				conn.Close()
				return
			}
			var init struct {
				Type   string `json:"type"`
				Method string `json:"method"`
				ID     string `json:"requestId"`
			}
			json.Unmarshal(bytes, &init)
			if init.Type != "request" || init.Method != "initialize" {
				t.Error("unexpected mutating operation")
			}
			writeDesktopFrame(conn, map[string]any{"type": "response", "requestId": init.ID, "resultType": "success"})
			bytes, err = readDesktopFrame(conn)
			if err != nil {
				t.Error(err)
				conn.Close()
				return
			}
			var follow struct {
				Type   string `json:"type"`
				Method string `json:"method"`
			}
			json.Unmarshal(bytes, &follow)
			if follow.Type != "broadcast" || follow.Method != "thread-stream-following-changed" {
				t.Error("task ownership operation")
			}
			var params any
			json.Unmarshal(desktopMessage("a", "snapshot", "", 1, rawState("a", "active", "inProgress", "t"), nil, 0), &params)
			writeDesktopFrame(conn, map[string]any{"type": "broadcast", "method": "thread-stream-state-changed", "version": 11, "sourceClientId": "owner", "params": params})
			handled <- struct{}{}
			if len(handled) == 1 {
				time.Sleep(50 * time.Millisecond)
			}
			conn.Close()
		}
	}()
	for range 2 {
		select {
		case <-handled:
		case <-time.After(5 * time.Second):
			t.Fatal("did not reconnect")
		}
	}
	cancel()
	listener.Close()
	<-running
	wg.Wait()
	if d.Snapshot().State != taskmonitor.Unknown {
		t.Fatal("cancelled connection retained running state")
	}
}
func TestDesktopSelectionAndSocketPermissions(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("Unix desktop IPC only")
	}
	autoHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(autoHome, "session_index.jsonl"), []byte(`{"id":"auto-thread","updated_at":"2026-10-28T17:00:00Z"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d := New(autoHome, nil, time.Now)
	go d.Run(ctx)
	d.WaitReady(ctx)
	if s := d.Snapshot(); s.Scope != "desktop_latest_thread" || len(s.Items) != 1 || s.Items[0].ID != "auto-thread" {
		t.Fatal("latest thread not auto-selected", s)
	}
	d = New(t.TempDir(), []string{"../bad"}, time.Now)
	d.Run(ctx)
	if d.Snapshot().Health.ErrorCode != "invalid_codex_thread_id" {
		t.Fatal("invalid id accepted")
	}
	home, err := os.MkdirTemp("/tmp", "esp32-ipc-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(home)
	ipc := filepath.Join(home, "ipc")
	os.Mkdir(ipc, 0700)
	file := filepath.Join(ipc, "ipc.sock")
	listener, err := net.Listen("unix", file)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	os.Chmod(file, 0600)
	if err := validateDesktopSocket(file); err != nil {
		t.Fatal(err)
	}
	os.Chmod(ipc, 0777)
	if err := validateDesktopSocket(file); err == nil {
		t.Fatal("shared-writable directory accepted")
	}
}
