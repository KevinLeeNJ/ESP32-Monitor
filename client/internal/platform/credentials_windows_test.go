//go:build windows

package platform

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func testCredentialAccount() string {
	return fmt.Sprintf("test-%d-%d", time.Now().UnixNano(), time.Now().Unix())
}

func TestWindowsCredentialRoundTrip(t *testing.T) {
	account := testCredentialAccount()
	t.Cleanup(func() { _ = windowsDeleteCredential(account) })
	if err := windowsSaveCredential(account, "synthetic-token"); err != nil {
		t.Skipf("Credential Manager unavailable: %v", err)
	}
	token, err := windowsGitHubToken(context.Background(), account)
	if err != nil || token != "synthetic-token" {
		t.Fatalf("stored token not readable: %q, %v", token, err)
	}
	if err = windowsDeleteCredential(account); err != nil {
		t.Fatal(err)
	}
	if _, err = windowsGitHubToken(context.Background(), account); !errors.Is(err, ErrCredentialUnavailable) {
		t.Fatalf("deleted token still readable: %v", err)
	}
	if err = windowsDeleteCredential(account); err != nil {
		t.Fatalf("delete must be idempotent: %v", err)
	}
}

func TestWindowsCredentialMissingAndCancelled(t *testing.T) {
	account := testCredentialAccount()
	if _, err := windowsGitHubToken(context.Background(), account); !errors.Is(err, ErrCredentialUnavailable) {
		t.Fatalf("missing credential accepted: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := windowsGitHubToken(ctx, account); !errors.Is(err, ErrCredentialUnavailable) {
		t.Fatalf("cancelled read returned: %v", err)
	}
}
