//go:build !windows

package collector

func hardwareMonitorStatus(map[string]string) *HardwareMonitorStatus { return nil }