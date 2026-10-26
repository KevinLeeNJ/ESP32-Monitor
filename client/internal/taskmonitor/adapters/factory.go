// Package adapters selects task sources at the composition boundary. Platform
// selection never falls back to a weaker source after a runtime source failure.
package adapters

import (
	"context"
	"esp32-monitor/client/internal/providers"
	"esp32-monitor/client/internal/taskmonitor"
	"time"
)

type Options struct {
	Home      string
	ThreadIDs []string
	Now       func() time.Time
}

// New is called only after observation is explicitly enabled. An empty name is
// an alias for auto to preserve --codex-desktop compatibility. Unknown names
// return an explicit unsupported result rather than silently selecting auto.
func New(name string, opts Options) taskmonitor.Adapter {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	switch name {
	case "", "auto":
		return platformDefault(opts)
	case "desktop-ipc":
		return platformDesktop(opts)
	default:
		return unavailable{name: name, code: "task_adapter_unknown", now: opts.Now}
	}
}

type unavailable struct {
	name, code string
	now        func() time.Time
}

func (a unavailable) ID() string                { return a.name }
func (a unavailable) Run(context.Context)       {}
func (a unavailable) WaitReady(context.Context) {}
func (a unavailable) Snapshot() taskmonitor.Tasks {
	return taskmonitor.Tasks{Adapter: a.name, Health: providers.Health{State: providers.Unsupported, ObservedAt: a.now(), ErrorCode: a.code}, Scope: "unavailable", State: taskmonitor.Unknown, Items: []taskmonitor.Task{}}
}
