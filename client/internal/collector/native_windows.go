//go:build windows

package collector

import (
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
)

// windowsCollector samples Windows-native metrics. Fixed inventory is probed
// once per Manager lifetime; everything else is read per collection interval.
type windowsCollector struct {
	mu               sync.Mutex
	fixed            *windowsFixed
	load             *windowsLoadState
	cpuTempFailedAt  time.Time
	diskHealth       map[string]float64
	diskHealthAt     time.Time
	partitions       []windowsPartition
	partitionsAt     time.Time
	publicIP         string
	publicIPAt       time.Time
	publicIPFetching bool
	fetchIP          func(context.Context) (string, error)
	lhm              *windowsLHMCache
}

type windowsFixed struct {
	once     sync.Once
	batch    Batch
	gpus     []windowsGPUInfo
	netKinds map[string]string
}

func newNativeCollector() Collector {
	return &windowsCollector{load: &windowsLoadState{}, lhm: &windowsLHMCache{}}
}

func (d *windowsCollector) Fixed(ctx context.Context) Batch {
	d.mu.Lock()
	defer d.mu.Unlock()
	return detached(d.fixedLocked(ctx).batch)
}

func (d *windowsCollector) Fork() Collector {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.fixed == nil {
		d.fixed = &windowsFixed{}
	}
	if d.load == nil {
		d.load = &windowsLoadState{}
	}
	if d.lhm == nil {
		d.lhm = &windowsLHMCache{}
	}
	return &windowsCollector{fixed: d.fixed, load: d.load, lhm: d.lhm}
}

func (d *windowsCollector) fixedLocked(ctx context.Context) *windowsFixed {
	if d.fixed == nil {
		d.fixed = &windowsFixed{}
	}
	if d.load == nil {
		d.load = &windowsLoadState{}
	}
	if d.lhm == nil {
		d.lhm = &windowsLHMCache{}
	}
	f := d.fixed
	f.once.Do(func() {
		f.batch = Batch{Metrics: []Metric{}}
		f.netKinds = map[string]string{}
		add := func(id string, tags map[string]string, fields map[string]float64, exact map[string]string) {
			if tags == nil {
				tags = map[string]string{}
			}
			f.batch.Metrics = append(f.batch.Metrics, Metric{Name: id, Tags: tags, Fields: fields, Exact: exact, Timestamp: time.Now().Unix()})
		}
		var tags map[string]string
		var fields map[string]float64
		tags, fields = map[string]string{}, map[string]float64{}
		if info, err := cpu.InfoWithContext(ctx); err == nil && len(info) > 0 {
			if model := strings.TrimSpace(info[0].ModelName); model != "" {
				tags["model"] = model
			}
		}
		for field, logical := range map[string]bool{"physical_cores": false, "logical_cores": true} {
			if count, err := cpu.CountsWithContext(ctx, logical); err == nil && count > 0 {
				fields[field] = float64(count)
			}
		}
		if len(tags) > 0 || len(fields) > 0 {
			add("windows_cpu", tags, fields, nil)
		}

		if gpus, err := windowsVideoControllers(ctx); err == nil {
			for _, gpu := range gpus {
				f.gpus = append(f.gpus, gpu)
				add("windows_gpu", map[string]string{"device": gpu.device, "model": gpu.model}, map[string]float64{"present": 1}, nil)
			}
		}

		// WMI owns physical-disk identity and capacity. Sensor helpers must not
		// alter the inventory, otherwise a missing low-level driver could make a
		// real disk disappear from the Client.
		if disks, err := windowsPhysicalDisks(ctx); err == nil {
			health := map[string]float64{}
			for _, disk := range disks {
				if disk.size == 0 {
					continue
				}
				tags := map[string]string{"device": disk.device}
				if disk.model != "" {
					tags["model"] = disk.model
				}
				fields := map[string]float64{"total": float64(disk.size), "present": 1}
				if healthy, ok := diskHealthValue(disk.health); ok {
					fields["smart_healthy"] = healthy
					health[disk.device] = healthy
				}
				add("windows_disk", tags, fields, map[string]string{"total": strconv.FormatUint(disk.size, 10)})
			}
			d.diskHealth, d.diskHealthAt = health, time.Now()
		}
		if adapters, err := windowsNetworkAdapters(ctx); err == nil {
			kinds := windowsAdapterKinds(adapters)
			if interfaces, err := net.Interfaces(); err == nil {
				for _, iface := range interfaces {
					kind, ok := adapterKindFor(kinds, iface.Name)
					if !ok {
						continue
					}
					f.netKinds[iface.Name] = kind
					add("windows_network", map[string]string{"interface": iface.Name, "type": kind}, map[string]float64{"present": 1}, nil)
				}
			}
		}

		if status, err := windowsPowerStatus(); err == nil {
			if _, ok := batteryFields(status); ok {
				add("windows_battery", map[string]string{}, map[string]float64{"present": 1}, nil)
			}
		}

		if systemTags := windowsSystemTags(); len(systemTags) > 0 {
			add("windows_system", systemTags, map[string]float64{"present": 1}, nil)
		}
	})
	return f
}

