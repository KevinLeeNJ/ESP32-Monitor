//go:build darwin

package collector

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

type darwinCollector struct {
	publicIP   string
	publicIPAt time.Time
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
	var fans []float64
	brand := ""
	if slices.Contains(ids, "darwin_cpu") || slices.Contains(ids, "darwin_gpu") || slices.Contains(ids, "darwin_fan") {
		brand = sysctlValue(ctx, "machdep.cpu.brand_string")
		cpuSensors, gpuSensors, fans = readSMC(brand)
	}
	for _, id := range ids {
		if ctx.Err() != nil {
			reasons[id] = gatherReason(ctx.Err())
			continue
		}
		switch id {
		case "darwin_cpu":
			tags := map[string]string{}
			if brand != "" {
				tags["model"] = brand
			}
			fields := map[string]float64{}
			for field, key := range map[string]string{"physical_cores": "hw.physicalcpu", "logical_cores": "hw.logicalcpu", "performance_cores": "hw.perflevel0.physicalcpu", "efficiency_cores": "hw.perflevel1.physicalcpu"} {
				if v, ok := numeric(sysctlValue(ctx, key)); ok {
					fields[field] = v
				}
			}
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
			out.Metrics = append(out.Metrics, gpu...)
			if avg, ok := sensorAverage(gpuSensors); ok {
				add(id, map[string]string{"device": "soc"}, map[string]float64{"temperature_celsius": avg})
			}
			if len(gpu) == 0 && len(gpuSensors) == 0 && reasons[id] == "" {
				reasons[id] = "unavailable"
			}
		case "darwin_fan":
			for index, rpm := range fans {
				add(id, map[string]string{"fan": fmt.Sprintf("%d", index)}, map[string]float64{"speed_rpm": rpm})
			}
			if len(fans) == 0 {
				reasons[id] = "unavailable"
			}
		case "darwin_disk":
			disks, err := readDisks(ctx)
			if err != nil {
				reasons[id] = gatherReason(err)
			} else if len(disks) == 0 {
				reasons[id] = "unavailable"
			}
			out.Metrics = append(out.Metrics, disks...)
		case "darwin_public_ip":
			if d.publicIP == "" || time.Since(d.publicIPAt) > 5*time.Minute {
				ip, err := publicIPv4(ctx)
				if err != nil {
					reasons[id] = gatherReason(err)
					d.publicIP = ""
				} else {
					d.publicIP = ip
					d.publicIPAt = time.Now()
				}
			}
			if d.publicIP != "" {
				add(id, map[string]string{"ipv4": d.publicIP}, map[string]float64{"present": 1})
			}
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
			ports, err := darwinCommand(ctx, "/usr/sbin/networksetup", "-listallhardwareports")
			interfaces, iferr := net.Interfaces()
			if err != nil || iferr != nil {
				reasons[id] = "unavailable"
				continue
			}
			kinds := parseNetworkPorts(ports)
			count := 0
			for _, iface := range interfaces {
				kind, ok := kinds[iface.Name]
				if !ok || iface.Flags&net.FlagUp == 0 {
					continue
				}
				addresses, err := iface.Addrs()
				if err != nil {
					continue
				}
				for _, address := range addresses {
					ip, _, err := net.ParseCIDR(address.String())
					if err != nil || ip.To4() == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
						continue
					}
					add(id, map[string]string{"interface": iface.Name, "type": kind, "ipv4": ip.String()}, map[string]float64{"connected": 1})
					count++
				}
			}
			if count == 0 {
				reasons[id] = "unavailable"
			}
		case "darwin_battery":
			fields := map[string]float64{}
			if s, err := darwinCommand(ctx, "/usr/bin/pmset", "-g", "batt"); err == nil {
				fields = parseBattery(s)
			}
			add(id, map[string]string{}, fields)
		case "darwin_system":
			tags := map[string]string{}
			if s := sysctlValue(ctx, "hw.model"); s != "" {
				tags["model_id"] = s
				if name := macProductName(s); name != "" {
					tags["product_name"] = name
				} else {
					tags["product_name"] = s
				}
			}
			if s, err := darwinCommand(ctx, "/usr/bin/sw_vers"); err == nil {
				name, version, build := parseSwVers(s)
				if name != "" {
					tags["os_name"] = name
				}
				if version != "" {
					tags["os_version"] = version
					if codename := darwinOSCodename(version); codename != "" {
						tags["os_codename"] = codename
					}
				}
				if build != "" {
					tags["os_build"] = build
				}
			}
			if len(tags) > 0 {
				add(id, tags, map[string]float64{"present": 1})
			} else {
				reasons[id] = "unavailable"
			}
		default:
			reasons[id] = "unsupported"
		}
	}
	return out, reasons
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
