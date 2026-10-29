//go:build !darwin && !windows

package collector

var nativeSources = []source{}

func nativeLoadSource() string { return "system" }

func nativeSourceID(string) string { return "" }
