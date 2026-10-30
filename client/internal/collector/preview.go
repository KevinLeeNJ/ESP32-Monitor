package collector

import (
	"context"
	"time"
)

// previewTTL comfortably exceeds the GUI snapshot poll interval.
const previewTTL = 15 * time.Second

// MarkPreview records that a GUI is consuming snapshots. Preview gathering
// stops once the TTL expires, so a headless Agent only reads upload selections.
func (m *Manager) MarkPreview() {
	now := time.Now()
	previous := m.previewAt.Swap(now.UnixNano())
	if previous == 0 || now.Sub(time.Unix(0, previous)) >= previewTTL {
		select {
		case m.previewCh <- struct{}{}:
		default:
		}
	}
	// A first-run Client has no committed upload configuration, so there is no
	// collection generation yet.  Start an in-memory preview generation on
	// demand. It is deliberately not persisted and cannot upload anything.
	m.mu.Lock()
	start := m.current == nil && !m.previewStarting
	if start {
		m.previewStarting = true
	}
	m.mu.Unlock()
	if start {
		go m.startPreview()
	}
}

func (m *Manager) startPreview() {
	m.op.Lock()
	defer m.op.Unlock()
	defer func() {
		m.mu.Lock()
		m.previewStarting = false
		m.mu.Unlock()
	}()
	if m.closed || m.current != nil || !m.previewActive() {
		return
	}
	snapshot := m.Snapshot()
	previewConfig := detached(snapshot.Config)
	// A failed restore must remain visible and must never start uploading as a
	// side effect of opening the GUI. Preserve its committed configuration.
	previewConfig.Selections = nil
	if previewConfig.Upload != nil {
		previewConfig.Upload.Enabled = false
	}
	if previewConfig.IntervalSeconds < 5 || previewConfig.IntervalSeconds > 60 {
		previewConfig.IntervalSeconds = 10
	}
	g, err := m.start(m.ctx, previewConfig, snapshot.Revision)
	if err != nil {
		_, _ = m.failure(err)
		return
	}
	g.previewOnly = true
	m.mu.Lock()
	m.current = g
	m.mu.Unlock()
	m.accept(g, g.staged, true)
	ctx, cancel := context.WithCancel(m.ctx)
	g.cancel = cancel
	g.done = make(chan struct{})
	go m.collectLoop(ctx, g)
}

func (m *Manager) previewActive() bool {
	at := m.previewAt.Load()
	return at != 0 && time.Since(time.Unix(0, at)) < previewTTL
}