func (d *windowsCollector) Gather(ctx context.Context, ids []string) (Batch, map[string]string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	fixed := d.fixedLocked(ctx)
	out := Batch{Metrics: []Metric{}}
	reasons := map[string]string{}
	add := func(id string, tags map[string]string, fields map[string]float64, exact map[string]string) bool {
		if tags == nil {
			tags = map[string]string{}
		}
		if len(fields) == 0 && len(exact) == 0 {
			return false
		}
		out.Metrics = append(out.Metrics, Metric{Name: id, Tags: tags, Fields: fields, Exact: exact, Timestamp: time.Now().Unix()})
		return true
	}
	for _, id := range ids {
		if ctx.Err() != nil {
			reasons[id] = gatherReason(ctx.Err())
			continue
		}
		switch id {
		case "windows_cpu":
			cached := fixedMetric(fixed.batch, id, "", "")
			fields := metricFields(cached)
			lhm, lhmErr := d.lhm.read(ctx)
			if lhmErr != nil {
				reasons[id] = lhmReason(lhmErr)
			} else if reason := lhmDataReason(lhm, id); reason != "" {
				reasons[id] = reason
			}
			if lhm.CPU != nil && *lhm.CPU > 0 && *lhm.CPU < 150 {
				fields["temperature_celsius"] = *lhm.CPU
			} else if value, ok := d.cpuTemperatureLocked(ctx); ok {
				fields["temperature_celsius"] = value
			}
			if load1, load5, load15, ok := d.loadLocked(ctx, fields["logical_cores"]); ok {
				fields["load1"], fields["load5"], fields["load15"] = load1, load5, load15
			}
			if !add(id, cached.Tags, fields, nil) {
				reasons[id] = "unavailable"
			}
		case "windows_gpu":
			if len(fixed.gpus) == 0 {
				reasons[id] = "unavailable"
				continue
			}
			lhm, lhmErr := d.lhm.read(ctx)
			if lhmErr != nil {
				reasons[id] = lhmReason(lhmErr)
			}
			adapters, _ := windowsGPUAdapters(ctx)
			usage, _ := windowsGPUUsage(ctx)
			fieldsByDevice := gpuFieldsByDevice(fixed.gpus, adapters, usage)
			for device, fields := range lhmGPUFields(fixed.gpus, lhm.GPUs) {
				if fieldsByDevice[device] == nil {
					fieldsByDevice[device] = map[string]float64{}
				}
				fieldsByDevice[device]["temperature_celsius"] = fields["temperature_celsius"]
			}
			count := 0
			for _, m := range fixed.batch.Metrics {
				if m.Name != id || len(fieldsByDevice[m.Tags["device"]]) == 0 {
					continue
				}
				if add(id, m.Tags, fieldsByDevice[m.Tags["device"]], nil) {
					count++
				}
			}
			if count == 0 && reasons[id] == "" {
				reasons[id] = "unavailable"
			}
		case "windows_disk":
			health := d.diskHealthLocked(ctx)
			lhm, lhmErr := d.lhm.read(ctx)
			if lhmErr != nil {
				reasons[id] = lhmReason(lhmErr)
			} else if reason := lhmDataReason(lhm, id); reason != "" {
				reasons[id] = reason
			}
			lhmDisks := lhmDiskByDevice(lhm.Disks)
			count := 0
			for _, m := range fixed.batch.Metrics {
				if m.Name != id {
					continue
				}
				fields := metricFields(m)
				if value, ok := health[m.Tags["device"]]; ok {
					fields["smart_healthy"] = value
				}
				if disk, ok := lhmDisks[strings.ToUpper(m.Tags["device"])]; ok {
					if disk.Temperature != nil && *disk.Temperature > 0 && *disk.Temperature < 150 {
						fields["temperature_celsius"] = *disk.Temperature
					}
					if disk.Healthy != nil {
						if *disk.Healthy {
							fields["smart_healthy"] = 1
						} else {
							fields["smart_healthy"] = 0
						}
					}
				}
				if add(id, m.Tags, fields, detached(m.Exact)) {
					count++
				}
			}
			if count == 0 {
				reasons[id] = "unavailable"
			}
		case "windows_disk_usage":
			usage, err := d.diskUsageLocked(ctx)
			if err != nil {
				reasons[id] = windowsReason(err)
				continue
			}
			for device, used := range usage {
				add(id, map[string]string{"device": device}, map[string]float64{"used": float64(used)}, map[string]string{"used": strconv.FormatUint(used, 10)})
			}
			if len(usage) == 0 {
				reasons[id] = "unavailable"
			}
		case "windows_network":
			interfaces, err := net.Interfaces()
			if err != nil {
				reasons[id] = gatherReason(err)
				continue
			}
			count := 0
			for _, iface := range interfaces {
				kind, ok := fixed.netKinds[iface.Name]
				if !ok {
					continue
				}
				addresses, err := iface.Addrs()
				if err != nil {
					reasons[id] = "partial_data"
					continue
				}
				ips := []string{}
				for _, address := range addresses {
					ip, _, err := net.ParseCIDR(address.String())
					if err == nil && ip.To4() != nil && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() {
						ips = append(ips, ip.To4().String())
					}
				}
				connected := 0.0
				if iface.Flags&net.FlagUp != 0 {
					connected = 1
				}
				add(id, map[string]string{"interface": iface.Name, "type": kind, "ipv4": strings.Join(ips, ",")}, map[string]float64{"connected": connected}, nil)
				count++
			}
			if count == 0 && reasons[id] == "" {
				reasons[id] = "unavailable"
			}
		case "windows_public_ip":
			ip, at, err := d.publicIPLocked(ctx)
			if errors.Is(err, errPublicIPPending) {
				continue
			}
			if err != nil {
				reasons[id] = gatherReason(err)
				continue
			}
			out.Metrics = append(out.Metrics, Metric{Name: id, Tags: map[string]string{"ipv4": ip}, Fields: map[string]float64{"present": 1}, Timestamp: at.Unix()})
		case "windows_battery":
			status, err := windowsPowerStatus()
			if err != nil {
				reasons[id] = windowsReason(err)
				continue
			}
			fields, ok := batteryFields(status)
			if !ok {
				reasons[id] = "unavailable"
				continue
			}
			add(id, map[string]string{}, fields, nil)
		case "windows_system":
			cached := fixedMetric(fixed.batch, id, "", "")
			if !add(id, cached.Tags, metricFields(cached), nil) {
				reasons[id] = "unavailable"
			}
		default:
			reasons[id] = "unsupported"
		}
	}
	return out, reasons
}

