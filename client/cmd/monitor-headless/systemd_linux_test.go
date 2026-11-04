package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnableSystemd(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("PATH", root)
	t.Setenv("SYSTEMCTL_LOG", filepath.Join(root, "commands"))
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$SYSTEMCTL_LOG\"\n"
	if err := os.WriteFile(filepath.Join(root, "systemctl"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(root, "state % $HOME")
	if err := enableSystemd(context.Background(), state); err != nil {
		t.Fatal(err)
	}
	unit, err := os.ReadFile(filepath.Join(root, "config", "systemd", "user", "monitor-headless.service"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(unit), `--state-dir "`+strings.ReplaceAll(strings.ReplaceAll(state, "%", "%%"), "$", "$$")+`"`) || !strings.Contains(string(unit), "WantedBy=default.target") {
		t.Fatalf("unexpected unit: %s", unit)
	}
	calls, err := os.ReadFile(filepath.Join(root, "commands"))
	if err != nil {
		t.Fatal(err)
	}
	if string(calls) != "--user daemon-reload\n--user enable monitor-headless.service\n" {
		t.Fatalf("unexpected commands: %s", calls)
	}
}

func TestEnableSystemdReportsFailure(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	t.Setenv("PATH", root)
	if err := os.WriteFile(filepath.Join(root, "systemctl"), []byte("#!/bin/sh\necho no-user-bus >&2\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := enableSystemd(context.Background(), root); err == nil || !strings.Contains(err.Error(), "no-user-bus") {
		t.Fatalf("expected systemctl failure, got %v", err)
	}
}
