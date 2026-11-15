//go:build windows

package proxy

import (
	"golang.org/x/sys/windows/registry"
)

// readWindows reads the logged-in user's WinINET configuration directly from
// the registry; never modify it. A per-request PowerShell launch can exceed
// the detection timeout while the machine is busy (e.g. during a build).
func readWindows() (Settings, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Internet Settings`, registry.QUERY_VALUE)
	if err != nil {
		return Settings{}, ErrDetection
	}
	defer key.Close()
	readString := func(name string) (string, error) {
		value, _, err := key.GetStringValue(name)
		if err == registry.ErrNotExist {
			return "", nil
		}
		return value, err
	}
	enabled, _, err := key.GetIntegerValue("ProxyEnable")
	if err != nil && err != registry.ErrNotExist {
		return Settings{}, ErrDetection
	}
	server, err := readString("ProxyServer")
	if err != nil {
		return Settings{}, ErrDetection
	}
	bypass, err := readString("ProxyOverride")
	if err != nil {
		return Settings{}, ErrDetection
	}
	pac, err := readString("AutoConfigURL")
	if err != nil {
		return Settings{}, ErrDetection
	}
	return windowsSettings(enabled != 0, server, bypass, pac)
}