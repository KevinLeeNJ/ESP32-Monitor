package adapters

import (
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/binary"
	"os/exec"
	"unicode/utf16"
)

//go:embed scripts/gsmtc.ps1
var gsmtcScript string

func platformCommand(o Options) (string, string, func(context.Context) (*exec.Cmd, error)) {
	units := utf16.Encode([]rune(gsmtcScript))
	b := make([]byte, len(units)*2)
	for i, v := range units {
		binary.LittleEndian.PutUint16(b[i*2:], v)
	}
	encoded := base64.StdEncoding.EncodeToString(b)
	return "gsmtc", "system_current_source", func(ctx context.Context) (*exec.Cmd, error) {
		return exec.CommandContext(ctx, "powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", encoded), nil
	}
}
