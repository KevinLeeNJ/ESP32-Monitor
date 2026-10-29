//go:build windows

package collector

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/sys/windows/registry"
)

const dotNetFramework472Release = 461808

var (
	errDotNetFramework472Required = errors.New("dotnet_framework_472_required")
	errLHMHelperMissing           = errors.New("lhm_helper_missing")
)

type windowsLHMData struct {
	OK       bool             `json:"ok"`
	Schema   int              `json:"schema"`
	Backend  string           `json:"backend"`
	Elevated bool             `json:"elevated"`
	Warnings []string         `json:"warnings"`
	CPU      *float64         `json:"cpu_temperature_celsius"`
	GPUs     []windowsLHMGPU  `json:"gpus"`
	Disks    []windowsLHMDisk `json:"disks"`
	Error    string           `json:"error_code"`
}
type windowsLHMGPU struct {
	Model       string   `json:"model"`
	Temperature *float64 `json:"temperature_celsius"`
}
type windowsLHMDisk struct {
	Device      string   `json:"device"`
	Model       string   `json:"model"`
	Total       *uint64  `json:"total_bytes"`
	Used        *uint64  `json:"used_bytes"`
	Temperature *float64 `json:"temperature_celsius"`
	Healthy     *bool    `json:"smart_healthy"`
}

type windowsLHMCache struct {
	mu   sync.Mutex
	data windowsLHMData
	at   time.Time
	err  error
}

func dotNetFramework472Installed() bool {
	for _, view := range []uint32{0, registry.WOW64_32KEY} {
		key, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\NET Framework Setup\NDP\v4\Full`, registry.QUERY_VALUE|view)
		if err != nil {
			continue
		}
		release, _, err := key.GetIntegerValue("Release")
		key.Close()
		if err == nil && release >= dotNetFramework472Release {
			return true
		}
	}
	return false
}

func windowsLHMHelperPath() (string, error) {
	if value := os.Getenv("MONITOR_LHM_HELPER"); value != "" {
		if info, err := os.Stat(value); err == nil && !info.IsDir() {
			return value, nil
		}
	}
	executable, err := os.Executable()
	if err == nil {
		for _, candidate := range []string{filepath.Join(filepath.Dir(executable), "helpers", "lhm-helper", "lhm-helper.exe"), filepath.Join(filepath.Dir(executable), "lhm-helper.exe")} {
			if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
				return candidate, nil
			}
		}
	}
	return "", errLHMHelperMissing
}

func (c *windowsLHMCache) read(ctx context.Context) (windowsLHMData, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c != nil && !c.at.IsZero() && time.Since(c.at) < 5*time.Second {
		return c.data, c.err
	}
	if !dotNetFramework472Installed() {
		c.err, c.at = errDotNetFramework472Required, time.Now()
		return windowsLHMData{}, c.err
	}
	path, err := windowsLHMHelperPath()
	if err != nil {
		c.err, c.at = err, time.Now()
		return windowsLHMData{}, err
	}
	callCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	output, err := exec.CommandContext(callCtx, path).Output()
	if err != nil {
		c.err, c.at = err, time.Now()
		return windowsLHMData{}, err
	}
	var data windowsLHMData
	if err = json.Unmarshal(output, &data); err != nil || !data.OK || data.Schema != 1 {
		c.err, c.at = errors.New("lhm_scan_failed"), time.Now()
		return windowsLHMData{}, c.err
	}
	c.data, c.err, c.at = data, nil, time.Now()
	return data, nil
}

func lhmReason(err error) string {
	if errors.Is(err, errDotNetFramework472Required) {
		return "dotnet_framework_472_required"
	}
	if errors.Is(err, errLHMHelperMissing) {
		return "lhm_helper_missing"
	}
	return "lhm_scan_failed"
}

func (data windowsLHMData) hasWarning(want string) bool {
	for _, warning := range data.Warnings {
		if warning == want {
			return true
		}
	}
	return false
}

// lhmDataReason turns the helper's explicit privilege diagnosis into a source
// capability state. The Windows Agent is normally elevated before it starts.
func lhmDataReason(data windowsLHMData, source string) string {
	if source != "windows_cpu" && source != "windows_disk" {
		return ""
	}
	if data.hasWarning("not_elevated") {
		if source == "windows_cpu" && data.CPU == nil {
			return "lhm_permission_required"
		}
		if source == "windows_disk" && len(data.Disks) == 0 {
			return "lhm_permission_required"
		}
	}
	return ""
}

func hardwareMonitorStatus(reasons map[string]string) *HardwareMonitorStatus {
	const backend = "LibreHardwareMonitor 0.9.4 (Ring0)"
	// Check actionable prerequisite failures before the generic runtime states;
	// map iteration order must not decide the GUI message.
	for _, wanted := range []string{"dotnet_framework_472_required", "lhm_helper_missing", "lhm_permission_required"} {
		for _, reason := range reasons {
			if reason != wanted {
				continue
			}
			switch reason {
			case "dotnet_framework_472_required":
				return &HardwareMonitorStatus{Backend: backend, State: "missing_dependency", Code: reason, Message: "需要 .NET Framework 4.7.2 或更高版本才能读取 Windows 硬件传感器。请安装后重新检测。"}
			case "lhm_helper_missing":
				return &HardwareMonitorStatus{Backend: backend, State: "missing_dependency", Code: reason, Message: "Windows 硬件监控组件未随当前安装包提供；请重新安装 Windows 发行包。"}
			case "lhm_permission_required":
				return &HardwareMonitorStatus{Backend: backend, State: "permission_required", Code: reason, Message: "后台 Agent 未获得管理员权限，无法读取 CPU 或磁盘低层传感器。请允许 Agent 的 UAC 请求后重新检测。"}
			}
		}
	}
	return nil
}
