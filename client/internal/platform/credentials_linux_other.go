//go:build !linux

package platform

import "context"

func linuxGitHubSecret(context.Context, string, string, string) (string, error) {
	return "", ErrCredentialUnavailable
}
