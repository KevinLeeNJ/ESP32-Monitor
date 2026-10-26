// Package nowplaying defines the UI-independent, read-only system media contract.
package nowplaying

import (
	"context"
	"esp32-monitor/client/internal/providers"
	"time"
)

type PlaybackState string

const (
	Playing    PlaybackState = "playing"
	NotPlaying PlaybackState = "not_playing"
)

type Current struct {
	Title              *string       `json:"title"`
	Artist             *string       `json:"artist"`
	Source             *string       `json:"source"`
	State              PlaybackState `json:"state"`
	Duration           *float64      `json:"duration"` // seconds; null means unavailable
	Position           *float64      `json:"position"` // seconds at PositionObservedAt
	PositionObservedAt *time.Time    `json:"position_observed_at"`
}

type Snapshot struct {
	Adapter  string           `json:"adapter"`
	Health   providers.Health `json:"health"`
	Coverage string           `json:"coverage"`
	Current  *Current         `json:"current"`
}

// Snapshot must return a detached copy without blocking on platform I/O.
// Run owns resources until cancellation. WaitReady waits for the first result,
// including empty or failed reads; it is not a guarantee of healthy data.
type Adapter interface {
	ID() string
	Run(context.Context)
	WaitReady(context.Context) error
	Snapshot() Snapshot
}
