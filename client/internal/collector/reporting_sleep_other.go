//go:build !darwin || !cgo

package collector

func macReportingPaused() bool { return false }