func metricFields(m Metric) map[string]float64 {
	out := map[string]float64{}
	for key, value := range m.Fields {
		out[key] = value
	}
	return out
}

func (d *windowsCollector) cpuTemperatureLocked(ctx context.Context) (float64, bool) {
	if !d.cpuTempFailedAt.IsZero() && time.Since(d.cpuTempFailedAt) < 30*time.Second {
		return 0, false
	}
	value, found, err := windowsLHMCPUTemperature(ctx)
	if err != nil {
		// LibreHardwareMonitor may not be installed or its WMI namespace may be
		// disabled. Do not retry the failing namespace on every interval.
		d.cpuTempFailedAt = time.Now()
		return 0, false
	}
	if !found {
		return 0, false
	}
	d.cpuTempFailedAt = time.Time{}
	return value, true
}

func (d *windowsCollector) loadLocked(ctx context.Context, cores float64) (float64, float64, float64, bool) {
	if cores <= 0 {
		return 0, 0, 0, false
	}
	busy, queue, err := windowsRunnable(ctx)
	if err != nil {
		return 0, 0, 0, false
	}
	// windows_exporter has no load-average counter; its system collector
	// exposes Processor Queue Length. Combine that queue with the runnable
	// share of logical CPUs and smooth it with the Unix load EWMA constants.
	runnable := queue + cores*busy/100
	load1, load5, load15 := d.load.observe(runnable, time.Now())
	return load1, load5, load15, true
}

func (d *windowsCollector) diskHealthLocked(ctx context.Context) map[string]float64 {
	if !d.diskHealthAt.IsZero() && time.Since(d.diskHealthAt) < 5*time.Minute {
		return d.diskHealth
	}
	disks, err := windowsPhysicalDisks(ctx)
	if err != nil {
		return d.diskHealth
	}
	health := map[string]float64{}
	for _, disk := range disks {
		if value, ok := diskHealthValue(disk.health); ok {
			health[disk.device] = value
		}
	}
	d.diskHealth, d.diskHealthAt = health, time.Now()
	return health
}

func (d *windowsCollector) diskUsageLocked(ctx context.Context) (map[string]uint64, error) {
	if d.partitionsAt.IsZero() || time.Since(d.partitionsAt) >= 5*time.Minute {
		partitions, err := windowsPartitions(ctx)
		if err != nil {
			if len(d.partitions) == 0 {
				return nil, err
			}
		} else {
			d.partitions, d.partitionsAt = partitions, time.Now()
		}
	}
	volumes, err := windowsLogicalDisks(ctx)
	if err != nil {
		return nil, err
	}
	return windowsDiskUsage(d.partitions, volumes), nil
}

func (d *windowsCollector) publicIPLocked(context.Context) (string, time.Time, error) {
	if d.publicIP != "" && time.Since(d.publicIPAt) <= 5*time.Minute {
		return d.publicIP, d.publicIPAt, nil
	}
	if d.publicIPFetching {
		return "", time.Time{}, errPublicIPPending
	}
	d.publicIPFetching = true
	fetch := d.fetchIP
	if fetch == nil {
		fetch = publicIPv4
	}
	go func() {
		ip, err := fetch(context.Background())
		d.mu.Lock()
		defer d.mu.Unlock()
		d.publicIPFetching = false
		if err != nil {
			d.publicIP = ""
			return
		}
		d.publicIP, d.publicIPAt = ip, time.Now()
	}()
	return "", time.Time{}, errPublicIPPending
}
