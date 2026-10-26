package app

import (
	"context"
	"esp32-monitor/client/internal/collector"
	"esp32-monitor/client/internal/config"
	"testing"
)

func TestCollectorBindingsDisabled(t *testing.T) {
	a, err := New(config.Config{}, "")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if _, err = a.GetCollectorCapabilities(); err == nil {
		t.Fatal("disabled capabilities have no error")
	}
	if _, err = a.GetCollectorSnapshot(); err == nil {
		t.Fatal("disabled status has no error")
	}
	if _, err = a.DiscoverCollectorMetrics(context.Background()); err == nil {
		t.Fatal("disabled discovery has no error")
	}
	if _, err = a.ApplyCollectorConfig(context.Background(), collector.DefaultConfig()); err == nil {
		t.Fatal("disabled apply has no error")
	}
}
