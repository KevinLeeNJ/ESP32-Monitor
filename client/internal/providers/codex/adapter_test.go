package codex

import (
	"context"
	"esp32-monitor/client/internal/providers"
	"esp32-monitor/client/internal/taskmonitor"
	"testing"
	"time"
)

// A native platform source needs no desktop IPC or quota RPC implementation.
type testTaskAdapter struct {
	id      string
	state   taskmonitor.TaskState
	ready   chan struct{}
	stopped chan struct{}
}

func (a *testTaskAdapter) ID() string              { return a.id }
func (a *testTaskAdapter) Run(ctx context.Context) { close(a.ready); <-ctx.Done(); close(a.stopped) }
func (a *testTaskAdapter) WaitReady(ctx context.Context) {
	select {
	case <-a.ready:
	case <-ctx.Done():
	}
}
func (a *testTaskAdapter) Snapshot() taskmonitor.Tasks {
	count := 1
	return taskmonitor.Tasks{Health: providers.Health{State: providers.Healthy}, Scope: "test_selected_threads", State: a.state, ActiveCount: &count, Items: []taskmonitor.Task{{ID: "a", State: a.state}}}
}
func TestTaskAdapterInjectionAndLifecycle(t *testing.T) {
	for _, tc := range []struct {
		id    string
		state taskmonitor.TaskState
	}{{"native-a", taskmonitor.Running}, {"native-b", taskmonitor.WaitingApproval}} {
		t.Run(tc.id, func(t *testing.T) {
			adapter := &testTaskAdapter{id: tc.id, state: tc.state, ready: make(chan struct{}), stopped: make(chan struct{})}
			// Injection wins even over a different explicit name; quota is disabled.
			p := New(Options{Tasks: adapter, TaskAdapter: "invalid"})
			p.connect = func(context.Context, string) (*Connection, error) {
				t.Error("task adapter invoked quota")
				return nil, nil
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan struct{})
			go func() { defer close(done); p.RunTasks(ctx) }()
			wait, stop := context.WithTimeout(ctx, time.Second)
			defer stop()
			p.WaitTasks(wait)
			if wait.Err() != nil {
				t.Fatal("adapter readiness not forwarded")
			}
			s := p.Refresh(ctx)
			if s.Tasks.Adapter != tc.id || s.Tasks.State != tc.state || s.Quota.Health.State != providers.Disabled {
				t.Fatal(s)
			}
			s.Tasks.Items[0].State = taskmonitor.Failed
			if p.Snapshot().Tasks.Items[0].State != tc.state {
				t.Fatal("snapshot not owned")
			}
			cancel()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("adapter shutdown blocked")
			}
			select {
			case <-adapter.stopped:
			default:
				t.Fatal("adapter lifecycle bypassed")
			}
		})
	}
}
func TestConfiguredTaskAdapterIndependentOfQuota(t *testing.T) {
	p := New(Options{TaskAdapter: "missing"})
	s := p.Refresh(context.Background())
	if s.Tasks.Health.State != providers.Unsupported || s.Tasks.State != Unknown || s.Tasks.ActiveCount != nil || s.Tasks.Health.ErrorCode != "task_adapter_unknown" {
		t.Fatal(s)
	}
	disabled := New(Options{Enabled: true}).Snapshot()
	if disabled.Tasks.Health.State != providers.Disabled {
		t.Fatal(disabled)
	}
	legacy := New(Options{DesktopEnabled: true, Home: t.TempDir(), ThreadIDs: []string{"a"}})
	explicit := New(Options{TaskAdapter: "auto", Home: t.TempDir(), ThreadIDs: []string{"a"}})
	if legacy.Snapshot().Tasks.Adapter != explicit.Snapshot().Tasks.Adapter {
		t.Fatal("legacy adapter selection changed")
	}
}
