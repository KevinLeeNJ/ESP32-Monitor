//go:build linux

package collector

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLinuxInventoryExcludesPartitionCounters(t *testing.T) {
	b := fixtureFixed()
	b.Metrics = append(b.Metrics, Metric{Name: "diskio", Tags: map[string]string{"name": "disk0p1"}})
	s := &v2State{IDs: map[string]uint16{}, Next: 1}
	if err := s.buildInventory(b); err != nil {
		t.Fatal(err)
	}
	if len(s.inventory.Drives) != 1 || s.IDs["drive:disk0p1"] != 0 {
		t.Fatalf("partition became a drive: %+v", s.inventory.Drives)
	}
}

func TestParseLinuxBattery(t *testing.T) {
	cases := []struct {
		name, input     string
		level, charging float64
		available       bool
	}{
		{"charging", "type: battery\npresent: yes\nstate: charging\npercentage: 42.5%\n", 42.5, 1, true},
		{"full", "type: battery\npresent: yes\nstate: fully-charged\npercentage: 100%\n", 100, 0, true},
		{"absent", "type: battery\npresent: no\nstate: charging\npercentage: 42%\n", 0, 0, false},
		{"wrong type", "type: line-power\npresent: yes\nstate: charging\npercentage: 42%\n", 0, 0, false},
		{"invalid percent", "type: battery\npresent: yes\nstate: charging\npercentage: 101%\n", 0, 0, false},
		{"unknown state", "type: battery\npresent: yes\nstate: unknown\npercentage: 42%\n", 0, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseLinuxBattery(tc.input)
			if (len(got) == 2) != tc.available {
				t.Fatalf("fields = %v", got)
			}
			if tc.available && (got["charge_percent"] != tc.level || got["charging"] != tc.charging) {
				t.Fatalf("fields = %v", got)
			}
		})
	}
}

func TestLinuxNVTopSnapshot(t *testing.T) {
	input := `[{"device_name":"NVIDIA GPU","gpu_util":"35%","temp":"58C"},{"device_name":"Intel GPU","gpu_util":"N/A","temp":"140F"},{"device_name":"Duplicate","gpu_util":"1%"},{"device_name":"Duplicate","gpu_util":"2%"},{"device_name":"Invalid","gpu_util":"101%","temp":"N/A"}]`
	got := parseLinuxNVTop([]byte(input))
	if len(got) != 2 || got[0].Fields["usage_percent"] != 35 || got[0].Fields["temperature_celsius"] != 58 || got[1].Fields["temperature_celsius"] != 60 {
		t.Fatalf("unexpected GPU metrics: %+v", got)
	}
	if _, ok := got[1].Fields["usage_percent"]; ok {
		t.Fatal("unavailable GPU utilization must be omitted")
	}
}

func TestLinuxDiskUsage(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "btrfs-id", "devices", "dev0"), 0700); err != nil {
		t.Fatal(err)
	}
	input := `{"blockdevices":[
	{"name":"sda","type":"disk","size":1000,"children":[{"name":"sda1","type":"part","size":300,"fstype":"ext4","uuid":"a","fsused":200},{"name":"sda2","type":"part","size":300,"fstype":"btrfs","uuid":"btrfs-id","fsused":100},{"name":"sda3","type":"part","size":100,"fstype":"swap","uuid":"swap","fsused":null}]},
	{"name":"sdb","type":"disk","size":1000,"children":[{"name":"sdb1","type":"part","size":500,"fstype":"ntfs","uuid":"b","fsused":null}]},
	{"name":"sdc","type":"disk","size":1000,"children":[{"name":"sdc1","type":"part","size":500,"fstype":"ext4","uuid":"c","fsused":0}]},
	{"name":"sdd","type":"disk","size":1000,"children":[{"name":"sdd1","type":"part","size":500,"children":[{"name":"dm-0","type":"crypt","size":500,"fstype":"ext4","uuid":"d","fsused":100}]}]}
	]}`
	got := linuxDiskUsage([]byte(input), root)
	if len(got) != 2 || got["sda"] != (linuxDiskStat{Used: 300, Total: 600}) || got["sdc"] != (linuxDiskStat{Used: 0, Total: 500}) {
		t.Fatalf("unexpected disk usage: %v", got)
	}
	if err := os.Mkdir(filepath.Join(root, "btrfs-id", "devices", "dev1"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, ok := linuxDiskUsage([]byte(input), root)["sda"]; ok {
		t.Fatal("multi-device btrfs must be omitted")
	}
}
