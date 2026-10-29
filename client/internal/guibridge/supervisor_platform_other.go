//go:build !windows

package guibridge

import "context"

func (s *Supervisor) startPlatformAgent(context.Context) (bool, error) { return false, nil }
