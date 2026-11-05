//go:build windows

package adapters

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"sync/atomic"
	"testing"
	"time"

	"esp32-monitor/client/internal/providers"
	"esp32-monitor/client/internal/windowshelper"
)

func TestSharedMusicHelperProcess(t *testing.T) {
	if os.Getenv("SHARED_MUSIC_TEST_HELPER") != "1" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var request struct{ Channel, ID, Method string }
		_ = json.Unmarshal(scanner.Bytes(), &request)
		if request.Method == "music.subscribe" {
			_ = encoder.Encode(map[string]any{"channel": "music", "id": "", "current": map[string]string{"title": "shared", "state": "playing"}})
		} else if request.Method == "sensors.read" {
			_ = encoder.Encode(map[string]any{"channel": "sensors", "id": request.ID, "data": map[string]any{"ok": true, "schema": 1}})
		}
	}
	os.Exit(0)
}

func TestSwitchableWindowsMusicSharesCollectorHelper(t *testing.T) {
	var starts atomic.Int32
	helper := windowshelper.New(func(ctx context.Context) (*exec.Cmd, error) {
		starts.Add(1)
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSharedMusicHelperProcess$")
		cmd.Env = append(os.Environ(), "SHARED_MUSIC_TEST_HELPER=1", "GORACE=atexit_sleep_ms=0")
		return cmd, nil
	})
	defer helper.Close()
	adapter, err := NewSwitchable(Options{Enabled: true, WindowsHelper: helper})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); adapter.Run(ctx) }()
	defer func() { cancel(); <-done }()
	ready, stop := context.WithTimeout(ctx, 3*time.Second)
	defer stop()
	if err := adapter.WaitReady(ready); err != nil {
		t.Fatal(err)
	}
	if snapshot := adapter.Snapshot(); snapshot.Health.State != providers.Healthy || snapshot.Current == nil {
		t.Fatal(snapshot)
	}
	if _, err := helper.ReadSensors(ready); err != nil {
		t.Fatal(err)
	}
	if err := adapter.SetEnabled(false); err != nil {
		t.Fatal(err)
	}
	if _, err := helper.ReadSensors(ready); err != nil {
		t.Fatal("music shutdown killed sensors:", err)
	}
	if err := adapter.SetEnabled(true); err != nil {
		t.Fatal(err)
	}
	if err := adapter.WaitReady(ready); err != nil {
		t.Fatal(err)
	}
	if starts.Load() != 1 {
		t.Fatalf("started %d helpers", starts.Load())
	}
}
