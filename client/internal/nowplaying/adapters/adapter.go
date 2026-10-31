package adapters

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"esp32-monitor/client/internal/logging"
	"esp32-monitor/client/internal/nowplaying"
	"esp32-monitor/client/internal/providers"
	"io"
	"math"
	"os/exec"
	"strings"
	"sync"
	"time"
	"unicode"
)

const StaleAfter = 15 * time.Second

// Message is the complete read-only native bridge response, never a diff.
type Message struct {
	Current   *nowplaying.Current `json:"current"`
	ErrorCode string              `json:"error_code,omitempty"`
}

type Options struct {
	Enabled       bool
	Adapter       string // auto, mediaremote, gsmtc, mpris
	NativeLibrary string // optional absolute macOS bridge path
}

type streamAdapter struct {
	mu       sync.RWMutex
	once     sync.Once
	runOnce  sync.Once
	ready    chan struct{}
	snapshot nowplaying.Snapshot
	lastRead time.Time // retains monotonic component for freshness
	command  func(context.Context) (*exec.Cmd, error)
	enabled  bool
}

func New(o Options) (nowplaying.Adapter, error) {
	id, coverage, command := platformCommand(o)
	if o.Adapter != "" && o.Adapter != "auto" && o.Adapter != id {
		return nil, errors.New("music_adapter_unavailable")
	}
	state := providers.Disabled
	if o.Enabled {
		state = providers.Starting
	}
	a := &streamAdapter{ready: make(chan struct{}), enabled: o.Enabled, command: command,
		snapshot: nowplaying.Snapshot{Adapter: id, Coverage: coverage, Health: providers.Health{State: state, ObservedAt: time.Now().UTC(), SourceVersion: "2"}}}
	if !o.Enabled {
		a.markReady()
	}
	return a, nil
}

