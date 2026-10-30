package platform

import (
	"os"
	"path/filepath"
)

// AppDataDir returns the application's private directory under the user's
// configuration directory. Callers create subdirectories as needed.
func AppDataDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "ESP32-Monitor"), nil
}
