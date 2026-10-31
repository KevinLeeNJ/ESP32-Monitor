//go:build linux

package collector

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/sensors"
)

type linuxCollector struct {
	mu               sync.Mutex
	fixed            *linuxFixed
	publicIP         string
	publicIPAt       time.Time
	publicIPFetching bool
}
type linuxFixed struct {
	once  sync.Once
	batch Batch
}

func newNativeCollector() Collector { return &linuxCollector{} }
func (d *linuxCollector) Fork() Collector {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.fixed == nil {
		d.fixed = &linuxFixed{}
	}
	return &linuxCollector{fixed: d.fixed}
}
func (d *linuxCollector) Fixed(ctx context.Context) Batch {
	d.mu.Lock()
	defer d.mu.Unlock()
	return detached(d.fixedLocked(ctx))
}
func (d *linuxCollector) fixedLocked(ctx context.Context) Batch {
	if d.fixed == nil {
		d.fixed = &linuxFixed{}
	}
	d.fixed.once.Do(func() {
		b, _ := d.collect(ctx, []string{"linux_disk", "linux_network", "linux_gpu", "linux_battery", "linux_system"})
		d.fixed.batch = b
	})
	return d.fixed.batch
}
func linuxCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}
func (d *linuxCollector) Gather(ctx context.Context, ids []string) (Batch, map[string]string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.collect(ctx, ids)
}
func (d *linuxCollector) collect(ctx context.Context, ids []string) (Batch, map[string]string) {
	out := Batch{Metrics: []Metric{}}
	reasons := map[string]string{}
	add := func(id string, tags map[string]string, fields map[string]float64, exact map[string]string) {
		out.Metrics = append(out.Metrics, Metric{Name: id, Tags: tags, Fields: fields, Exact: exact, Timestamp: time.Now().Unix()})
	}
	for _, id := range ids {
		if ctx.Err() != nil {
			reasons[id] = gatherReason(ctx.Err())
			continue
		}
		before := len(out.Metrics)
		switch id {
		case "linux_cpu":
			if values, _ := sensors.TemperaturesWithContext(ctx); len(values) > 0 {
				for _, s := range values {
					key := strings.ToLower(s.SensorKey)
					if (strings.Contains(key, "package_id_0") || strings.Contains(key, "tctl") || strings.Contains(key, "tdie")) && s.Temperature > 0 {
						add(id, map[string]string{}, map[string]float64{"temperature_celsius": s.Temperature}, nil)
						break
					}
				}
			}
		case "linux_disk":
			health := linuxUDisksHealth(ctx)
			usage, arrays := linuxStorage("/proc/self/mountinfo", "/sys/class/block", "/sys/fs/btrfs")
			for _, disk := range linuxPhysicalDisks("/sys/class/block") {
				stat, ok := usage[disk.Name]
				if !ok || stat.Total == 0 {
					continue
				}
				fields := map[string]float64{"total": float64(stat.Total)}
				if healthy, ok := health[disk.Name]; ok {
					fields["smart_healthy"] = healthy
				}
				add(id, map[string]string{"device": disk.Name, "model": disk.Model}, fields, map[string]string{"total": strconv.FormatUint(stat.Total, 10)})
			}
			raidHealth := linuxMDHealth("/sys/block", health)
			for name, array := range arrays {
				fields := map[string]float64{"total": float64(array.Total)}
				if healthy, ok := raidHealth[name]; ok {
					fields["smart_healthy"] = healthy
				}
				add(id, map[string]string{"device": name, "model": name + " " + strings.ToUpper(array.Level)}, fields, map[string]string{"total": strconv.FormatUint(array.Total, 10)})
			}
		case "linux_network":
			if interfaces, err := net.Interfaces(); err == nil {
				for _, iface := range interfaces {
					base := filepath.Join("/sys/class/net", iface.Name)
					if _, err := os.Stat(filepath.Join(base, "device")); err != nil {
						continue
					}
					kind := "lan"
					if _, err := os.Stat(filepath.Join(base, "wireless")); err == nil {
						kind = "wifi"
					}
					ips := []string{}
					addresses, err := iface.Addrs()
					if err != nil {
						reasons[id] = "partial_data"
					}
					for _, a := range addresses {
						ip, _, err := net.ParseCIDR(a.String())
						if err == nil && ip.To4() != nil && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() {
							ips = append(ips, ip.To4().String())
						}
					}
					up := float64(0)
					if iface.Flags&net.FlagUp != 0 && iface.Flags&net.FlagRunning != 0 {
						up = 1
					}
					add(id, map[string]string{"interface": iface.Name, "type": kind, "ipv4": strings.Join(ips, ",")}, map[string]float64{"present": 1, "connected": up}, nil)
				}
			}
		case "linux_gpu":
			if data, err := linuxCommand(ctx, "nvtop", "--snapshot"); err == nil {
				for _, m := range parseLinuxNVTop(data) {
					add(id, m.Tags, m.Fields, nil)
				}
			} else {
				reasons[id] = gatherReason(err)
			}
		case "linux_disk_usage":
			usage, arrays := linuxStorage("/proc/self/mountinfo", "/sys/class/block", "/sys/fs/btrfs")
			for name, array := range arrays {
				if array.UsedKnown {
					usage[name] = linuxDiskStat{Used: array.Used, UsedKnown: true}
				}
			}
			for device, stat := range usage {
				if !stat.UsedKnown {
					continue
				}
				add(id, map[string]string{"device": device}, map[string]float64{"used": float64(stat.Used)}, map[string]string{"used": strconv.FormatUint(stat.Used, 10)})
			}
		case "linux_battery":
			if data, err := linuxCommand(ctx, "upower", "-i", "/org/freedesktop/UPower/devices/DisplayDevice"); err == nil {
				fields := parseLinuxBattery(string(data))
				if len(fields) == 2 {
					add(id, map[string]string{}, fields, nil)
				}
			}
		case "linux_system":
			if info, err := host.InfoWithContext(ctx); err == nil {
				tags := map[string]string{"os_name": info.Platform, "os_version": info.PlatformVersion, "os_build": info.KernelVersion}
				if product, err := os.ReadFile("/sys/class/dmi/id/product_name"); err == nil {
					tags["product_name"] = strings.TrimSpace(string(product))
				}
				add(id, tags, map[string]float64{"present": 1}, nil)
			}
		case "linux_public_ip":
			if d.publicIP == "" || time.Since(d.publicIPAt) > 5*time.Minute {
				if !d.publicIPFetching {
					d.publicIPFetching = true
					go func() {
						ip, err := publicIPv4(context.Background())
						d.mu.Lock()
						defer d.mu.Unlock()
						d.publicIPFetching = false
						if err == nil {
							d.publicIP, d.publicIPAt = ip, time.Now()
						}
					}()
				}
			} else {
				out.Metrics = append(out.Metrics, Metric{Name: id, Tags: map[string]string{"ipv4": d.publicIP}, Fields: map[string]float64{"present": 1}, Timestamp: d.publicIPAt.Unix()})
			}
		default:
			reasons[id] = "unsupported"
		}
		if len(out.Metrics) == before && reasons[id] == "" && id != "linux_public_ip" {
			reasons[id] = "unavailable"
		}
	}
	return out, reasons
}

func parseLinuxBattery(data string) map[string]float64 {
	fields := map[string]float64{}
	present := false
	battery := false
	for _, line := range strings.Split(data, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch key {
		case "type":
			battery = value == "battery"
		case "present":
			present = value == "yes"
		case "percentage":
			if v, err := strconv.ParseFloat(strings.TrimSuffix(value, "%"), 64); err == nil && v >= 0 && v <= 100 {
				fields["charge_percent"] = v
			}
		case "state":
			switch value {
			case "charging", "pending-charge":
				fields["charging"] = 1
			case "discharging", "fully-charged", "empty", "pending-discharge":
				fields["charging"] = 0
			}
		}
	}
	if !battery || !present {
		return nil
	}
	return fields
}
