//go:build windows

package windowshelper

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// Opt-in smoke test against the published Windows executable, without elevation
// or changing the running desktop Agent. Missing low-level readings are valid
// for a non-administrator; the wire protocol and single process are mandatory.
func TestBundledHelperLive(t *testing.T) {
	if os.Getenv("WINDOWS_HELPER_LIVE") != "1" {
		t.Skip("opt-in bundled helper test")
	}
	c := NewPlatform()
	command := c.command
	var starts atomic.Int32
	c.command = func(ctx context.Context) (*exec.Cmd, error) { starts.Add(1); return command(ctx) }
	defer c.Close()
	events, unsubscribe := c.SubscribeMusic()
	defer unsubscribe()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for i := 0; i < 2; i++ {
		line, err := c.ReadSensors(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var data struct {
			OK       bool
			Schema   int
			Warnings []string
			CPU      *float64 `json:"cpu_temperature_celsius"`
		}
		if err := json.Unmarshal(line, &data); err != nil || !data.OK || data.Schema != 1 {
			t.Fatalf("%s: %v", line, err)
		}
		t.Logf("read %d: cpu=%v warnings=%v", i+1, data.CPU, data.Warnings)
	}
	select {
	case line := <-events:
		t.Logf("music: %s", line)
	case <-ctx.Done():
		t.Fatal("music channel did not respond")
	}
	if starts.Load() != 1 {
		t.Fatalf("read requests spawned %d helpers", starts.Load())
	}
}

func TestHelperPathOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), helperName)
	if err := os.WriteFile(path, []byte("helper"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MONITOR_WINDOWS_HELPER", path)
	got, err := helperPath()
	if err != nil || got != path {
		t.Fatalf("%q %v", got, err)
	}
	t.Setenv("MONITOR_WINDOWS_HELPER", filepath.Join(t.TempDir(), "missing.exe"))
	if _, err := helperPath(); !errors.Is(err, ErrMissing) {
		t.Fatal(err)
	}
}
