//go:build !windows

package transport

// PreferredLANIPv4 preserves the platform's existing default-route selection.
func PreferredLANIPv4(defaultIP string) string { return defaultIP }
