package collector

import (
	"context"
	"errors"
	"os"
	"runtime"
	"slices"
	"strings"
	"time"
)

// Collector returns local observations (including exact integer text) and failures.
// Failed/unimplemented sources emit no invented zero samples.
type Collector interface {
	Gather(context.Context, []string) (Batch, map[string]string)
}
type source struct{ id, category, collector string }

var sources = []source{
	{"cpu", "cpu", "core"}, {"mem", "memory", "core"}, {"swap", "memory", "core"},
	{"disk", "disk", "core"}, {"diskio", "disk", "core"}, {"net", "network", "core"},
	{"system", "system", "core"},
	{"darwin_cpu", "cpu", "native"}, {"darwin_memory", "memory", "native"}, {"darwin_gpu", "gpu", "native"}, {"darwin_disk", "disk", "native"}, {"darwin_network", "network", "native"}, {"darwin_battery", "battery", "native"}, {"darwin_fan", "cpu", "native"}, {"darwin_public_ip", "network", "native"}, {"darwin_system", "system", "native"},
}

func allSources() []string {
	out := []string{}
	for _, s := range sources {
		out = append(out, s.id)
	}
	return out
}
func selectedSources(c Config) []string {
	out := []string{}
	for _, s := range c.Selections {
		id := strings.Split(s.ID, "/")[0]
		if !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}
func gather(ctx context.Context, core, native Collector, ids []string) (Batch, map[string]string) {
	out := Batch{Metrics: []Metric{}}
	reasons := map[string]string{}
	for _, provider := range []struct {
		name      string
		collector Collector
	}{{"core", core}, {"native", native}} {
		selected := []string{}
		for _, src := range sources {
			if src.collector == provider.name && slices.Contains(ids, src.id) {
				selected = append(selected, src.id)
			}
		}
		if len(selected) == 0 {
			continue
		}
		batch, failed := provider.collector.Gather(ctx, selected)
		out.Metrics = append(out.Metrics, batch.Metrics...)
		for id, reason := range failed {
			reasons[id] = reason
		}
	}
	return out, reasons
}
func capabilitiesFrom(batch Batch, reasons map[string]string) Capabilities {
	caps := Capabilities{SchemaVersion: SchemaVersion, CoreCollector: "gopsutil", NativeCollector: runtime.GOOS, OS: runtime.GOOS, Arch: runtime.GOARCH, ObservedAt: time.Now(), Categories: []Category{
		{ID: "cpu", Name: "CPU", Sources: []Capability{}}, {ID: "memory", Name: "内存（Swap）", Sources: []Capability{}},
		{ID: "gpu", Name: "GPU", Sources: []Capability{}}, {ID: "disk", Name: "磁盘", Sources: []Capability{}}, {ID: "network", Name: "网络", Sources: []Capability{}},
		{ID: "battery", Name: "电池", Sources: []Capability{}}, {ID: "system", Name: "系统", Sources: []Capability{}},
	}}
	for _, src := range sources {
		samples := Batch{Metrics: []Metric{}}
		for _, m := range batch.Metrics {
			if m.Name == src.id {
				samples.Metrics = append(samples.Metrics, m)
			}
		}
		c := Capability{ID: src.id, Collector: src.collector, State: "unsupported", Reason: reasons[src.id], Metrics: capabilitiesFor(src.id, samples)}
		if len(c.Metrics) > 0 {
			c.State = "available"
			if c.Reason != "" {
				c.State = "partial"
			}
		} else if c.Reason == "permission_required" {
			c.State = "permission_required"
		}
		for i := range caps.Categories {
			if caps.Categories[i].ID == src.category {
				caps.Categories[i].Sources = append(caps.Categories[i].Sources, c)
			}
		}
	}
	return caps
}
func gatherReason(err error) string {
	if errors.Is(err, os.ErrPermission) {
		return "permission_required"
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return "collect_timeout_or_canceled"
	}
	return "collect_failed"
}

// NativeCollector is the platform extension point for platform-owned metrics.
// Platform constructors intentionally return this stub until native adapters exist.
type NativeCollector struct{ Platform string }

func (n *NativeCollector) Gather(ctx context.Context, ids []string) (Batch, map[string]string) {
	reasons := map[string]string{}
	for _, id := range ids {
		reasons[id] = "native_collector_stub"
	}
	return Batch{Metrics: []Metric{}}, reasons
}
