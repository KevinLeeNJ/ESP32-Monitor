//go:build darwin

package collector

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func readGPU(ctx context.Context) ([]Metric, error) {
	s, e := darwinCommand(ctx, "/usr/sbin/ioreg", "-r", "-c", "IOAccelerator", "-a")
	if e != nil {
		return nil, e
	}
	v, e := decodePlist(s)
	if e != nil {
		return nil, e
	}
	list, _ := v.([]any)
	out := []Metric{}
	for index, item := range list {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		stats, _ := obj["PerformanceStatistics"].(map[string]any)
		if stats == nil {
			continue
		}
		fields := map[string]float64{}
		for _, k := range []string{"Device Utilization %", "GPU Activity(%)"} {
			if n, ok := stats[k].(float64); ok && n >= 0 {
				fields["usage_percent"] = min(100, n)
				break
			}
		}
		if n, ok := stats["Temperature(C)"].(float64); ok && n > 0 && n < 110 {
			fields["temperature_celsius"] = n
		}
		if len(fields) > 0 {
			class, _ := obj["IOClass"].(string)
			if class == "" {
				class = "gpu"
			}
			out = append(out, Metric{Name: "darwin_gpu", Tags: map[string]string{"device": class + "-" + strconv.Itoa(index)}, Fields: fields, Timestamp: time.Now().Unix()})
		}
	}
	return out, nil
}
func readDisks(ctx context.Context) ([]Metric, error) {
	s, e := darwinCommand(ctx, "/usr/sbin/diskutil", "list", "-plist", "physical")
	if e != nil {
		return nil, e
	}
	v, e := decodePlist(s)
	if e != nil {
		return nil, e
	}
	root, _ := v.(map[string]any)
	disks, _ := root["WholeDisks"].([]any)
	devices := []string{}
	for _, item := range disks {
		if device, ok := item.(string); ok {
			devices = append(devices, device)
		}
	}
	return readDisksFor(ctx, devices, true)
}

func readDisksFor(ctx context.Context, devices []string, fixed bool) ([]Metric, error) {
	out := []Metric{}

	for _, disk := range devices {
		if !strings.HasPrefix(disk, "disk") {
			continue
		}
		s, e := darwinCommand(ctx, "/usr/sbin/diskutil", "info", "-plist", disk)
		if e != nil {
			continue
		}
		v, e := decodePlist(s)
		if e != nil {
			continue
		}
		info, _ := v.(map[string]any)
		if info == nil {
			continue
		}
		tags := map[string]string{"device": disk}
		model := ""
		if fixed {
			model, _ = info["MediaName"].(string)
		}
		if fixed && model != "" {
			tags["model"] = model
		}
		fields := map[string]float64{}
		exact := map[string]string{}
		if fixed {
			if n, ok := info["TotalSize"].(float64); ok {
				fields["total"] = n
			}
		}
		if status, ok := info["SMARTStatus"].(string); ok {
			switch strings.ToLower(status) {
			case "verified":
				fields["smart_healthy"] = 1
			case "failing":
				fields["smart_healthy"] = 0
			}
		}
		if smart, ok := info["SMARTDeviceSpecificKeysMayVaryNotGuaranteed"].(map[string]any); ok {
			for k, name := range map[string]string{"PERCENTAGE_USED": "smart_percent_used", "AVAILABLE_SPARE": "smart_available_spare_percent", "AVAILABLE_SPARE_THRESHOLD": "smart_spare_threshold_percent", "MEDIA_ERRORS_0": "smart_media_errors", "POWER_ON_HOURS_0": "smart_power_on_hours", "UNSAFE_SHUTDOWNS_0": "smart_unsafe_shutdowns"} {
				if n, ok := smart[k].(float64); ok && n >= 0 {
					fields[name] = n
				} else if n, ok := smart[k].(json.Number); ok {
					exact[name] = n.String()
				}
			}
		}
		if fixed && model != "" {
			fields["present"] = 1
		}
		if len(fields) > 0 || len(exact) > 0 {
			out = append(out, Metric{Name: "darwin_disk", Tags: tags, Fields: fields, Exact: exact, Timestamp: time.Now().Unix()})
		}
	}
	return out, nil
}
func publicIPv4(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.mac-stats.com/ip", nil)
	if e != nil {
		return "", e
	}
	dialer := &net.Dialer{Timeout: 3 * time.Second}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, address string) (net.Conn, error) {
		return dialer.DialContext(ctx, "tcp4", address)
	}, ForceAttemptHTTP2: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, e := client.Do(req)
	if e != nil {
		return "", e
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", io.ErrUnexpectedEOF
	}
	b, e := io.ReadAll(io.LimitReader(resp.Body, 4097))
	if e != nil {
		return "", e
	}
	if len(b) > 4096 {
		return "", io.ErrUnexpectedEOF
	}
	return parsePublicIPv4(b)
}

func parsePublicIPv4(b []byte) (string, error) {
	var result struct {
		IPv4 string `json:"ipv4"`
	}
	if err := json.Unmarshal(b, &result); err != nil {
		return "", err
	}
	ip := net.ParseIP(strings.TrimSpace(result.IPv4))
	if ip == nil || ip.To4() == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() {
		return "", io.ErrUnexpectedEOF
	}
	return ip.To4().String(), nil
}

func parseSwVers(s string) (name, version, build string) {
	for _, line := range strings.Split(s, "\n") {
		key, value, _ := strings.Cut(line, ":")
		switch strings.TrimSpace(key) {
		case "ProductName":
			name = strings.TrimSpace(value)
		case "ProductVersion":
			version = strings.TrimSpace(value)
		case "BuildVersion":
			build = strings.TrimSpace(value)
		}
	}
	return name, version, build
}

// macOS marketing names keyed by major version, adapted from fastfetch
// (src/detection/os/os_apple.m, MIT license, (c) fastfetch-cli contributors).
var darwinOSCodenames = map[int]string{
	27: "Golden Gate", 26: "Tahoe", 16: "Tahoe", 15: "Sequoia", 14: "Sonoma", 13: "Ventura", 12: "Monterey", 11: "Big Sur",
}

func darwinOSCodename(version string) string {
	major, _, _ := strings.Cut(version, ".")
	n, err := strconv.Atoi(major)
	if err != nil {
		return ""
	}
	return darwinOSCodenames[n]
}
