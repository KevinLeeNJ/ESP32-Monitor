//go:build darwin || linux

package guibridge

import (
	"os"
	"path/filepath"
)

func defaultIPCPath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "ESP32-Monitor", "agent-ipc.sock"), nil
}
