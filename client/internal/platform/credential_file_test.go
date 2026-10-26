package platform

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestLocalGitHubCredentialRoundTrip(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("production Windows uses Credential Manager")
	}
	store := localGitHubCredentials{Directory: filepath.Join(t.TempDir(), "github")}
	ctx := context.Background()
	if err := store.Save(ctx, "TestUser", `{"access_token":"synthetic"}`); err != nil {
		t.Fatal(err)
	}
	value, err := store.Load(ctx, "testuser")
	if err != nil || value != `{"access_token":"synthetic"}` {
		t.Fatal("roundtrip failed")
	}
	for path, want := range map[string]os.FileMode{store.Directory: 0700, filepath.Join(store.Directory, "testuser.json"): 0600} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != want {
			t.Fatal("unexpected credential permissions")
		}
	}
	if err := store.Save(ctx, "TestUser", "updated"); err != nil {
		t.Fatal(err)
	}
	if value, err = store.Load(ctx, "TestUser"); err != nil || value != "updated" {
		t.Fatal("update failed")
	}
	if err := store.Delete(ctx, "TestUser"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(ctx, "TestUser"); err == nil {
		t.Fatal("deleted credential readable")
	}
	if err := store.Delete(ctx, "TestUser"); err != nil {
		t.Fatal("delete must be idempotent")
	}
}
func TestLocalGitHubCredentialRejectsUnsafeInputs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("production Windows uses Credential Manager")
	}
	store := localGitHubCredentials{Directory: filepath.Join(t.TempDir(), "github")}
	ctx := context.Background()
	for _, account := range []string{"", "../outside", "a/b", "a--b"} {
		if store.Save(ctx, account, "synthetic") == nil {
			t.Fatal("unsafe account accepted")
		}
	}
	for _, secret := range []string{"", strings.Repeat("a", 16385)} {
		if store.Save(ctx, "user", secret) == nil {
			t.Fatal("invalid size accepted")
		}
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if store.Save(cancelled, "user", "synthetic") == nil {
		t.Fatal("cancelled save accepted")
	}
	if err := os.MkdirAll(store.Directory, 0700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(store.Directory, "user.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(ctx, "user"); err == nil {
		t.Fatal("symlink read accepted")
	}
	if err := store.Save(ctx, "user", "synthetic"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(outside)
	if string(data) != "outside" {
		t.Fatal("save followed symlink")
	}
	if err := os.Chmod(filepath.Join(store.Directory, "user.json"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(ctx, "user"); err == nil {
		t.Fatal("public credential readable")
	}
	if err := os.Chmod(store.Directory, 0755); err != nil {
		t.Fatal(err)
	}
	if store.Save(ctx, "user", "synthetic") == nil {
		t.Fatal("public store accepted")
	}
}
func TestLocalGitHubCredentialAtomicWrites(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("production Windows uses Credential Manager")
	}
	store := localGitHubCredentials{Directory: filepath.Join(t.TempDir(), "github")}
	ctx := context.Background()
	records := []string{strings.Repeat("a", 2048), strings.Repeat("b", 4096)}
	if err := store.Save(ctx, "user", records[0]); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for _, record := range records {
		wg.Add(1)
		go func(value string) {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				if err := store.Save(ctx, "user", value); err != nil {
					t.Error("write failed")
				}
			}
		}(record)
	}
	for i := 0; i < 50; i++ {
		v, err := store.Load(ctx, "user")
		if err != nil || (v != records[0] && v != records[1]) {
			t.Fatal("partial credential read")
		}
	}
	wg.Wait()
	entries, err := os.ReadDir(store.Directory)
	if err != nil || len(entries) != 1 {
		t.Fatal("temporary files left behind")
	}
}
