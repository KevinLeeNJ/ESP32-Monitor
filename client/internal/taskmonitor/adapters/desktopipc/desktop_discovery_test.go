package desktopipc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeSessionIndex(t *testing.T, home, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(home, "session_index.jsonl"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverThreadPicksNewestUpdatedAt(t *testing.T) {
	home := t.TempDir()
	writeSessionIndex(t, home, strings.Join([]string{
		`{"id":"older","updated_at":"2026-10-28T09:00:00Z"}`,
		`{"id":"newest","updated_at":"2026-10-28T11:00:00.123456Z"}`,
		`{"id":"middle","updated_at":"2026-10-28T10:00:00Z"}`,
	}, "\n")+"\n")
	id, err := discoverThread(home)
	if err != nil || id != "newest" {
		t.Fatalf("discover = %q, %v", id, err)
	}
}

func TestDiscoverThreadIgnoresMalformedAndInvalidEntries(t *testing.T) {
	home := t.TempDir()
	writeSessionIndex(t, home, strings.Join([]string{
		`not-json`,
		`{"id":"../bad","updated_at":"2026-10-28T12:00:00Z"}`,
		`{"id":"","updated_at":"2026-10-28T12:00:00Z"}`,
		`{"id":"no-timestamp"}`,
		`{"id":"valid","updated_at":"2026-10-28T08:00:00Z"}`,
	}, "\n")+"\n")
	id, err := discoverThread(home)
	if err != nil || id != "valid" {
		t.Fatalf("discover = %q, %v", id, err)
	}
}

func TestDiscoverThreadMissingOrEmptyIndex(t *testing.T) {
	if _, err := discoverThread(t.TempDir()); err == nil || err.Error() != "desktop_task_not_found" {
		t.Fatalf("missing index = %v", err)
	}
	home := t.TempDir()
	writeSessionIndex(t, home, "\n")
	if _, err := discoverThread(home); err == nil || err.Error() != "desktop_task_not_found" {
		t.Fatalf("empty index = %v", err)
	}
}

func TestDiscoverThreadReadsTailOfLargeIndex(t *testing.T) {
	home := t.TempDir()
	tail := `{"id":"tail-thread","updated_at":"2026-10-28T13:00:00Z"}` + "\n"
	content := strings.Repeat(`{"id":"bulk-thread","updated_at":"2026-10-28T12:00:00Z"}`+"\n", (maxSessionIndex/64)+1) + tail
	writeSessionIndex(t, home, content)
	id, err := discoverThread(home)
	if err != nil || id != "tail-thread" {
		t.Fatalf("discover = %q, %v", id, err)
	}
}

func TestAutoSelectionScopeAndReset(t *testing.T) {
	d := New(t.TempDir(), nil, time.Now)
	if s := d.Snapshot(); s.Scope != "desktop_latest_thread" || len(s.Items) != 0 {
		t.Fatal("auto observer without selection", s)
	}
	if !d.selectThread("latest") || d.currentID() != "latest" {
		t.Fatal("latest thread not selected")
	}
	if s := d.Snapshot(); len(s.Items) != 1 || s.Items[0].ID != "latest" || s.SelectedTask == nil || s.SelectedTask.ID != "latest" {
		t.Fatal("auto selection not projected", s)
	}
	d.states["latest"].code = "desktop_state_stale"
	d.success = nil
	if !d.selectThread("newer") {
		t.Fatal("newer thread not selected")
	}
	if s := d.Snapshot(); len(s.Items) != 1 || s.Items[0].ID != "newer" || s.Items[0].State != "unknown" || s.Health.ErrorCode != "desktop_state_stale" {
		t.Fatal("switch retained previous thread state", s)
	}
	if d.selectThread("newer") {
		t.Fatal("reselecting same thread reset state")
	}
	explicit := New(t.TempDir(), []string{"a"}, time.Now)
	if s := explicit.Snapshot(); s.Scope != "desktop_selected_threads" {
		t.Fatal("explicit selection scope", s)
	}
}