func (a *streamAdapter) ID() string { return a.snapshot.Adapter }
func (a *streamAdapter) markReady() { a.once.Do(func() { close(a.ready) }) }
func (a *streamAdapter) WaitReady(ctx context.Context) error {
	select {
	case <-a.ready:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (a *streamAdapter) Snapshot() nowplaying.Snapshot {
	a.mu.RLock()
	defer a.mu.RUnlock()
	s := a.snapshot
	if s.Health.LastSuccessAt != nil {
		v := *s.Health.LastSuccessAt
		s.Health.LastSuccessAt = &v
	}
	if s.Current != nil {
		b, _ := json.Marshal(s.Current)
		var c nowplaying.Current
		_ = json.Unmarshal(b, &c)
		s.Current = &c
	}
	if a.enabled && !a.lastRead.IsZero() && time.Since(a.lastRead) >= StaleAfter {
		s.Current = nil
		s.Health.State = providers.Degraded
		s.Health.ErrorCode = "music_stale"
	}
	return s
}
func clean(s *string, limit int) *string {
	if s == nil {
		return nil
	}
	var b strings.Builder
	for _, r := range *s {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
			continue
		}
		if b.Len()+len(string(r)) > limit {
			break
		}
		b.WriteRune(r)
	}
	v := strings.TrimSpace(b.String())
	if v == "" {
		return nil
	}
	return &v
}
func validSeconds(v *float64) *float64 {
	if v == nil || math.IsNaN(*v) || math.IsInf(*v, 0) || *v < 0 {
		return nil
	}
	return v
}
func (a *streamAdapter) update(m Message) {
	if m.ErrorCode == "" && m.Current != nil && m.Current.State != nowplaying.Playing && m.Current.State != nowplaying.NotPlaying {
		m.ErrorCode = "music_protocol_error"
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	previousState, previousCode := a.snapshot.Health.State, a.snapshot.Health.ErrorCode
	now := time.Now()
	a.lastRead = now
	a.snapshot.Health.ObservedAt = now.UTC()
	a.snapshot.Current = nil
	if m.ErrorCode != "" {
		a.snapshot.Health.State = providers.Degraded
		switch m.ErrorCode {
		case "music_permission_denied":
			a.snapshot.Health.State = providers.PermissionDenied
		case "music_unsupported", "music_framework_missing", "music_symbols_missing":
			a.snapshot.Health.State = providers.Unsupported
		}
		// Only bounded protocol error codes reach UI; never pass raw native stderr.
		switch m.ErrorCode {
		case "music_permission_denied", "music_unsupported", "music_framework_missing", "music_symbols_missing", "music_dependency_missing", "music_session_unavailable", "music_read_failed", "music_timeout", "music_bridge_missing", "music_protocol_error", "music_bridge_exited", "music_cancelled":
			a.snapshot.Health.ErrorCode = m.ErrorCode
		default:
			a.snapshot.Health.ErrorCode = "music_read_failed"
		}
	} else {
		a.snapshot.Health.State = providers.Healthy
		a.snapshot.Health.ErrorCode = ""
		t := now.UTC()
		a.snapshot.Health.LastSuccessAt = &t
		if c := m.Current; c != nil {
			c.Title = clean(c.Title, 160)
			c.Artist = clean(c.Artist, 96)
			c.Source = clean(c.Source, 160)
			c.Duration = validSeconds(c.Duration)
			c.Position = validSeconds(c.Position)
			if c.Position != nil {
				if c.Duration != nil && *c.Position > *c.Duration {
					v := *c.Duration
					c.Position = &v
				}
				c.PositionObservedAt = &t
			} else {
				c.PositionObservedAt = nil
			}
			a.snapshot.Current = c
		}
	}
	a.markReady()
	// music_cancelled is the normal shutdown path, not a fault worth warning.
	if a.snapshot.Health.ErrorCode != "music_cancelled" && (a.snapshot.Health.State != previousState || a.snapshot.Health.ErrorCode != previousCode) {
		if a.snapshot.Health.State == providers.Healthy {
			logging.L("music").Info("music_recovered")
		} else {
			logging.L("music").Warn("music_state", "state", a.snapshot.Health.State, "error_code", a.snapshot.Health.ErrorCode)
		}
	}
}

func (a *streamAdapter) Run(ctx context.Context) {
	a.runOnce.Do(func() { a.run(ctx) })
}
func (a *streamAdapter) run(ctx context.Context) {
	if !a.enabled {
		return
	}
	defer a.update(Message{ErrorCode: "music_cancelled"})
	if a.command == nil {
		runNative(ctx, a)
		return
	}
	for ctx.Err() == nil {
		a.readStream(ctx)
		timer := time.NewTimer(2 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
func (a *streamAdapter) readStream(ctx context.Context) {
	child, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd, err := a.command(child)
	if err != nil {
		a.update(Message{ErrorCode: err.Error()})
		return
	}
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		a.update(Message{ErrorCode: "music_read_failed"})
		return
	}
	// Bound subprocess cleanup even when a platform unexpectedly inherits handles.
	cmd.WaitDelay = time.Second
	cmd.Stderr = io.Discard
	if err = cmd.Start(); err != nil {
		a.update(Message{ErrorCode: "music_dependency_missing"})
		return
	}
	// A silent or hung bridge must not keep old content alive or block shutdown.
	watchdog := time.AfterFunc(StaleAfter, func() { cancel() })
	receivedError := false
	protocolError := false
	scanner := bufio.NewScanner(pipe)
	scanner.Buffer(make([]byte, 4096), 64*1024)
	for scanner.Scan() {
		var m Message
		var envelope map[string]json.RawMessage
		line := scanner.Bytes()
		if json.Unmarshal(line, &envelope) != nil || envelope == nil || (envelope["current"] == nil && envelope["error_code"] == nil) || json.Unmarshal(line, &m) != nil {
			a.update(Message{ErrorCode: "music_protocol_error"})
			protocolError = true
			cancel()
			break
		}
		if !watchdog.Reset(StaleAfter) {
			cancel()
			break
		}
		receivedError = m.ErrorCode != ""
		a.update(m)
	}
	watchdog.Stop()
	_ = cmd.Wait()
	if ctx.Err() == nil && !receivedError && !protocolError {
		code := "music_bridge_exited"
		if scanner.Err() != nil {
			code = "music_protocol_error"
		} else if child.Err() != nil {
			code = "music_timeout"
		}
		a.update(Message{ErrorCode: code})
	}
}
