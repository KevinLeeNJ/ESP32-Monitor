// Package platform isolates user-session OS integrations from providers and GUI.
package platform

import (
	"context"
	"errors"
	"os/exec"
	"runtime"
	"strings"
)

var ErrCredentialUnavailable = errors.New("credential_unavailable")

// GitHubToken reads a secret from the platform-configured application store.
// No token is accepted on the command line, written to config, or returned in snapshots.
// OAuth updates use GitHubCredentials; GUI bindings never expose secret values.
func GitHubToken(ctx context.Context, account string) (string, error) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		return windowsGitHubToken(ctx, account)
	case "darwin":
		return (localGitHubCredentials{}).Load(ctx, account)
	case "linux":
		cmd = exec.CommandContext(ctx, "secret-tool", "lookup", "service", "esp32-monitor.github", "account", account)
	default:
		return "", ErrCredentialUnavailable
	}
	data, err := cmd.Output()
	if err != nil || len(data) > 16384 {
		return "", ErrCredentialUnavailable
	}
	token := strings.TrimSpace(string(data))
	if token == "" || strings.ContainsAny(token, "\r\n") {
		return "", ErrCredentialUnavailable
	}
	return token, nil
}
