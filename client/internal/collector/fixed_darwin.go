//go:build darwin

package collector

import (
	"context"
	"strconv"
	"sync"
	"time"
)

type darwinFixed struct {
	once    sync.Once
	batch   Batch
	brand   string
	kinds   map[string]string
	devices []string
}

func (d *darwinCollector) Fixed(ctx context.Context) Batch {
	d.mu.Lock()
	defer d.mu.Unlock()
	return detached(d.fixedLocked(ctx).batch)
}
func (d *darwinCollector) Fork() Collector {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.fixed == nil {
		d.fixed = &darwinFixed{}
	}
	return &darwinCollector{fixed: d.fixed}
}
func (d *darwinCollector) fixedLocked(ctx context.Context) *darwinFixed {
	if d.fixed == nil {
		d.fixed = &darwinFixed{}
	}
	f := d.fixed
	f.once.Do(func() {
		f.batch = Batch{Metrics: []Metric{}}
		add := func(id string, tags map[string]string, fields map[string]float64) {
			f.batch.Metrics = append(f.batch.Metrics, Metric{Name: id, Tags: tags, Fields: fields, Timestamp: time.Now().Unix()})
		}
		f.brand = sysctlValue(ctx, "machdep.cpu.brand_string")
		tags := map[string]string{}
		if f.brand != "" {
			tags["model"] = f.brand
		}
		fields := map[string]float64{}
		for field, key := range map[string]string{"physical_cores": "hw.physicalcpu", "logical_cores": "hw.logicalcpu", "performance_cores": "hw.perflevel0.physicalcpu", "efficiency_cores": "hw.perflevel1.physicalcpu"} {
			if v, ok := numeric(sysctlValue(ctx, key)); ok {
				fields[field] = v
			}
		}
		add("darwin_cpu", tags, fields)
		ports, _ := darwinCommand(ctx, "/usr/sbin/networksetup", "-listallhardwareports")
		f.kinds = parseNetworkPorts(ports)
		for iface, kind := range f.kinds {
			add("darwin_network", map[string]string{"interface": iface, "type": kind}, map[string]float64{"present": 1})
		}
		gpu, _ := readGPU(ctx)
		for _, m := range gpu {
			add("darwin_gpu", m.Tags, map[string]float64{"present": 1})
		}
		_, _, fans := readSMC(f.brand)
		for i := range fans {
			add("darwin_fan", map[string]string{"fan": strconv.Itoa(i)}, map[string]float64{"present": 1})
		}
		if value, e := darwinCommand(ctx, "/usr/bin/pmset", "-g", "batt"); e == nil && len(parseBattery(value)) > 0 {
			add("darwin_battery", map[string]string{}, map[string]float64{"present": 1})
		}
		disks, _ := readDisks(ctx)
		for _, m := range disks {
			f.devices = append(f.devices, m.Tags["device"])
			fields := detached(m.Fields)
			fields["present"] = 1
			add("darwin_disk", m.Tags, fields)
			f.batch.Metrics[len(f.batch.Metrics)-1].Exact = detached(m.Exact)
		}
		tags = map[string]string{}
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
			add("darwin_system", tags, map[string]float64{"present": 1})
		}
	})
	return f
}
