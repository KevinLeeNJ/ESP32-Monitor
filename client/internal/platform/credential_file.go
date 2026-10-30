package platform

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// localGitHubCredentials is macOS's application-owned credential file store.
// Directory is injectable for tests; production uses the user's config folder.
// Credentials are plaintext, protected by a private directory and file modes.
type localGitHubCredentials struct{ Directory string }

func (s localGitHubCredentials) root(ctx context.Context, account string, create bool) (*os.Root, string, error) {
	if ctx.Err() != nil || !ValidGitHubLogin(account) {
		return nil, "", ErrCredentialUnavailable
	}
	dir := s.Directory
	if dir == "" {
		base, err := AppDataDir()
		if err != nil {
			return nil, "", ErrCredentialUnavailable
		}
		dir = filepath.Join(base, "credentials", "github")
	}
	if create {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return nil, "", ErrCredentialUnavailable
		}
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 {
		return nil, "", ErrCredentialUnavailable
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, "", ErrCredentialUnavailable
	}
	return root, strings.ToLower(account) + ".json", nil
}
func (s localGitHubCredentials) Load(ctx context.Context, account string) (string, error) {
	root, name, err := s.root(ctx, account, false)
	if err != nil {
		return "", err
	}
	defer root.Close()
	info, err := root.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 16384 {
		return "", ErrCredentialUnavailable
	}
	f, err := root.Open(name)
	if err != nil {
		return "", ErrCredentialUnavailable
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 16385))
	if err != nil || len(data) == 0 || len(data) > 16384 || ctx.Err() != nil {
		return "", ErrCredentialUnavailable
	}
	return string(data), nil
}
func (s localGitHubCredentials) Save(ctx context.Context, account, secret string) error {
	if secret == "" || len(secret) > 16384 {
		return ErrCredentialUnavailable
	}
	root, name, err := s.root(ctx, account, true)
	if err != nil {
		return err
	}
	defer root.Close()
	// CreateTemp guarantees 0600. Stage in the same private directory and rename
	// through Root so readers see either the old complete record or the new one.
	f, err := os.CreateTemp(root.Name(), ".oauth-*")
	if err != nil {
		return ErrCredentialUnavailable
	}
	temp := filepath.Base(f.Name())
	defer root.Remove(temp)
	if _, err = f.WriteString(secret); err != nil {
		f.Close()
		return ErrCredentialUnavailable
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return ErrCredentialUnavailable
	}
	if err = f.Close(); err != nil {
		return ErrCredentialUnavailable
	}
	if ctx.Err() != nil {
		return ErrCredentialUnavailable
	}
	if err = root.Rename(temp, name); err != nil {
		return ErrCredentialUnavailable
	}
	return nil
}
func (s localGitHubCredentials) Delete(ctx context.Context, account string) error {
	root, name, err := s.root(ctx, account, true)
	if err != nil {
		return err
	}
	defer root.Close()
	if err = root.Remove(name); err != nil && !os.IsNotExist(err) {
		return ErrCredentialUnavailable
	}
	return nil
}
