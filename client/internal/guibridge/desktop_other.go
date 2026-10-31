//go:build !linux

package guibridge

func EnsureDesktopIntegration() error { return nil }
