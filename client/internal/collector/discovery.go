package collector

import (
	"context"
	"sort"
	"strings"
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
	ctx, cancel := context.WithTimeout(ctx, m.opts.ApplyTimeout)
	defer cancel()
	batch, reasons := gather(ctx, m.opts.CoreFactory(), m.opts.NativeFactory(), allSources())
	if ctx.Err() != nil {
		return Capabilities{}, ctx.Err()
	}
	caps := capabilitiesFrom(batch, reasons)

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
				c = &MetricCapability{ID: id, Measurement: sample.Name, Field: field, Unit: unit(sample.Name, field), UploadSupported: uploadSupported(sample.Name, field), Objects: []map[string]string{}}
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
