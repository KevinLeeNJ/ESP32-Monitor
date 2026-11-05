package windowshelper

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
)

const helperName = "windows-helper.exe"

func helperPath() (string, error) {
	if value := os.Getenv("MONITOR_WINDOWS_HELPER"); value != "" {
		if info, err := os.Stat(value); err == nil && !info.IsDir() {
			return value, nil
		}
		return "", ErrMissing
	}
	if executable, err := os.Executable(); err == nil {
		for _, candidate := range []string{
			filepath.Join(filepath.Dir(executable), "helpers", "windows-helper", helperName),
			filepath.Join(filepath.Dir(executable), helperName),
		} {
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				return candidate, nil
			}
		}
	}
	return "", ErrMissing
}

func NewPlatform() *Client {
	return New(func(ctx context.Context) (*exec.Cmd, error) {
		path, err := helperPath()
		if err != nil {
			return nil, err
		}
		cmd := exec.CommandContext(ctx, path, "--parent", strconv.Itoa(os.Getpid()))
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		return cmd, nil
	})
}
