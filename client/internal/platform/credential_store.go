package platform

import (
	"context"
	"runtime"
)

// GitHubCredentials stores either a legacy PAT or a JSON OAuth token record in
// an application-private file on macOS, or the OS credential store elsewhere.
// Secret values never enter argv.
type GitHubCredentials struct{}

func (GitHubCredentials) Load(ctx context.Context, account string) (string, error) {
	return GitHubToken(ctx, account)
}
func (GitHubCredentials) Save(ctx context.Context, account, secret string) error {
	if secret == "" || len(secret) > 16384 {
		return ErrCredentialUnavailable
	}
	switch runtime.GOOS {
	case "darwin":
		return (localGitHubCredentials{}).Save(ctx, account, secret)
	case "windows":
		return windowsSaveCredential(account, secret)
	case "linux":
		_, err := linuxGitHubSecret(ctx, "save", account, secret)
		return err
	default:
		return ErrCredentialUnavailable
	}
}
func (GitHubCredentials) Delete(ctx context.Context, account string) error {
	switch runtime.GOOS {
	case "darwin":
		return (localGitHubCredentials{}).Delete(ctx, account)
	case "windows":
		return windowsDeleteCredential(account)
	case "linux":
		_, err := linuxGitHubSecret(ctx, "delete", account, "")
		return err
	default:
		return ErrCredentialUnavailable
	}
}
