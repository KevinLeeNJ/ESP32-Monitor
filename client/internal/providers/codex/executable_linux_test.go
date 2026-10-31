//go:build linux

package codex

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveExecutableLinuxDesktopFallback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", "")
	file := filepath.Join(home, ".local", "bin", "codex")
	if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("#!/bin/sh\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if got := resolveExecutable("codex"); got != file {
		t.Fatalf("got %q want %q", got, file)
	}
}
