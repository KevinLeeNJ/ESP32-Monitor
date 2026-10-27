package collector

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/mem"
	"net"
)

// fixedCollector forks sampling baselines while sharing the startup-only inventory.
type fixedCollector interface {
	Collector
	Fixed(context.Context) Batch
	Fork() Collector
}

type coreFixed struct {
	once       sync.Once
	batch      Batch
	partitions []disk.PartitionStat
}

func (c *CoreCollector) Fixed(ctx context.Context) Batch {
	c.mu.Lock()
	defer c.mu.Unlock()
	return detached(c.fixedLocked(ctx).batch)
}

func (c *CoreCollector) Fork() Collector {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.fixed == nil {
		c.fixed = &coreFixed{}
	}
	return &CoreCollector{fixed: c.fixed}
}

func (c *CoreCollector) fixedLocked(ctx context.Context) *coreFixed {
	if c.fixed == nil {
		c.fixed = &coreFixed{}
	}
	f := c.fixed
	f.once.Do(func() { f.collect(ctx) })
	return f
}

func (f *coreFixed) collect(ctx context.Context) {
	f.batch = Batch{Metrics: []Metric{}}
	add := func(name string, tags map[string]string, fields map[string]float64) {
		f.batch.Metrics = append(f.batch.Metrics, Metric{Name: name, Tags: tags, Fields: fields, Timestamp: time.Now().Unix()})
	}
	tags, fields := map[string]string{}, map[string]float64{}
	if info, err := cpu.InfoWithContext(ctx); err == nil && len(info) > 0 && info[0].ModelName != "" {
		tags["model"] = info[0].ModelName
	}
	for key, logical := range map[string]bool{"physical_cores": false, "logical_cores": true} {
		if count, err := cpu.CountsWithContext(ctx, logical); err == nil && count > 0 {
			fields[key] = float64(count)
		}
	}
	add("cpu", tags, fields)
	if interfaces, err := net.Interfaces(); err == nil {
		for _, iface := range interfaces {
			add("net", map[string]string{"interface": iface.Name}, map[string]float64{"present": 1})
		}
	}
	if counters, err := disk.IOCountersWithContext(ctx); err == nil {
		for name := range counters {
			add("diskio", map[string]string{"name": name}, map[string]float64{"present": 1})
		}
	}
	tags = map[string]string{}
	if platform, _, version, err := host.PlatformInformationWithContext(ctx); err == nil {
		tags["os_platform"], tags["os_version"] = platform, version
	}
	systemFields := map[string]float64{}
	if count, ok := fields["logical_cores"]; ok {
		systemFields["n_cpus"] = count
	}
	add("system", tags, systemFields)
	if v, err := mem.VirtualMemoryWithContext(ctx); err == nil {
		add("mem", map[string]string{}, map[string]float64{"total": float64(v.Total)})
		f.batch.Metrics[len(f.batch.Metrics)-1].Exact = map[string]string{"total": strconv.FormatUint(v.Total, 10)}
	}
	f.partitions, _ = disk.PartitionsWithContext(ctx, false)
	seen := map[string]bool{}
	for _, p := range f.partitions {
		if seen[p.Mountpoint] {
			continue
		}
		seen[p.Mountpoint] = true
		if v, err := disk.UsageWithContext(ctx, p.Mountpoint); err == nil {
			mode := "rw"
			for _, o := range p.Opts {
				if o == "ro" {
					mode = "ro"
				}
			}
			add("disk", map[string]string{"path": p.Mountpoint, "device": strings.TrimPrefix(p.Device, "/dev/"), "fstype": p.Fstype, "mode": mode}, map[string]float64{"total": float64(v.Total)})
			f.batch.Metrics[len(f.batch.Metrics)-1].Exact = map[string]string{"total": strconv.FormatUint(v.Total, 10)}
		}
	}
}

func fixedMetric(b Batch, name, key, value string) Metric {
	for _, m := range b.Metrics {
		if m.Name == name && (key == "" || m.Tags[key] == value) {
			return detached(m)
		}
	}
	return Metric{Name: name, Tags: map[string]string{}, Fields: map[string]float64{}}
}
