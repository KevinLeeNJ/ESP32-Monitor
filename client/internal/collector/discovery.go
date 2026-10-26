package collector

import (
	"context"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"time"
)

func (m *Manager) Discover(ctx context.Context) (Capabilities, error) {
	m.op.Lock()
	defer m.op.Unlock()
	if m.closed {
		return Capabilities{}, errClosed
	}
	caps, err := m.discover(ctx)
	if err != nil {
		_, _ = m.failure(err)
	}
	return caps, err
}
func (m *Manager) discover(ctx context.Context) (Capabilities, error) {
	version, err := m.exe.version(ctx)
	if err != nil {
		return Capabilities{}, err
	}
	caps := Capabilities{SchemaVersion: SchemaVersion, TelegrafVersion: version, OS: runtime.GOOS, Arch: runtime.GOARCH, ObservedAt: time.Now(), Categories: []Category{
		{ID: "cpu", Name: "CPU", Sources: []Capability{}}, {ID: "memory", Name: "内存（Swap）", Sources: []Capability{}}, {ID: "disk", Name: "磁盘", Sources: []Capability{}},
		{ID: "network", Name: "网络", Sources: []Capability{}}, {ID: "system", Name: "系统指标", Sources: []Capability{}}, {ID: "hardware", Name: "硬件与传感器", Sources: []Capability{}},
	}}
	for _, s := range sources {
		if ctx.Err() != nil {
			return Capabilities{}, ctx.Err()
		}
		c := Capability{ID: s.id, State: "unsupported", Metrics: []MetricCapability{}}
		switch {
		case s.reason != "":
			c.Reason = s.reason
		case s.platforms != "" && !contains(strings.Fields(s.platforms), runtime.GOOS):
			c.Reason = "unsupported_os"
		default:
			if s.dependency != "" {
				if _, err := exec.LookPath(s.dependency); err != nil {
					c.State = "missing_dependency"
					c.Reason = s.dependency
					break
				}
			}
			batch, reason := m.exe.probe(ctx, s.id)
			c.Reason = reason
			c.State = "unsupported"
			if reason == "permission_required" {
				c.State = "permission_required"
			}
			if len(batch.Metrics) > 0 {
				c.State = "available"
				if reason != "" {
					c.State = "partial"
				}
				c.Metrics = capabilitiesFor(s.id, batch)
			}
		}
		for i := range caps.Categories {
			if caps.Categories[i].ID == s.category {
				caps.Categories[i].Sources = append(caps.Categories[i].Sources, c)
			}
		}
	}
	m.mu.Lock()
	m.caps = caps
	m.mu.Unlock()
	return detached(caps), nil
}
func capabilitiesFor(plugin string, b Batch) []MetricCapability {
	byID := map[string]*MetricCapability{}
	for _, sample := range b.Metrics {
		for field := range sample.Fields {
			id := metricID(plugin, sample.Name, field)
			c := byID[id]
			if c == nil {
				c = &MetricCapability{ID: id, Measurement: sample.Name, Field: field, Unit: unit(field), UploadSupported: uploadSupported(sample.Name, field), Objects: []map[string]string{}}
				if !c.UploadSupported {
					c.UploadReason = "server_schema_not_supported"
				}
				byID[id] = c
			}
			found := false
			for _, o := range c.Objects {
				found = found || sameTags(o, sample.Tags)
			}
			if !found {
				c.Objects = append(c.Objects, sample.Tags)
			}
		}
	}
	ids := []string{}
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := []MetricCapability{}
	for _, id := range ids {
		c := byID[id]
		sort.Slice(c.Objects, func(i, j int) bool { return tagKey(c.Objects[i]) < tagKey(c.Objects[j]) })
		out = append(out, *c)
	}
	return out
}
func tagKey(tags map[string]string) string {
	keys := []string{}
	for k, v := range tags {
		keys = append(keys, k+"="+v)
	}
	sort.Strings(keys)
	return strings.Join(keys, "\x00")
}
