//go:build windows

package collector

func newNativeCollector() Collector { return &NativeCollector{Platform: "windows"} }
