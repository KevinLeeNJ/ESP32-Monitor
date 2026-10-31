//go:build linux

package collector

import (
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

func TestUDisksHealth(t *testing.T) {
	now := time.Unix(200000, 0)
	drive := dbus.ObjectPath("/drive/test")
	objects := udisksObjects{
		"/block/disk": {udisksName + ".Block": {"Device": dbus.MakeVariant([]byte("/dev/nvme0n1\x00")), "Drive": dbus.MakeVariant(drive)}},
		drive:         {udisksName + ".NVMe.Controller": {"SmartUpdated": dbus.MakeVariant(uint64(now.Unix() - 60)), "SmartCriticalWarning": dbus.MakeVariant([]string{})}},
	}
	health := parseUDisksHealth(objects, now)
	if v, ok := health["nvme0n1"]; !ok || v != 1 {
		t.Fatalf("healthy: %v", health)
	}
	props := objects[drive][udisksName+".NVMe.Controller"]
	props["SmartCriticalWarning"] = dbus.MakeVariant([]string{"degraded"})
	if v, ok := parseUDisksHealth(objects, now)["nvme0n1"]; !ok || v != 0 {
		t.Fatal("critical warning must report failure")
	}
	props["SmartUpdated"] = dbus.MakeVariant(uint64(now.Unix() - 3600))
	if _, ok := parseUDisksHealth(objects, now)["nvme0n1"]; ok {
		t.Fatal("stale SMART data accepted")
	}
	props["SmartUpdated"] = dbus.MakeVariant(uint64(0))
	if _, ok := parseUDisksHealth(objects, now)["nvme0n1"]; ok {
		t.Fatal("uninitialized SMART data accepted")
	}
	delete(objects[drive], udisksName+".NVMe.Controller")
	objects[drive][udisksName+".Drive.Ata"] = map[string]dbus.Variant{"SmartUpdated": dbus.MakeVariant(uint64(now.Unix())), "SmartSupported": dbus.MakeVariant(true), "SmartEnabled": dbus.MakeVariant(true), "SmartFailing": dbus.MakeVariant(false)}
	if v, ok := parseUDisksHealth(objects, now)["nvme0n1"]; !ok || v != 1 {
		t.Fatal("ATA healthy state missing")
	}
}
