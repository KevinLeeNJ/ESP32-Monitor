//go:build darwin && !cgo

package collector

func readSMC(string) (map[string]float64, map[string]float64) { return nil, nil }
