package simserver

import (
	"net/http"
	"net/http/httptest"
	"testing"

	v2 "esp32-monitor/client/internal/collector/uploadv2"
)

func inventoryProfile() v2.Profile {
	model := "Apple M4 Pro"
	build := "26100"
	medium := "wifi"
	filesystem := "NTFS"
	total := uint64(994662586752)
	mem := uint64(25769803776)
	p, l := uint16(8), uint16(14)
	return v2.Profile{
		Version: 2, IntervalSeconds: 10,
		Inventory: v2.Inventory{
			OS:          &v2.OSInfo{Family: "windows", Name: "Windows 11 Pro", Version: "24H2", Build: &build},
			Product:     &model,
			CPU:         &v2.CPUInfo{Model: &model, PhysicalCores: &p, LogicalCores: &l},
			MemoryTotal: &mem,
			Drives:      []v2.DriveInfo{{ID: 1, Model: &model, TotalBytes: &total}},
			Volumes:     []v2.VolumeInfo{{ID: 2, Filesystem: &filesystem, TotalBytes: &total}},
			Interfaces:  []v2.InterfaceInfo{{ID: 3, Medium: &medium}},
		},
	}
}

func TestDeviceProfileEndpoint(t *testing.T) {
	s := NewRegistration(nil)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	id, code := createDevice(t, ts, "测试机")
	_, body := request(t, ts, "POST", "/v2/collector/register", "", map[string]string{"code": code}, nil)
	token, _ := body["token"].(string)
	if token == "" {
		t.Fatalf("claim: %v", body)
	}

	if resp, body := request(t, ts, "GET", "/api/devices/"+id+"/profile", "", nil, nil); resp.StatusCode != http.StatusNotFound || body["error"] != "profile_required" {
		t.Fatalf("missing profile: %d %v", resp.StatusCode, body)
	}
	if resp, _ := request(t, ts, "GET", "/api/devices/deadbeef/profile", "", nil, nil); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown device: %d", resp.StatusCode)
	}
	if resp, _ := request(t, ts, "DELETE", "/api/devices/"+id+"/profile", "", nil, nil); resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("method guard: %d", resp.StatusCode)
	}

	if resp, _ := request(t, ts, "PUT", "/v2/collector/profile", token, inventoryProfile(), map[string]string{"If-None-Match": "*"}); resp.StatusCode != http.StatusOK {
		t.Fatalf("put profile: %d", resp.StatusCode)
	}
	resp, got := request(t, ts, "GET", "/api/devices/"+id+"/profile", "", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("profile: %d %v", resp.StatusCode, got)
	}
	if got["device_id"] != id || got["name"] != "测试机" || got["revision"] != float64(1) || got["interval_seconds"] != float64(10) {
		t.Fatalf("profile meta: %v", got)
	}
	inv, _ := got["inventory"].(map[string]any)
	if inv == nil {
		t.Fatalf("inventory missing: %v", got)
	}
	cpu, _ := inv["cpu"].(map[string]any)
	if cpu == nil || cpu["model"] != "Apple M4 Pro" {
		t.Fatalf("inventory cpu: %v", inv)
	}
	if inv["memory_total"] != "25769803776" {
		t.Fatalf("inventory memory_total: %v", inv)
	}
	drives, _ := inv["drives"].([]any)
	if len(drives) != 1 {
		t.Fatalf("inventory drives: %v", inv)
	}
}
