//go:build linux

package collector

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
)

const udisksName = "org.freedesktop.UDisks2"

type udisksObjects map[dbus.ObjectPath]map[string]map[string]dbus.Variant

// Read the daemon's cached, recently observed health without invoking a
// privileged SMART refresh or accessing a raw block device.
func linuxUDisksHealth(ctx context.Context) map[string]float64 {
	bus, err := dbus.SystemBus()
	if err != nil {
		return nil
	}
	var objects udisksObjects
	if err := bus.Object(udisksName, "/org/freedesktop/UDisks2").CallWithContext(ctx, "org.freedesktop.DBus.ObjectManager.GetManagedObjects", 0).Store(&objects); err != nil {
		return nil
	}
	return parseUDisksHealth(objects, time.Now())
}

func parseUDisksHealth(objects udisksObjects, now time.Time) map[string]float64 {
	out := map[string]float64{}
	for _, interfaces := range objects {
		block := interfaces[udisksName+".Block"]
		device, ok := block["Device"].Value().([]byte)
		if !ok {
			continue
		}
		path := strings.TrimRight(string(device), "\x00")
		if filepath.Dir(path) != "/dev" {
			continue
		}
		drive, ok := block["Drive"].Value().(dbus.ObjectPath)
		if !ok || drive == "/" {
			continue
		}
		driveInterfaces := objects[drive]
		for _, iface := range []string{udisksName + ".NVMe.Controller", udisksName + ".Drive.Ata"} {
			props, exists := driveInterfaces[iface]
			if !exists {
				continue
			}
			updated, ok := props["SmartUpdated"].Value().(uint64)
			if !ok || updated == 0 || updated > uint64(now.Unix()) || now.Unix()-int64(updated) > int64((30*time.Minute).Seconds()) {
				continue
			}
			if strings.HasSuffix(iface, ".NVMe.Controller") {
				warnings, ok := props["SmartCriticalWarning"].Value().([]string)
				if !ok {
					continue
				}
				if len(warnings) == 0 {
					out[filepath.Base(path)] = 1
				} else {
					out[filepath.Base(path)] = 0
				}
			} else {
				supported, _ := props["SmartSupported"].Value().(bool)
				enabled, _ := props["SmartEnabled"].Value().(bool)
				failing, ok := props["SmartFailing"].Value().(bool)
				if !supported || !enabled || !ok {
					continue
				}
				if failing {
					out[filepath.Base(path)] = 0
				} else {
					out[filepath.Base(path)] = 1
				}
			}
			break
		}
	}
	return out
}
