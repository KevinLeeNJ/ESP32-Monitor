//go:build !darwin && !linux && !windows

package adapters

import "esp32-monitor/client/internal/taskmonitor"

// Add a platform's native observer here once its source has been verified.
func platformDefault(opts Options) taskmonitor.Adapter {
	return unavailable{name: "none", code: "task_platform_unsupported", now: opts.Now}
}
func platformDesktop(opts Options) taskmonitor.Adapter {
	return unavailable{name: "desktop-ipc", code: "task_platform_unsupported", now: opts.Now}
}
