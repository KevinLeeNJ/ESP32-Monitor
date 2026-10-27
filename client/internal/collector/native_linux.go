//go:build linux

package collector

func newNativeCollector() Collector { return &NativeCollector{Platform: "linux"} }
