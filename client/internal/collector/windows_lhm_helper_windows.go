//go:build windows

package collector

import (
	"context"
	"encoding/json"
	"errors"
	"esp32-monitor/client/internal/windowshelper"
	"time"
)

var errLHMHelperMissing = errors.New("lhm_helper_missing")

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
	gate        chan struct{}
	readSensors func(context.Context) (json.RawMessage, error)
	data        windowsLHMData
	at          time.Time
	err         error
}

func newWindowsLHMCache(helper *windowshelper.Client) *windowsLHMCache {
	c := &windowsLHMCache{gate: make(chan struct{}, 1)}
	if helper != nil {
		c.readSensors = helper.ReadSensors
	}
	return c
}

func (c *windowsLHMCache) read(ctx context.Context) (windowsLHMData, error) {
	if c == nil || c.readSensors == nil {
		return windowsLHMData{}, errLHMHelperMissing
	}
	callCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	select {
	case c.gate <- struct{}{}:
		defer func() { <-c.gate }()
	case <-callCtx.Done():
		return windowsLHMData{}, callCtx.Err()
	}
	if err := callCtx.Err(); err != nil {
		return windowsLHMData{}, err
	}
	if !c.at.IsZero() && time.Since(c.at) < 5*time.Second {
		return c.data, c.err
	}
	output, err := c.readSensors(callCtx)
	if err != nil {
		// A cancelled generation must not cache its cancellation for the next
		// generation; process-level failures are still briefly cached.
		if callCtx.Err() == nil {
			c.data, c.err, c.at = windowsLHMData{}, err, time.Now()
		}
		return windowsLHMData{}, err
	}
	var data windowsLHMData
	if err = json.Unmarshal(output, &data); err != nil || !data.OK || data.Schema != 1 {
		c.data, c.err, c.at = windowsLHMData{}, errors.New("lhm_scan_failed"), time.Now()
		return windowsLHMData{}, c.err
	}
	c.data, c.err, c.at = data, nil, time.Now()
	return data, nil
}

func lhmReason(err error) string {
	if errors.Is(err, errLHMHelperMissing) || errors.Is(err, windowshelper.ErrMissing) {
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
	const backend = "LibreHardwareMonitor 0.9.6 (PawnIO)"
	// Check actionable prerequisite failures before the generic runtime states;
	// map iteration order must not decide the GUI message.
	for _, wanted := range []string{"lhm_helper_missing", "lhm_permission_required"} {
		for _, reason := range reasons {
			if reason != wanted {
				continue
			}
			switch reason {
			case "lhm_helper_missing":
				return &HardwareMonitorStatus{Backend: backend, State: "missing_dependency", Code: reason, Message: "Windows 硬件监控组件未随当前安装包提供；请重新安装 Windows 发行包。"}
			case "lhm_permission_required":
				return &HardwareMonitorStatus{Backend: backend, State: "permission_required", Code: reason, Message: "后台 Agent 未获得管理员权限，无法读取 CPU 或磁盘低层传感器。请允许 Agent 的 UAC 请求后重新检测。"}
			}
		}
	}
	return nil
}
