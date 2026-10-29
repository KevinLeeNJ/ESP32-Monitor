package codex

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

// resolveExecutable prefers an explicit CLI path, then PATH, then a CLI bundled
// with the ChatGPT/Codex desktop app. A GUI-launched Agent inherits a minimal
// PATH, which usually has no codex shim even though the desktop app ships one.
func resolveExecutable(configured string) string {
	if configured != "" && configured != "codex" {
		return configured
	}
	if path, err := exec.LookPath("codex"); err == nil {
		return path
	}
	home, _ := os.UserHomeDir()
	var candidates []string
	switch runtime.GOOS {
	case "darwin":
		candidates = codexBundledPaths(home)
	case "windows":
		candidates = codexBundledPathsWindows(home)
	}
	for _, candidate := range candidates {
		if bundledExecutable(candidate) {
			return candidate
		}
	}
	return configured
}

func bundledExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	if runtime.GOOS == "windows" {
		return strings.EqualFold(filepath.Ext(path), ".exe")
	}
	return info.Mode().Perm()&0o111 != 0
}

// codexBundledPathsWindows returns candidates in update order: the desktop app
// downloads versioned CLIs under %LOCALAPPDATA%\OpenAI\Codex\bin\<hash>.
func codexBundledPathsWindows(home string) []string {
	type candidate struct {
		path string
		mod  time.Time
	}
	var found []candidate
	add := func(path string) {
		info, err := os.Stat(path)
		if err == nil && info.Mode().IsRegular() && strings.EqualFold(filepath.Ext(path), ".exe") {
			found = append(found, candidate{path: path, mod: info.ModTime()})
		}
	}
	if local := os.Getenv("LOCALAPPDATA"); local != "" {
		binRoot := filepath.Join(local, "OpenAI", "Codex", "bin")
		if entries, err := os.ReadDir(binRoot); err == nil {
			for _, entry := range entries {
				if entry.IsDir() {
					add(filepath.Join(binRoot, entry.Name(), "codex.exe"))
				}
			}
		}
		add(filepath.Join(local, "Programs", "codex", "codex.exe"))
		add(filepath.Join(local, "Programs", "Codex", "codex.exe"))
	}
	if home != "" {
		add(filepath.Join(home, ".codex", "bin", "codex.exe"))
	}
	if programFiles := os.Getenv("ProgramFiles"); programFiles != "" {
		matches, _ := filepath.Glob(filepath.Join(programFiles, "WindowsApps", "OpenAI.Codex_*_x64__*", "app", "resources", "codex.exe"))
		for _, match := range matches {
			add(match)
		}
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].mod.After(found[j].mod) })
	paths := make([]string, 0, len(found))
	for _, c := range found {
		paths = append(paths, c.path)
	}
	return paths
}

func codexBundledPaths(home string) []string {
	return []string{
		"/Applications/ChatGPT.app/Contents/Resources/codex-cli/CodexCLI.app/Contents/MacOS/codex",
		filepath.Join(home, "Applications/ChatGPT.app/Contents/Resources/codex-cli/CodexCLI.app/Contents/MacOS/codex"),
		"/Applications/Codex.app/Contents/Resources/codex-cli/CodexCLI.app/Contents/MacOS/codex",
		filepath.Join(home, "Applications/Codex.app/Contents/Resources/codex-cli/CodexCLI.app/Contents/MacOS/codex"),
	}
}
