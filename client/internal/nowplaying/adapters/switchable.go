package adapters

import (
	"context"
	"sync"

	"esp32-monitor/client/internal/nowplaying"
)

// Switchable keeps the public adapter stable while replacing its worker.
type Switchable struct {
	mu      sync.Mutex
	options Options
	adapter nowplaying.Adapter
	ctx     context.Context
	cancel  context.CancelFunc
	done    chan struct{}
}

func NewSwitchable(options Options) (*Switchable, error) {
	adapter, err := New(options)
	if err != nil {
		return nil, err
	}
	return &Switchable{options: options, adapter: adapter}, nil
}
func (s *Switchable) ID() string { s.mu.Lock(); defer s.mu.Unlock(); return s.adapter.ID() }
func (s *Switchable) Snapshot() nowplaying.Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.adapter.Snapshot()
}
func (s *Switchable) WaitReady(ctx context.Context) error {
	s.mu.Lock()
	adapter := s.adapter
	s.mu.Unlock()
	return adapter.WaitReady(ctx)
}
func (s *Switchable) start() {
	ctx, cancel := context.WithCancel(s.ctx)
	s.cancel = cancel
	s.done = make(chan struct{})
	adapter, done := s.adapter, s.done
	go func() { defer close(done); adapter.Run(ctx) }()
}
func (s *Switchable) stop() {
	if s.cancel != nil {
		s.cancel()
		<-s.done
		s.cancel = nil
		s.done = nil
	}
}
func (s *Switchable) Run(ctx context.Context) {
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
func (s *Switchable) SetEnabled(enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.options.Enabled == enabled {
		return nil
	}
	options := s.options
	options.Enabled = enabled
	adapter, err := New(options)
	if err != nil {
		return err
	}
	s.stop()
	s.options, s.adapter = options, adapter
	if s.ctx != nil && s.ctx.Err() == nil {
		s.start()
	}
	return nil
}
