package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"esp32-monitor/client/internal/platform"
)

func enableSystemd(ctx context.Context, stateDir string) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	stateDir, err = filepath.Abs(stateDir)
	if err != nil {
		return err
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	unitDir := filepath.Join(configDir, "systemd", "user")
	if err = os.MkdirAll(unitDir, 0700); err != nil {
		return err
	}
	// systemd expands percent specifiers even inside quoted arguments.
	quote := func(s string) string { return strconv.Quote(strings.NewReplacer("%", "%%", "$", "$$").Replace(s)) }
	unit := fmt.Sprintf("[Unit]\nDescription=ESP32 Monitor headless collector\nAfter=network-online.target\n\n[Service]\nExecStart=%s --state-dir %s\nRestart=on-failure\nRestartSec=5\n\n[Install]\nWantedBy=default.target\n", quote(executable), quote(stateDir))
	if err = platform.WriteFileAtomic(unitDir, ".monitor-headless-*", "monitor-headless.service", []byte(unit), 0644); err != nil {
		return err
	}
	for _, args := range [][]string{{"--user", "daemon-reload"}, {"--user", "enable", "monitor-headless.service"}} {
		output, err := exec.CommandContext(ctx, "systemctl", args...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("systemctl %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
		}
	}
	return nil
}
