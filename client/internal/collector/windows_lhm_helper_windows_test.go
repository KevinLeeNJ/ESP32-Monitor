//go:build windows

package collector

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"esp32-monitor/client/internal/windowshelper"
)

func TestLHMSharedCacheAndCancellation(t *testing.T) {
	cache := newWindowsLHMCache(nil)
	var reads atomic.Int32
	entered, release := make(chan struct{}), make(chan struct{})
	cache.readSensors = func(ctx context.Context) (json.RawMessage, error) {
		reads.Add(1)
		close(entered)
		select {
		case <-release:
			return json.RawMessage(`{"ok":true,"schema":1,"cpu_temperature_celsius":42}`), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	collector := &windowsCollector{lhm: cache}
	fork := collector.Fork().(*windowsCollector)
	if fork.lhm != cache {
		t.Fatal("fork did not share cache")
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if data, err := cache.read(context.Background()); err != nil || data.CPU == nil {
			t.Errorf("%+v %v", data, err)
		}
	}()
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := fork.lhm.read(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	close(release)
	wg.Wait()
	for i := 0; i < 3; i++ {
		if _, err := fork.lhm.read(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if reads.Load() != 1 {
		t.Fatalf("reads=%d", reads.Load())
	}
}

func TestLHMFailureDoesNotRetainOldReadings(t *testing.T) {
	cache := newWindowsLHMCache(nil)
	cache.readSensors = func(context.Context) (json.RawMessage, error) {
		return json.RawMessage(`{"ok":true,"schema":1,"cpu_temperature_celsius":42}`), nil
	}
	if _, err := cache.read(context.Background()); err != nil {
		t.Fatal(err)
	}
	cache.at = time.Now().Add(-time.Minute)
	cache.readSensors = func(context.Context) (json.RawMessage, error) { return nil, windowshelper.ErrExited }
	for i := 0; i < 2; i++ {
		data, err := cache.read(context.Background())
		if err == nil || data.CPU != nil {
			t.Fatalf("stale data: %+v %v", data, err)
		}
	}
	if lhmReason(windowshelper.ErrMissing) != "lhm_helper_missing" {
		t.Fatal("missing helper classification")
	}
}
