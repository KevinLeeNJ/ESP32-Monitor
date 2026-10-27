//go:build !darwin && !linux && !windows

package collector

import "runtime"

func newNativeCollector() Collector { return &NativeCollector{Platform: runtime.GOOS} }
