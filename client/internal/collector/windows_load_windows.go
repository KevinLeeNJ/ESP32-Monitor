//go:build windows

package collector

import (
	"context"
	"errors"
	"math"
	"strings"
	"sync"
	"time"
)

type windowsPerfSystem struct {
	ProcessorQueueLength *uint32
}

type windowsPerfProcessor struct {
	Name                 string
	PercentProcessorTime *uint64
}

// windowsRunnable returns the overall processor busy percentage and the
// processor queue length. windows_exporter exposes the same queue counter in
// its system collector; it has no load-average metric.
func windowsRunnable(ctx context.Context) (float64, float64, error) {
	var system []windowsPerfSystem
	if err := windowsQuery(ctx, `root\cimv2`, `SELECT ProcessorQueueLength FROM Win32_PerfFormattedData_PerfOS_System`, &system); err != nil {
		return 0, 0, err
	}
	var processors []windowsPerfProcessor
	if err := windowsQuery(ctx, `root\cimv2`, `SELECT Name,PercentProcessorTime FROM Win32_PerfFormattedData_PerfOS_Processor`, &processors); err != nil {
		return 0, 0, err
	}
	if len(system) == 0 || system[0].ProcessorQueueLength == nil {
		return 0, 0, errors.New("processor queue length unavailable")
	}
	busy, ok := processorBusy(processors)
	if !ok {
		return 0, 0, errors.New("processor time unavailable")
	}
	return busy, float64(*system[0].ProcessorQueueLength), nil
}

func processorBusy(rows []windowsPerfProcessor) (float64, bool) {
	total := -1.0
	sum, count := 0.0, 0
	for _, row := range rows {
		if row.PercentProcessorTime == nil {
			continue
		}
		value := math.Min(100, float64(*row.PercentProcessorTime))
		if row.Name == "_Total" {
			total = value
			continue
		}
		// Per-processor instances are numeric; skip other localized aggregates.
		if row.Name != "" && !strings.HasPrefix(row.Name, "_") {
			sum += value
			count++
		}
	}
	if total >= 0 {
		return total, true
	}
	if count > 0 {
		return sum / float64(count), true
	}
	return 0, false
}

type windowsLoadState struct {
	mu                   sync.Mutex
	last                 time.Time
	load1, load5, load15 float64
}

// observe applies the Unix load-average decay (60 s, 300 s, 900 s) to the
// current runnable estimate. The estimate is seeded on the first sample and
// reseeded after a long gap instead of blending unrelated measurements.
func (l *windowsLoadState) observe(runnable float64, now time.Time) (float64, float64, float64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.last.IsZero() || now.Before(l.last) || now.Sub(l.last) > 5*time.Minute {
		l.load1, l.load5, l.load15 = runnable, runnable, runnable
		l.last = now
		return l.load1, l.load5, l.load15
	}
	elapsed := now.Sub(l.last).Seconds()
	if elapsed <= 0 {
		elapsed = 1
	}
	decay := func(tau float64) float64 { return math.Exp(-elapsed / tau) }
	d1, d5, d15 := decay(60), decay(300), decay(900)
	l.load1 = l.load1*d1 + runnable*(1-d1)
	l.load5 = l.load5*d5 + runnable*(1-d5)
	l.load15 = l.load15*d15 + runnable*(1-d15)
	l.last = now
	return l.load1, l.load5, l.load15
}
