//go:build !windows

package proxy

// readWindows is only reached on Windows; the stub keeps other platforms building.
func readWindows() (Settings, error) { return Settings{}, ErrDetection }