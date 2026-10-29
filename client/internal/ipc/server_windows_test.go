//go:build windows

package ipc

import (
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

var testPipeSeq atomic.Uint64

func testSocketPath(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf(`\\.\pipe\esp32-monitor-ipc-%d-%d-%d`, os.Getpid(), time.Now().UnixNano(), testPipeSeq.Add(1))
}

func TestWindowsPipePathValidation(t *testing.T) {
	path := testSocketPath(t)
	if err := validateIPCPath(path); err != nil {
		t.Fatalf("valid pipe rejected: %v", err)
	}
	for _, bad := range []string{`\\.\pipe\`, `\\.\pipe\a\b`, `C:\agent.sock`, `\\server\pipe\agent`} {
		if err := validateIPCPath(bad); err == nil {
			t.Fatalf("invalid endpoint accepted: %q", bad)
		}
	}
}

func TestWindowsCurrentUserSID(t *testing.T) {
	sid, err := currentUserSID()
	if err != nil || !strings.HasPrefix(sid, "S-1-") {
		t.Fatalf("sid = %q, %v", sid, err)
	}
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		t.Fatal(err)
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil || sid != user.User.Sid.String() {
		t.Fatalf("sid mismatch: %q vs %v (%v)", sid, user, err)
	}
}
