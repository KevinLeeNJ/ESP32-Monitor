//go:build linux

package platform

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

func TestLinuxSecretServiceRoundTrip(t *testing.T) {
	if os.Getenv("ESP32_TEST_SECRET_SERVICE") != "1" {
		t.Skip("requires an unlocked user Secret Service")
	}
	account := fmt.Sprintf("esp32test%d", time.Now().UnixNano())
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	store := GitHubCredentials{}
	defer store.Delete(context.Background(), account)
	if err := store.Save(ctx, account, `{"access_token":"synthetic-test-value"}`); err != nil {
		t.Fatal("save failed", err)
	}
	got, err := store.Load(ctx, account)
	if err != nil || got != `{"access_token":"synthetic-test-value"}` {
		t.Fatal("read did not return saved value")
	}
	if err := store.Delete(ctx, account); err != nil {
		t.Fatal("delete failed", err)
	}
	if _, err := store.Load(ctx, account); err == nil {
		t.Fatal("deleted credential remains readable")
	}
}
