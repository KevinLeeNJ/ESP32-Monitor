// Package platform isolates user-session OS integrations from providers and GUI.
package platform

import (
	"context"
	"errors"
	"runtime"
)

var ErrCredentialUnavailable = errors.New("credential_unavailable")

// GitHubToken reads a secret from the platform-configured application store.
// No token is accepted on the command line, written to config, or returned in snapshots.
// OAuth updates use GitHubCredentials; GUI bindings never expose secret values.
func GitHubToken(ctx context.Context, account string) (string, error) {
	switch runtime.GOOS {
	case "windows":
		return windowsGitHubToken(ctx, account)
	case "darwin":
		return (localGitHubCredentials{}).Load(ctx, account)
	case "linux":
		return linuxGitHubSecret(ctx, "load", account, "")
	default:
		return "", ErrCredentialUnavailable
	}
}
