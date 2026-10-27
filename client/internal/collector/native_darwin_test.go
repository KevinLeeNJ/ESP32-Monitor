//go:build darwin

package collector

import "testing"

func TestDarwinMemoryAndBatteryParsing(t *testing.T) {
	vm := `Mach Virtual Memory Statistics: (page size of 4096 bytes)
Pages active: 100.
Pages inactive: 50.
Pages speculative: 10.
Pages wired down: 20.
Pages occupied by compressor: 30.
Pages purgeable: 5.
File-backed pages: 15.`
	f := parseVMStat(vm)
	if f["app_bytes"] != 140*4096 || f["wired_bytes"] != 20*4096 || f["compressed_bytes"] != 30*4096 {
		t.Fatalf("memory: %v", f)
	}
	if len(parseVMStat("invalid")) != 0 {
		t.Fatal("invalid memory generated data")
	}
	f = parseBattery("Now drawing from 'AC Power'\n -InternalBattery-0 (id=123)\t85%; discharging; 6:55 remaining present: true")
	if f["charge_percent"] != 85 || f["charging"] != 0 || len(f) != 2 {
		t.Fatalf("battery: %v", f)
	}
	if len(parseBattery("Now drawing from 'AC Power'")) != 0 {
		t.Fatal("battery invented on desktop")
	}
}

func TestNetworkPortClassification(t *testing.T) {
	p := parseNetworkPorts("Hardware Port: Wi-Fi\nDevice: en0\n\nHardware Port: Thunderbolt Ethernet\nDevice: en8\n")
	if p["en0"] != "wifi" || p["en8"] != "lan" {
		t.Fatalf("ports: %v", p)
	}
}

func TestPlistGPUAndDiskParsing(t *testing.T) {
	input := `<?xml version="1.0"?><plist version="1.0"><array><dict><key>IOClass</key><string>AGX</string><key>PerformanceStatistics</key><dict><key>Device Utilization %</key><integer>37</integer><key>other</key><data>AA==</data></dict></dict></array></plist>`
	v, err := decodePlist(input)
	if err != nil {
		t.Fatal(err)
	}
	a, ok := v.([]any)
	if !ok || len(a) != 1 {
		t.Fatalf("plist: %#v", v)
	}
	d := a[0].(map[string]any)
	stats := d["PerformanceStatistics"].(map[string]any)
	if stats["Device Utilization %"] != float64(37) {
		t.Fatal(stats)
	}
	if _, err := decodePlist(`<plist><dict><key>a</key><integer>broken</integer></dict></plist>`); err == nil {
		t.Fatal("invalid plist accepted")
	}
}

func TestParsePublicIPv4(t *testing.T) {
	if ip, err := parsePublicIPv4([]byte(`{"ipv4":"8.8.8.8","country":"US"}`)); err != nil || ip != "8.8.8.8" {
		t.Fatalf("valid: %q %v", ip, err)
	}
	for _, value := range []string{`{"ipv4":"192.168.1.1"}`, `{"ipv4":"::1"}`, `{"ipv6":"::1"}`, `broken`} {
		if _, err := parsePublicIPv4([]byte(value)); err == nil {
			t.Fatalf("accepted %s", value)
		}
	}
}
