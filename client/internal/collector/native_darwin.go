//go:build darwin

package collector

import (
	"context"
	"net"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

type darwinCollector struct {
	mu               sync.Mutex
	fixed            *darwinFixed
	smart            []Metric
	smartAt          time.Time
	publicIP         string
	publicIPAt       time.Time
	publicIPFetching bool
}

func newNativeCollector() Collector { return &darwinCollector{} }

func darwinCommand(ctx context.Context, path string, args ...string) (string, error) {
	b, err := exec.CommandContext(ctx, path, args...).Output()
	return strings.TrimSpace(string(b)), err
}
func sysctlValue(ctx context.Context, key string) string {
	s, err := darwinCommand(ctx, "/usr/sbin/sysctl", "-n", key)
	if err != nil {
		return ""
	}
	return s
}
func numeric(s string) (float64, bool) {
	v, e := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return v, e == nil && v >= 0
}
func (d *darwinCollector) Gather(ctx context.Context, ids []string) (Batch, map[string]string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	fixed := d.fixedLocked(ctx)
	out := Batch{Metrics: []Metric{}}
	reasons := map[string]string{}
	add := func(id string, tags map[string]string, fields map[string]float64) {
		if len(fields) == 0 {
			reasons[id] = "unavailable"
			return
		}
		out.Metrics = append(out.Metrics, Metric{Name: id, Tags: tags, Fields: fields, Timestamp: time.Now().Unix()})
	}
	var cpuSensors, gpuSensors map[string]float64
	brand := fixed.brand
	if slices.Contains(ids, "darwin_cpu") || slices.Contains(ids, "darwin_gpu") {
		cpuSensors, gpuSensors = readSMC(brand)
	}
	for _, id := range ids {
		if ctx.Err() != nil {
			reasons[id] = gatherReason(ctx.Err())
			continue
		}
		switch id {
		case "darwin_cpu":
			cached := fixedMetric(fixed.batch, id, "", "")
			tags, fields := cached.Tags, cached.Fields
			if avg, ok := sensorAverage(cpuSensors); ok {
				fields["temperature_celsius"] = avg
			}
			add(id, tags, fields)
			if len(fields) > 0 && (tags["model"] == "" || fields["physical_cores"] == 0) {
				reasons[id] = "partial_data"
			}
		case "darwin_gpu":
			gpu, err := readGPU(ctx)
			if err != nil {
				reasons[id] = gatherReason(err)
			}
			if avg, ok := sensorAverage(gpuSensors); ok {
				if len(gpu) == 1 {
					gpu[0].Fields["temperature_celsius"] = avg
				}
			}
			out.Metrics = append(out.Metrics, gpu...)
			if len(gpu) == 0 && len(gpuSensors) == 0 && reasons[id] == "" {
				reasons[id] = "unavailable"
			}
		case "darwin_disk":
			if d.smartAt.IsZero() || time.Since(d.smartAt) >= 5*time.Minute {
				disks, err := readDisksFor(ctx, fixed.devices, false)
				if err != nil {
					reasons[id] = gatherReason(err)
				} else {
					d.smart = disks
					d.smartAt = time.Now()
				}
			}
			if len(d.smart) == 0 {
				reasons[id] = "unavailable"
			}
			out.Metrics = append(out.Metrics, detached(d.smart)...)
		case "darwin_disk_usage":
			value, err := darwinCommand(ctx, "/usr/sbin/diskutil", "apfs", "list", "-plist")
			if err != nil {
				reasons[id] = gatherReason(err)
				continue
			}
			parsed, err := decodePlist(value)
			if err != nil {
				reasons[id] = "invalid_data"
				continue
			}
			count := 0
			for device, used := range apfsDiskUsage(parsed, fixed.partitions) {
				total := number(fixedMetric(fixed.batch, "darwin_disk", "device", device), "total")
				if total == nil || used > *total {
					continue
				}
				out.Metrics = append(out.Metrics, Metric{Name: id, Tags: map[string]string{"device": device}, Fields: map[string]float64{"used": float64(used)}, Exact: map[string]string{"used": strconv.FormatUint(used, 10)}, Timestamp: time.Now().Unix()})
				count++
			}
			if count == 0 {
				reasons[id] = "unavailable"
			}

		case "darwin_public_ip":
			if d.publicIP == "" || time.Since(d.publicIPAt) > 5*time.Minute {
				if !d.publicIPFetching {
					d.publicIPFetching = true
					go func() {
						ip, err := publicIPv4(context.Background())
						d.mu.Lock()
						defer d.mu.Unlock()
						d.publicIPFetching = false
						if err != nil {
							d.publicIP = ""
							return
						}
						d.publicIP, d.publicIPAt = ip, time.Now()
					}()
				}
				continue
			}
			out.Metrics = append(out.Metrics, Metric{Name: id, Tags: map[string]string{"ipv4": d.publicIP}, Fields: map[string]float64{"present": 1}, Timestamp: d.publicIPAt.Unix()})
		case "darwin_memory":
			fields := map[string]float64{}
			if s, err := darwinCommand(ctx, "/usr/bin/vm_stat"); err == nil {
				fields = parseVMStat(s)
			}
			if v, ok := numeric(sysctlValue(ctx, "kern.memorystatus_vm_pressure_level")); ok {
				fields["pressure_level"] = v
			}
			add(id, map[string]string{}, fields)
			if len(fields) > 0 {
				if _, ok := fields["pressure_level"]; !ok {
					reasons[id] = "partial_data"
				}
			}
		case "darwin_network":
			interfaces, err := net.Interfaces()
			if err != nil {
				reasons[id] = "unavailable"
				continue
			}
			for _, iface := range interfaces {
				kind, ok := fixed.kinds[iface.Name]
				if !ok {
					continue
				}
				addresses, err := iface.Addrs()
				if err != nil {
					reasons[id] = "partial_data"
					continue
				}
				ips := []string{}
				for _, a := range addresses {
					ip, _, e := net.ParseCIDR(a.String())
					if e == nil && ip.To4() != nil && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() {
						ips = append(ips, ip.To4().String())
					}
				}
				fields := map[string]float64{"present": 1}
				if output, err := darwinCommand(ctx, "/sbin/ifconfig", iface.Name); err == nil {
					if connected, ok := parseDarwinLinkStatus(output); ok {
						fields["connected"] = connected
					}
				}
				add(id, map[string]string{"interface": iface.Name, "type": kind, "ipv4": strings.Join(ips, ",")}, fields)
			}

		case "darwin_battery":
			fields := map[string]float64{}
			if s, err := darwinCommand(ctx, "/usr/bin/pmset", "-g", "batt"); err == nil {
				fields = parseBattery(s)
			}
			add(id, map[string]string{}, fields)
		case "darwin_system":
			cached := fixedMetric(fixed.batch, id, "", "")
			if len(cached.Tags) > 0 {
				out.Metrics = append(out.Metrics, cached)
			} else {
				reasons[id] = "unavailable"
			}

		default:
			reasons[id] = "unsupported"
		}
	}
	return out, reasons
}

func parseDarwinLinkStatus(output string) (float64, bool) {
	for _, line := range strings.Split(output, "\n") {
		switch strings.TrimSpace(line) {
		case "status: active":
			return 1, true
		case "status: inactive":
			return 0, true
		}
	}
	return 0, false
}

var pageSizePattern = regexp.MustCompile(`page size of ([0-9]+) bytes`)

func parseVMStat(s string) map[string]float64 {
	fields := map[string]float64{}
	match := pageSizePattern.FindStringSubmatch(s)
	if len(match) != 2 {
		return fields
	}
	page, ok := numeric(match[1])
	if !ok || page == 0 {
		return fields
	}
	vals := map[string]float64{}
	for _, line := range strings.Split(s, "\n") {
		p := strings.SplitN(line, ":", 2)
		if len(p) == 2 {
			if v, ok := numeric(strings.TrimSuffix(strings.TrimSpace(p[1]), ".")); ok {
				vals[strings.TrimSpace(p[0])] = v * page
			}
		}
	}
	keys := []string{"Pages active", "Pages inactive", "Pages speculative", "Pages wired down", "Pages occupied by compressor", "Pages purgeable", "File-backed pages"}
	for _, k := range keys {
		if _, ok := vals[k]; !ok {
			return fields
		}
	}
	wired, compressed := vals[keys[3]], vals[keys[4]]
	used := vals[keys[0]] + vals[keys[1]] + vals[keys[2]] + wired + compressed - vals[keys[5]] - vals[keys[6]]
	app := used - wired - compressed
	if app >= 0 {
		fields["app_bytes"] = app
		fields["wired_bytes"] = wired
		fields["compressed_bytes"] = compressed
	}
	return fields
}

var batteryPattern = regexp.MustCompile(`(?m)^\s*-InternalBattery-[^\n]*\s([0-9]+)%;\s*([^;]+);`)

func parseBattery(s string) map[string]float64 {
	fields := map[string]float64{}
	m := batteryPattern.FindStringSubmatch(s)
	if len(m) != 3 {
		return fields
	}
	if v, ok := numeric(m[1]); ok && v <= 100 {
		fields["charge_percent"] = v
	}
	switch strings.TrimSpace(m[2]) {
	case "charging":
		fields["charging"] = 1
	case "discharging", "charged", "finishing charge", "not charging":
		fields["charging"] = 0
	}
	return fields
}

func parseNetworkPorts(s string) map[string]string {
	result := map[string]string{}
	kind := ""
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Hardware Port: ") {
			name := strings.ToLower(strings.TrimPrefix(line, "Hardware Port: "))
			kind = ""
			if strings.Contains(name, "wi-fi") || strings.Contains(name, "airport") {
				kind = "wifi"
			} else if strings.Contains(name, "ethernet") {
				kind = "lan"
			}
		} else if strings.HasPrefix(line, "Device: ") && kind != "" {
			result[strings.TrimSpace(strings.TrimPrefix(line, "Device: "))] = kind
		}
	}
	return result
}

func sensorAverage(values map[string]float64) (float64, bool) {
	if len(values) == 0 {
		return 0, false
	}
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values)), true
}
