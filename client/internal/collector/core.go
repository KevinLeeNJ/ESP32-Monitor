package collector

import (
	"context"
	"errors"
	"fmt"
	"math"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	gnet "github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/process"
)

// CoreCollector owns its CPU baseline; discovery and other generations cannot
// consume the running collector's deltas via gopsutil's global Percent cache.
type CoreCollector struct {
	mu       sync.Mutex
	previous map[string]cpu.TimesStat
}

func (c *CoreCollector) Gather(ctx context.Context, ids []string) (Batch, map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := Batch{Metrics: []Metric{}}
	reasons := map[string]string{}
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
			f := map[string]float64{"total": float64(v.Total), "available": float64(v.Available), "used": float64(v.Used), "free": float64(v.Free), "used_percent": v.UsedPercent}
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
		case "disk":
			partitions, err := disk.PartitionsWithContext(ctx, false)
			if err != nil {
				reasons[id] = gatherReason(err)
				continue
			}
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
				f := map[string]float64{"total": float64(v.Total), "free": float64(v.Free), "used": float64(v.Used), "used_percent": v.UsedPercent}
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
		case "netstat":
			connections, err := gnet.ConnectionsWithContext(ctx, "all")
			if err != nil {
				reasons[id] = gatherReason(err)
				continue
			}
			f := map[string]float64{"tcp_established": 0, "tcp_syn_sent": 0, "tcp_syn_recv": 0, "tcp_fin_wait1": 0, "tcp_fin_wait2": 0, "tcp_time_wait": 0, "tcp_close": 0, "tcp_close_wait": 0, "tcp_last_ack": 0, "tcp_listen": 0, "tcp_closing": 0, "tcp_none": 0, "udp_socket": 0}
			for _, v := range connections {
				if v.Type == 2 {
					f["udp_socket"]++
					continue
				}
				if v.Type != 1 {
					continue
				}
				key := "tcp_" + strings.ToLower(v.Status)
				if _, ok := f[key]; ok {
					f[key]++
				} else {
					f["tcp_none"]++
				}
			}
			add(id, map[string]string{}, f)
		case "processes":
			pids, err := process.PidsWithContext(ctx)
			if err != nil {
				reasons[id] = gatherReason(err)
				continue
			}
			f := map[string]float64{"total": float64(len(pids)), "running": 0, "sleeping": 0, "stopped": 0, "zombies": 0, "unknown": 0, "total_threads": 0}
			threadsComplete := true
			for _, pid := range pids {
				p, err := process.NewProcessWithContext(ctx, pid)
				if err != nil {
					f["unknown"]++
					threadsComplete = false
					continue
				}
				status, err := p.StatusWithContext(ctx)
				key := "unknown"
				if err == nil && len(status) > 0 {
					switch status[0] {
					case "running":
						key = "running"
					case "sleep", "idle", "wait", "lock":
						key = "sleeping"
					case "stop":
						key = "stopped"
					case "zombie":
						key = "zombies"
					}
				}
				f[key]++
				threads, err := p.NumThreadsWithContext(ctx)
				if err != nil {
					threadsComplete = false
				} else {
					f["total_threads"] += float64(threads)
				}
			}
			if !threadsComplete {
				delete(f, "total_threads")
			}
			add(id, map[string]string{}, f)
		case "system":
			f := map[string]float64{}
			uptime, err := host.UptimeWithContext(ctx)
			if err != nil {
				reasons[id] = gatherReason(err)
			} else {
				f["uptime"] = float64(uptime)
			}
			count, err := cpu.CountsWithContext(ctx, true)
			if err == nil {
				f["n_cpus"] = float64(count)
			}
			if runtime.GOOS != "windows" {
				avg, err := load.AvgWithContext(ctx)
				if err == nil {
					f["load1"] = avg.Load1
					f["load5"] = avg.Load5
					f["load15"] = avg.Load15
				}
				users, err := host.UsersWithContext(ctx)
				if err == nil {
					f["n_users"] = float64(len(users))
				}
			}
			add(id, map[string]string{}, f)
		}
	}
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
