//go:build windows

package collector

import (
	"context"
	"strings"
)

type windowsLMHSensor struct {
	Identifier string
	Name       string
	SensorType string
	Value      *float32
}

// windowsLHMCPUTemperature retains the WMI reader for compatibility with
// developer installations that explicitly enable LHM's WMI server. Production
// packages use the bundled .NET Framework helper instead (see lhm-helper).
func windowsLHMCPUTemperature(ctx context.Context) (float64, bool, error) {
	var sensors []windowsLMHSensor
	if err := windowsQuery(ctx, `root\LibreHardwareMonitor`, `SELECT Identifier,Name,SensorType,Value FROM Sensor`, &sensors); err != nil {
		return 0, false, err
	}
	value, ok := lhmCPUTemperature(sensors)
	return value, ok, nil
}

func lhmGPUFields(gpus []windowsGPUInfo, readings []windowsLHMGPU) map[string]map[string]float64 {
	out := map[string]map[string]float64{}
	for _, gpu := range gpus {
		matches := []windowsLHMGPU{}
		gpuClaims := 0
		for _, candidate := range gpus {
			if strings.EqualFold(strings.TrimSpace(candidate.model), strings.TrimSpace(gpu.model)) {
				gpuClaims++
			}
		}
		for _, reading := range readings {
			if reading.Temperature != nil && strings.EqualFold(strings.TrimSpace(reading.Model), strings.TrimSpace(gpu.model)) {
				matches = append(matches, reading)
			}
		}
		if gpuClaims == 1 && len(matches) == 1 && *matches[0].Temperature > 0 && *matches[0].Temperature < 150 {
			out[gpu.device] = map[string]float64{"temperature_celsius": *matches[0].Temperature}
		}
	}
	return out
}

func lhmDiskByDevice(disks []windowsLHMDisk) map[string]windowsLHMDisk {
	out := map[string]windowsLHMDisk{}
	for _, disk := range disks {
		if disk.Device != "" {
			out[strings.ToUpper(disk.Device)] = disk
		}
	}
	return out
}

func lhmCPUTemperature(sensors []windowsLMHSensor) (float64, bool) {
	preferredSum, preferredCount := 0.0, 0
	allSum, allCount := 0.0, 0
	for _, sensor := range sensors {
		if sensor.Value == nil || !strings.EqualFold(sensor.SensorType, "Temperature") || !isLHMCPUSensor(sensor.Identifier) {
			continue
		}
		value := float64(*sensor.Value)
		if value <= 0 || value > 150 {
			continue
		}
		allSum += value
		allCount++
		name := strings.ToLower(sensor.Name)
		if strings.Contains(name, "package") || strings.Contains(name, "tctl") || strings.Contains(name, "tdie") {
			preferredSum += value
			preferredCount++
		}
	}
	if preferredCount > 0 {
		return preferredSum / float64(preferredCount), true
	}
	if allCount > 0 {
		return allSum / float64(allCount), true
	}
	return 0, false
}

func isLHMCPUSensor(identifier string) bool {
	lower := strings.ToLower(identifier)
	return strings.HasPrefix(lower, "/intelcpu/") || strings.HasPrefix(lower, "/amdcpu/") || strings.HasPrefix(lower, "/cpu/")
}
