package adapters

import (
	"context"
	"os/exec"
)

func platformCommand(o Options) (string, string, func(context.Context) (*exec.Cmd, error)) {
	return "mpris", "playing_first_complete_metadata", nil
}
