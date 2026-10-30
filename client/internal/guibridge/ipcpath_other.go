//go:build !darwin && !linux && !windows

package guibridge

import (
	"path/filepath"

	"esp32-monitor/client/internal/platform"
)

func defaultIPCPath() (string, error) {
	base, err := platform.AppDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "agent-ipc.sock"), nil
}
