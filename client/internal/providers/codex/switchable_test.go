package codex

import (
	"context"
	"testing"
	"time"

	"esp32-monitor/client/internal/providers"
)

func TestSwitchableDisablesAndCancelsRefresh(t *testing.T) {
	s := NewSwitchable(Options{Enabled: true})
	started, stopped := make(chan struct{}), make(chan struct{})
	s.provider.connect = func(ctx context.Context, _ string) (*Connection, error) {
		close(started)
		<-ctx.Done()
		close(stopped)
		return nil, ctx.Err()
	}
	done := make(chan struct{})
	go func() { s.Refresh(context.Background()); close(done) }()
	<-started
	s.SetEnabled(false)
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("refresh was not canceled")
	}
	<-done
	if got := s.Snapshot().Quota.Health.State; got != providers.Disabled {
		t.Fatalf("state = %s", got)
	}
}
