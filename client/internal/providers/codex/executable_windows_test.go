//go:build windows

package codex

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestResolveExecutableWindowsBundledNewest(t *testing.T) {
	local := t.TempDir()
	home := t.TempDir()
	oldDir := filepath.Join(local, "OpenAI", "Codex", "bin", "old")
	newDir := filepath.Join(local, "OpenAI", "Codex", "bin", "new")
	for _, dir := range []string{oldDir, newDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "codex.exe"), []byte("MZ"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	oldTime := time.Now().Add(-2 * time.Hour)
	newTime := time.Now().Add(-1 * time.Hour)
	if err := os.Chtimes(filepath.Join(oldDir, "codex.exe"), oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(newDir, "codex.exe"), newTime, newTime); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LOCALAPPDATA", local)
	t.Setenv("USERPROFILE", home)
	t.Setenv("ProgramFiles", filepath.Join(local, "Program Files"))
	t.Setenv("PATH", "")
	got := resolveExecutable("codex")
	want := filepath.Join(newDir, "codex.exe")
	if got != want {
		t.Fatalf("windows bundled = %q, want %q", got, want)
	}
}
