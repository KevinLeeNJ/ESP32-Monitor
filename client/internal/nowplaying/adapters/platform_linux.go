package adapters

import (
	"context"
	_ "embed"
	"os/exec"
)

//go:embed scripts/mpris.py
var mprisScript string

func platformCommand(o Options) (string, string, func(context.Context) (*exec.Cmd, error)) {
	return "mpris", "playing_first_lexical_bus_name", func(ctx context.Context) (*exec.Cmd, error) {
		return exec.CommandContext(ctx, "python3", "-u", "-c", mprisScript), nil
	}
}
