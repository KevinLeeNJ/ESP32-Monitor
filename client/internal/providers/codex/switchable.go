package codex

import (
	"context"
	"sync"
)

// Switchable owns task-observer workers while preserving the provider interface.
type Switchable struct {
	mu            sync.Mutex
	options       Options
	provider      *Provider
	ctx           context.Context
	cancel        context.CancelFunc
	done          chan struct{}
	refreshCtx    context.Context
	refreshCancel context.CancelFunc
}

func NewSwitchable(options Options) *Switchable {
	ctx, cancel := context.WithCancel(context.Background())
	return &Switchable{options: options, provider: New(options), refreshCtx: ctx, refreshCancel: cancel}
}
func (s *Switchable) Snapshot() Snapshot {
	s.mu.Lock()
	p := s.provider
	s.mu.Unlock()
	return p.Snapshot()
}
func (s *Switchable) Refresh(ctx context.Context) Snapshot {
	s.mu.Lock()
	p := s.provider
	generation := s.refreshCtx
	s.mu.Unlock()
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(generation, cancel)
	defer func() { stop(); cancel() }()
	return p.Refresh(ctx)
}
func (s *Switchable) WaitTasks(ctx context.Context) {
	s.mu.Lock()
	p := s.provider
	s.mu.Unlock()
	p.WaitTasks(ctx)
}
func (s *Switchable) start() {
	ctx, cancel := context.WithCancel(s.ctx)
	s.cancel = cancel
	s.done = make(chan struct{})
	p, done := s.provider, s.done
	go func() { defer close(done); p.RunTasks(ctx) }()
}
func (s *Switchable) stop() {
	if s.cancel != nil {
		s.cancel()
		<-s.done
		s.cancel = nil
		s.done = nil
	}
}
func (s *Switchable) RunTasks(ctx context.Context) {
	s.mu.Lock()
	s.ctx = ctx
	s.start()
	s.mu.Unlock()
	<-ctx.Done()
	s.mu.Lock()
	s.stop()
	s.ctx = nil
	s.mu.Unlock()
}
func (s *Switchable) SetEnabled(enabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.options.Enabled == enabled {
		return
	}
	options := s.options
	options.Enabled = enabled
	if enabled && options.TaskAdapter == "" && !options.DesktopEnabled {
		options.TaskAdapter = "auto"
	}
	if !enabled {
		options.TaskAdapter = ""
		options.DesktopEnabled = false
		options.Tasks = nil
	}
	p := New(options)
	s.refreshCancel()
	s.refreshCtx, s.refreshCancel = context.WithCancel(context.Background())
	s.stop()
	s.options.Enabled = enabled
	s.provider = p
	if s.ctx != nil && s.ctx.Err() == nil {
		s.start()
	}
}
