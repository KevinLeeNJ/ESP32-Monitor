//go:build linux

package collector

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// nvtop's snapshot identifies cards by name only. Repeated names cannot be
// associated reliably across samples and are therefore omitted.
func parseLinuxNVTop(data []byte) []Metric {
	var devices []struct {
		Name string `json:"device_name"`
		Util string `json:"gpu_util"`
		Temp string `json:"temp"`
	}
	if json.Unmarshal(data, &devices) != nil {
		return nil
	}
	counts := map[string]int{}
	for _, d := range devices {
		counts[strings.ToLower(strings.TrimSpace(d.Name))]++
	}
	var metrics []Metric
	for _, d := range devices {
		name := strings.TrimSpace(d.Name)
		key := strings.ToLower(name)
		if name == "" || counts[key] != 1 {
			continue
		}
		fields := map[string]float64{}
		if strings.HasSuffix(d.Util, "%") {
			if v, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(d.Util, "%")), 64); err == nil && !math.IsNaN(v) && v >= 0 && v <= 100 {
				fields["usage_percent"] = v
			}
		}
		temp := strings.TrimSpace(d.Temp)
		if strings.HasSuffix(temp, "C") || strings.HasSuffix(temp, "F") {
			fahrenheit := strings.HasSuffix(temp, "F")
			if v, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(temp, "C"), "F")), 64); err == nil {
				if fahrenheit {
					v = (v - 32) * 5 / 9
				}
				if !math.IsNaN(v) && v > -50 && v < 200 {
					fields["temperature_celsius"] = v
				}
			}
		}
		if len(fields) == 0 {
			continue
		}
		hash := sha256.Sum256([]byte(key))
		metrics = append(metrics, Metric{Tags: map[string]string{"device": fmt.Sprintf("nvtop-%x", hash[:16]), "model": name}, Fields: fields})
	}
	return metrics
}
