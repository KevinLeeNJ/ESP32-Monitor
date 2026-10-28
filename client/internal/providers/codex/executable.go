package codex

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// resolveExecutable prefers an explicit CLI path, then PATH, then the Codex CLI
// bundled with the macOS ChatGPT/Codex desktop app. A GUI-launched Agent
// inherits launchd's minimal PATH, which usually has no codex shim even though
// the desktop app ships a compatible CLI.
func resolveExecutable(configured string) string {
	if configured != "" && configured != "codex" {
		return configured
	}
	if path, err := exec.LookPath("codex"); err == nil {
		return path
	}
	if runtime.GOOS == "darwin" {
		home, _ := os.UserHomeDir()
		for _, candidate := range codexBundledPaths(home) {
			if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
				return candidate
			}
		}
	}
	return configured
}

func codexBundledPaths(home string) []string {
	return []string{
		"/Applications/ChatGPT.app/Contents/Resources/codex-cli/CodexCLI.app/Contents/MacOS/codex",
		filepath.Join(home, "Applications/ChatGPT.app/Contents/Resources/codex-cli/CodexCLI.app/Contents/MacOS/codex"),
		"/Applications/Codex.app/Contents/Resources/codex-cli/CodexCLI.app/Contents/MacOS/codex",
		filepath.Join(home, "Applications/Codex.app/Contents/Resources/codex-cli/CodexCLI.app/Contents/MacOS/codex"),
	}
}
