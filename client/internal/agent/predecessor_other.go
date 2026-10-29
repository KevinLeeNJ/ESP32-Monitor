//go:build !darwin && !linux && !windows

package agent

func predecessorAlive(int) bool { return false }
