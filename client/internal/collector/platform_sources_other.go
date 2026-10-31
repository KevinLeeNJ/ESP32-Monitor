//go:build !darwin && !windows && !linux

package collector

var nativeSources = []source{}

func nativeLoadSource() string { return "system" }

func nativeSourceID(string) string { return "" }
