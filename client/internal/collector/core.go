package collector

import (
	"context"
	"errors"
	"fmt"
	"math"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	gnet "github.com/shirou/gopsutil/v4/net"
)

// CoreCollector owns its CPU baseline; discovery and other generations cannot
// consume the running collector's deltas via gopsutil's global Percent cache.
type CoreCollector struct {
	mu         sync.Mutex
	fixed      *coreFixed
	maxRateGap time.Duration
	previous   map[string]cpu.TimesStat
	rates      map[string]rateSample
}

func (c *CoreCollector) Gather(ctx context.Context, ids []string) (Batch, map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	fixed := c.fixedLocked(ctx)
	out := Batch{Metrics: []Metric{}}
	reasons := map[string]string{}
	if c.rates == nil && (slices.Contains(ids, "diskio") || slices.Contains(ids, "net")) {
		c.rates = map[string]rateSample{}
		now := time.Now()
		if slices.Contains(ids, "diskio") {
			if values, err := disk.IOCountersWithContext(ctx); err == nil {
				for name, v := range values {
					c.rates["diskio"+tagKey(map[string]string{"name": name})] = rateSample{now, map[string]float64{"read_bytes": float64(v.ReadBytes), "write_bytes": float64(v.WriteBytes)}}
				}
			}
		}
		if slices.Contains(ids, "net") {
			if values, err := gnet.IOCountersWithContext(ctx, true); err == nil {
				for _, v := range values {
					c.rates["net"+tagKey(map[string]string{"interface": v.Name})] = rateSample{now, map[string]float64{"bytes_sent": float64(v.BytesSent), "bytes_recv": float64(v.BytesRecv)}}
				}
			}
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
		case <-timer.C:
		}
	}

	add := func(name string, tags map[string]string, fields map[string]float64) {
		for k, v := range fields {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				delete(fields, k)
			}
		}
		if len(fields) > 0 {
			out.Metrics = append(out.Metrics, Metric{Name: name, Tags: tags, Fields: fields, Timestamp: time.Now().Unix()})
		}
	}
	for _, id := range ids {
		if ctx.Err() != nil {
			reasons[id] = gatherReason(ctx.Err())
			continue
		}
		switch id {
		case "cpu":
			samples, err := c.cpu(ctx)
			if err != nil {
				reasons[id] = gatherReason(err)
				continue
			}
			for _, m := range samples {
				add("cpu", m.Tags, m.Fields)
			}
		case "mem":
			v, err := mem.VirtualMemoryWithContext(ctx)
			if err != nil {
				reasons[id] = gatherReason(err)
				continue
			}
			f := map[string]float64{"available": float64(v.Available), "used": float64(v.Used), "free": float64(v.Free), "used_percent": v.UsedPercent}
			if total, ok := fixedMetric(fixed.batch, "mem", "", "").Fields["total"]; ok {
				f["total"] = total
			}
			if v.Total > 0 {
				f["available_percent"] = float64(v.Available) / float64(v.Total) * 100
			}
			// gopsutil leaves unsupported OS-specific struct members at zero. Only
			// expose members whose semantics are supplied on this OS.
			switch runtime.GOOS {
			case "linux":
				f["cached"] = float64(v.Cached)
				f["buffered"] = float64(v.Buffers)
				f["active"] = float64(v.Active)
				f["inactive"] = float64(v.Inactive)
			case "darwin":
				f["active"] = float64(v.Active)
				f["inactive"] = float64(v.Inactive)
				f["wired"] = float64(v.Wired)
			}
			add(id, map[string]string{}, f)
			out.Metrics[len(out.Metrics)-1].Exact = map[string]string{"used": strconv.FormatUint(v.Used, 10)}
		case "swap":
			v, err := mem.SwapMemoryWithContext(ctx)
			if err != nil {
				reasons[id] = gatherReason(err)
				continue
			}
			f := map[string]float64{"total": float64(v.Total), "used": float64(v.Used), "free": float64(v.Free), "used_percent": v.UsedPercent}
			if runtime.GOOS == "linux" {
				f["in"] = float64(v.Sin)
				f["out"] = float64(v.Sout)
			}
			add(id, map[string]string{}, f)
			out.Metrics[len(out.Metrics)-1].Exact = map[string]string{"total": strconv.FormatUint(v.Total, 10), "used": strconv.FormatUint(v.Used, 10)}
		case "disk":
			partitions := fixed.partitions
			seen := map[string]bool{}
			for _, p := range partitions {
				if seen[p.Mountpoint] {
					continue
				}
				seen[p.Mountpoint] = true
				v, err := disk.UsageWithContext(ctx, p.Mountpoint)
				if err != nil {
					reasons[id] = gatherReason(err)
					continue
				}
				f := map[string]float64{"free": float64(v.Free), "used": float64(v.Used), "used_percent": v.UsedPercent}
				if total, ok := fixedMetric(fixed.batch, "disk", "path", p.Mountpoint).Fields["total"]; ok {
					f["total"] = total
				}
				if runtime.GOOS != "windows" {
					f["inodes_total"] = float64(v.InodesTotal)
					f["inodes_free"] = float64(v.InodesFree)
					f["inodes_used"] = float64(v.InodesUsed)
					f["inodes_used_percent"] = v.InodesUsedPercent
				}
				mode := "rw"
				if slices.Contains(p.Opts, "ro") {
					mode = "ro"
				}
				add(id, map[string]string{"path": p.Mountpoint, "device": strings.TrimPrefix(p.Device, "/dev/"), "fstype": p.Fstype, "mode": mode}, f)
				out.Metrics[len(out.Metrics)-1].Exact = map[string]string{"used": strconv.FormatUint(v.Used, 10)}
			}
		case "diskio":
			counters, err := disk.IOCountersWithContext(ctx)
			if err != nil {
				reasons[id] = gatherReason(err)
				continue
			}
			names := []string{}
			for name := range counters {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				v := counters[name]
				fields := map[string]float64{"reads": float64(v.ReadCount), "writes": float64(v.WriteCount), "read_bytes": float64(v.ReadBytes), "write_bytes": float64(v.WriteBytes), "read_time": float64(v.ReadTime), "write_time": float64(v.WriteTime)}
				if runtime.GOOS != "windows" {
					fields["io_time"] = float64(v.IoTime)
				}
				add(id, map[string]string{"name": name}, fields)
			}
		case "net":
			counters, err := gnet.IOCountersWithContext(ctx, true)
			if err != nil {
				reasons[id] = gatherReason(err)
				continue
			}
			for _, v := range counters {
				add(id, map[string]string{"interface": v.Name}, map[string]float64{"bytes_sent": float64(v.BytesSent), "bytes_recv": float64(v.BytesRecv), "packets_sent": float64(v.PacketsSent), "packets_recv": float64(v.PacketsRecv), "err_in": float64(v.Errin), "err_out": float64(v.Errout), "drop_in": float64(v.Dropin), "drop_out": float64(v.Dropout)})
			}
		case "system":
			cached := fixedMetric(fixed.batch, "system", "", "")
			f, tags := cached.Fields, cached.Tags
			if uptime, err := host.UptimeWithContext(ctx); err == nil {
				f["uptime"] = float64(uptime)
			}
			if runtime.GOOS != "windows" {
				avg, err := load.AvgWithContext(ctx)
				if err == nil {
					f["load1"] = avg.Load1
					f["load5"] = avg.Load5
					f["load15"] = avg.Load15
				}

			}
			add(id, tags, f)
		}
	}
	out = c.restrictAndRate(out)
	sort.Slice(out.Metrics, func(i, j int) bool {
		a, b := out.Metrics[i], out.Metrics[j]
		return a.Name+tagKey(a.Tags) < b.Name+tagKey(b.Tags)
	})
	return out, reasons
}
func cpuTimes(ctx context.Context) (map[string]cpu.TimesStat, error) {
	per, err := cpu.TimesWithContext(ctx, true)
	if err != nil {
		return nil, err
	}
	total, err := cpu.TimesWithContext(ctx, false)
	if err != nil {
		return nil, err
	}
	result := map[string]cpu.TimesStat{}
	for i, v := range per {
		name := v.CPU
		if name == "" {
			name = fmt.Sprintf("cpu%d", i)
		}
		result[name] = v
	}
	if len(total) > 0 {
		result["cpu-total"] = total[0]
	}
	return result, nil
}
func (c *CoreCollector) cpu(ctx context.Context) ([]Metric, error) {
	warming := c.previous == nil
	if warming {
		baseline, err := cpuTimes(ctx)
		if err != nil {
			return nil, err
		}
		c.previous = baseline
	}
	// Darwin's aggregate counters may stay unchanged across a short first
	// interval. Wait for a real total delta instead of publishing transiently
	// incomplete capabilities. Each collector retains its own baseline.
	sampleCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	for {
		if warming {
			timer := time.NewTimer(100 * time.Millisecond)
			select {
			case <-sampleCtx.Done():
				timer.Stop()
				return nil, sampleCtx.Err()
			case <-timer.C:
			}
		}
		current, err := cpuTimes(sampleCtx)
		if err != nil {
			return nil, err
		}
		result := []Metric{}
		totalReady := false
		for name, now := range current {
			prev, ok := c.previous[name]
			if !ok {
				continue
			}
			fields := cpuUsage(prev, now)
			if len(fields) > 0 {
				if name == "cpu-total" {
					totalReady = true
				}
				result = append(result, Metric{Name: "cpu", Tags: map[string]string{"cpu": name}, Fields: fields})
			}
		}
		if totalReady {
			c.previous = current
			return result, nil
		}
		if !warming {
			c.previous = current
			return nil, errors.New("cpu_delta_unavailable")
		}
	}
}
func cpuUsage(prev, now cpu.TimesStat) map[string]float64 {
	// Linux guest times are already included in user/nice. Telegraf excludes
	// them from the denominator and reports the guest deltas separately.
	total := func(v cpu.TimesStat) float64 {
		return v.User + v.System + v.Idle + v.Nice + v.Iowait + v.Irq + v.Softirq + v.Steal
	}
	delta := total(now) - total(prev)
	if delta <= 0 {
		return nil
	}
	f := map[string]float64{}
	for k, v := range map[string]float64{"user": now.User - prev.User - (now.Guest - prev.Guest), "system": now.System - prev.System, "idle": now.Idle - prev.Idle} {
		f["usage_"+k] = max(0, min(100, v/delta*100))
	}
	switch runtime.GOOS {
	case "linux":
		for k, v := range map[string]float64{"nice": now.Nice - prev.Nice - (now.GuestNice - prev.GuestNice), "iowait": now.Iowait - prev.Iowait, "irq": now.Irq - prev.Irq, "softirq": now.Softirq - prev.Softirq, "steal": now.Steal - prev.Steal, "guest": now.Guest - prev.Guest, "guest_nice": now.GuestNice - prev.GuestNice} {
			f["usage_"+k] = max(0, min(100, v/delta*100))
		}
	case "darwin":
		f["usage_nice"] = max(0, min(100, (now.Nice-prev.Nice)/delta*100))
	}
	f["usage_active"] = 100 - f["usage_idle"]
	return f
}

