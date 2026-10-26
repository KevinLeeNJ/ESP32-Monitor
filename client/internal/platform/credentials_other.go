//go:build !windows

package platform

import "context"

func windowsGitHubToken(context.Context, string) (string, error) { return "", ErrCredentialUnavailable }

func windowsSaveCredential(string, string) error { return ErrCredentialUnavailable }
func windowsDeleteCredential(string) error       { return ErrCredentialUnavailable }
