package agent

import (
	"os"
	"path/filepath"
	"testing"
)

const testSocket = `\\.\pipe\esp32-monitor-restart-test`

func TestValidateRestartArgsAcceptsGUIFlags(t *testing.T) {
	config, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	token := filepath.Join(config, "ESP32-Monitor", "server-token")
	args := []string{
		"--ipc-socket", testSocket,
		"--collector", "--collector-upload-url", "https://example.com/v2/collector/samples",
		"--collector-upload-token-file", token,
		"--companion", "--companion-ip", "192.168.1.20",
		"--github-user", "octocat", "--github-client-id", "Ov23liExample",
		"--codex", "--codex-task-adapter", "auto", "--music",
	}
	if err := validateRestartArgs(testSocket, args); err != nil {
		t.Fatal(err)
	}
}

func TestValidateRestartArgsRejectsPrivilegeChanges(t *testing.T) {
	for name, args := range map[string][]string{
		"foreign socket":    {"--ipc-socket", `\\.\pipe\other`},
		"unknown flag":      {"--ipc-socket", testSocket, "--codex-executable", `C:\evil.exe`},
		"positional arg":    {"--ipc-socket", testSocket, "extra"},
		"upload no collect": {"--ipc-socket", testSocket, "--collector-upload-url", "https://example.com/v2/collector/samples"},
		"plain http":        {"--ipc-socket", testSocket, "--collector", "--collector-upload-url", "http://example.com/v2/collector/samples"},
		"token outside":     {"--ipc-socket", testSocket, "--collector", "--collector-upload-token-file", filepath.Join(string(os.PathSeparator), "Windows", "System32", "config", "SAM")},
		"bad login":         {"--ipc-socket", testSocket, "--github-user", "../octocat"},
		"bad client id":     {"--ipc-socket", testSocket, "--github-client-id", "id with space"},
		"ipv6 companion":    {"--ipc-socket", testSocket, "--companion", "--companion-ip", "::1"},
		"unknown adapter":   {"--ipc-socket", testSocket, "--codex", "--codex-task-adapter", "arbitrary"},
	} {
		if err := validateRestartArgs(testSocket, args); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
}
