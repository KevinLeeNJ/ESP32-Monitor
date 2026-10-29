package app

import (
	"context"
	"errors"
	"esp32-monitor/client/internal/collector"
)

// Native GUI binding surface; all returned DTOs are detached from the manager.
func (a *Application) DiscoverCollectorMetrics(ctx context.Context) (collector.Capabilities, error) {
	if a.Collector == nil {
		return collector.Capabilities{}, errors.New("collector_disabled")
	}
	return a.Collector.Discover(ctx)
}
func (a *Application) GetCollectorCapabilities() (collector.Capabilities, error) {
	if a.Collector == nil {
		return collector.Capabilities{}, errors.New("collector_disabled")
	}
	return a.Collector.Capabilities(), nil
}
func (a *Application) GetCollectorSnapshot() (collector.Snapshot, error) {
	if a.Collector == nil {
		return collector.Snapshot{}, errors.New("collector_disabled")
	}
	a.Collector.MarkPreview()
	return a.Collector.Snapshot(), nil
}
func (a *Application) ApplyCollectorConfig(ctx context.Context, c collector.Config) (collector.Snapshot, error) {
	if a.Collector == nil {
		return collector.Snapshot{}, errors.New("collector_disabled")
	}
	return a.Collector.Apply(ctx, c)
}
