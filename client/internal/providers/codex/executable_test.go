package codex

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestResolveExecutableKeepsExplicitPath(t *testing.T) {
	if got := resolveExecutable("/custom/bin/codex"); got != "/custom/bin/codex" {
		t.Fatalf("explicit path = %q", got)
	}
}

func TestResolveExecutableUsesPATH(t *testing.T) {
	dir := t.TempDir()
	name := "codex"
	if runtime.GOOS == "windows" {
		name = "codex.exe"
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	if got := resolveExecutable("codex"); got != path {
		t.Fatalf("PATH lookup = %q, want %q", got, path)
	}
	if got := resolveExecutable(""); got != path {
		t.Fatalf("empty configured = %q, want %q", got, path)
	}
}

func TestBundledPathsIncludeDesktopApps(t *testing.T) {
	paths := codexBundledPaths("/Users/example")
	want := map[string]bool{
		"/Applications/ChatGPT.app/Contents/Resources/codex-cli/CodexCLI.app/Contents/MacOS/codex":               false,
		"/Users/example/Applications/ChatGPT.app/Contents/Resources/codex-cli/CodexCLI.app/Contents/MacOS/codex": false,
		"/Applications/Codex.app/Contents/Resources/codex-cli/CodexCLI.app/Contents/MacOS/codex":                 false,
		"/Users/example/Applications/Codex.app/Contents/Resources/codex-cli/CodexCLI.app/Contents/MacOS/codex":   false,
	}
	for _, path := range paths {
		if _, ok := want[path]; ok {
			want[path] = true
		}
	}
	for path, found := range want {
		if !found {
			t.Fatalf("bundled path missing: %s", path)
		}
	}
}
