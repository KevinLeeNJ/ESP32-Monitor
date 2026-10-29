package adapters

import (
	"context"
	"esp32-monitor/client/internal/providers"
	"esp32-monitor/client/internal/taskmonitor"
	"runtime"
	"testing"
	"time"
)

func TestPlatformSelectionAndUnknownSource(t *testing.T) {
	now := time.Unix(123, 0)
	opts := Options{Home: t.TempDir(), ThreadIDs: []string{"a"}, Now: func() time.Time { return now }}
	for _, name := range []string{"", "auto", "desktop-ipc"} {
		adapter := New(name, opts)
		if runtime.GOOS == "darwin" || runtime.GOOS == "linux" || runtime.GOOS == "windows" {
			if adapter.ID() != "desktop-ipc" || adapter.Snapshot().Scope != "desktop_selected_threads" {
				t.Fatal("wrong Unix source", adapter.ID())
			}
		} else if adapter.Snapshot().Health.State != providers.Unsupported || adapter.Snapshot().ActiveCount != nil {
			t.Fatal("unimplemented platform claimed coverage")
		}
	}
	adapter := New("typo", opts)
	adapter.Run(context.Background())
	adapter.WaitReady(context.Background())
	s := adapter.Snapshot()
	if adapter.ID() != "typo" || s.State != taskmonitor.Unknown || s.ActiveCount != nil || s.Health.State != providers.Unsupported || s.Health.ErrorCode != "task_adapter_unknown" || !s.Health.ObservedAt.Equal(now) {
		t.Fatal(s)
	}
}
