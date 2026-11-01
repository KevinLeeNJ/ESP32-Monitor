package app

import (
	"context"
	"errors"
	"esp32-monitor/client/internal/collector"
)

// Native GUI binding surface; all returned DTOs are detached from the manager.
func (a *Application) DiscoverCollectorMetrics(ctx context.Context) (collector.Capabilities, error) {
	if !a.CollectorEnabled() {
		return collector.Capabilities{}, errors.New("collector_disabled")
	}
	manager := a.Collector()
	if manager == nil {
		return collector.Capabilities{}, errors.New("collector_disabled")
	}
	return manager.Discover(ctx)
}
func (a *Application) GetCollectorCapabilities() (collector.Capabilities, error) {
	if !a.CollectorEnabled() {
		return collector.Capabilities{}, errors.New("collector_disabled")
	}
	manager := a.Collector()
	if manager == nil {
		return collector.Capabilities{}, errors.New("collector_disabled")
	}
	return manager.Capabilities(), nil
}
func (a *Application) GetCollectorSnapshot() (collector.Snapshot, error) {
	if !a.CollectorEnabled() {
		return collector.Snapshot{}, errors.New("collector_disabled")
	}
	manager := a.Collector()
	if manager == nil {
		return collector.Snapshot{}, errors.New("collector_disabled")
	}
	manager.MarkPreview()
	return manager.Snapshot(), nil
}
func (a *Application) ApplyCollectorConfig(ctx context.Context, c collector.Config) (collector.Snapshot, error) {
	if !a.CollectorEnabled() {
		return collector.Snapshot{}, errors.New("collector_disabled")
	}
	manager := a.Collector()
	if manager == nil {
		return collector.Snapshot{}, errors.New("collector_disabled")
	}
	return manager.Apply(ctx, c)
}