// Only the agreed metrics leave the core collector. Counters are converted to
// rates with per-instance baselines; resets and the first reading are omitted.
type rateSample struct {
	at     time.Time
	values map[string]float64
}

func (c *CoreCollector) restrictAndRate(batch Batch) Batch {
	allowed := map[string]map[string]bool{
		"cpu": {"usage_active": true}, "mem": {"total": true, "used": true, "used_percent": true},
		"swap":   {"total": true, "used": true, "used_percent": true},
		"disk":   {"total": true, "used": true, "used_percent": true},
		"system": {"uptime": true, "load1": true, "load5": true, "load15": true, "n_cpus": true},
	}
	if c.rates == nil {
		c.rates = map[string]rateSample{}
	}
	result := Batch{Metrics: []Metric{}}
	now := time.Now()
	for _, m := range batch.Metrics {
		if m.Name == "diskio" || m.Name == "net" {
			key := m.Name + tagKey(m.Tags)
			old, ok := c.rates[key]
			fields := map[string]float64{}
			names := map[string]string{"read_bytes": "read_bytes_per_second", "write_bytes": "write_bytes_per_second"}
			if m.Name == "net" {
				names = map[string]string{"bytes_sent": "upload_bytes_per_second", "bytes_recv": "download_bytes_per_second"}
			}
			if ok && now.After(old.at) && (c.maxRateGap == 0 || now.Sub(old.at) <= c.maxRateGap) {
				for input, output := range names {
					v, exists := m.Fields[input]
					previous, had := old.values[input]
					if exists && had && v >= previous {
						fields[output] = (v - previous) / now.Sub(old.at).Seconds()
					}
				}
			}
			c.rates[key] = rateSample{now, m.Fields}
			m.Fields = fields
		} else {
			for k := range m.Fields {
				if !allowed[m.Name][k] {
					delete(m.Fields, k)
				}
			}
		}
		if len(m.Fields) > 0 {
			result.Metrics = append(result.Metrics, m)
		}
	}
	return result
}
