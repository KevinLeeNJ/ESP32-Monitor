//go:build linux

package collector

import "testing"

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
