// Package companion combines provider snapshots without coupling them to GUI or ESP32 transport.
package companion

import (
	"context"
	"esp32-monitor/client/internal/nowplaying"
	"esp32-monitor/client/internal/providers/codex"
	"esp32-monitor/client/internal/providers/github"
	"sync"
	"time"
)

type Snapshot struct {
	SchemaVersion int                 `json:"schema_version"`
	ObservedAt    time.Time           `json:"observed_at"`
	GitHub        github.Snapshot     `json:"github"`
	Codex         codex.Snapshot      `json:"codex"`
	Music         nowplaying.Snapshot `json:"music"`
}
type Service struct {
	Music  nowplaying.Adapter
	GitHub *github.Provider
	Codex  interface {
		Refresh(context.Context) codex.Snapshot
		Snapshot() codex.Snapshot
		RunTasks(context.Context)
		WaitTasks(context.Context)
	}
}

func (s *Service) Refresh(ctx context.Context) Snapshot {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); s.GitHub.Refresh(ctx, false) }()
	go func() { defer wg.Done(); s.Codex.Refresh(ctx) }()
	wg.Wait()
	return s.Snapshot()
}
func (s *Service) Snapshot() Snapshot {
	result := Snapshot{SchemaVersion: 1, ObservedAt: time.Now().UTC()}
	if s.GitHub != nil {
		result.GitHub = s.GitHub.Snapshot()
	}
	if s.Codex != nil {
		result.Codex = s.Codex.Snapshot()
	}
	if s.Music != nil {
		result.Music = s.Music.Snapshot()
	}
	return result
}
