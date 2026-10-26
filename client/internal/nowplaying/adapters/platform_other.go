//go:build !darwin && !linux && !windows

package adapters

import (
	"context"
	"errors"
	"os/exec"
)

func platformCommand(o Options) (string, string, func(context.Context) (*exec.Cmd, error)) {
	return "unsupported", "none", func(context.Context) (*exec.Cmd, error) { return nil, errors.New("music_unsupported") }
}
