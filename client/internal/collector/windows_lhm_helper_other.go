//go:build !windows

package collector

import "errors"

func hardwareMonitorStatus(map[string]string) *HardwareMonitorStatus { return nil }
func lhmReason(error) string                                         { return "unavailable" }

var errDotNetFramework472Required = errors.New("not_windows")
