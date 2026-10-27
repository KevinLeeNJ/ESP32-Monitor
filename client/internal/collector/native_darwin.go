//go:build darwin

package collector

func newNativeCollector() Collector { return &NativeCollector{Platform: "darwin"} }
