package desktopipc

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestDiscoverDesktopUserThreadSkipsInternalAndArchived(t *testing.T) {
	home := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(home, "state_5.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE threads(id TEXT, archived INTEGER, thread_source TEXT, source TEXT, updated_at_ms INTEGER);
INSERT INTO threads VALUES ('user-chat',0,'user','vscode',10),('guardian',0,'guardian_review','subagent',30),('archived',1,'user','vscode',40);`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	id, err := discoverThread(home)
	if err != nil || id != "user-chat" {
		t.Fatalf("got %q, %v", id, err)
	}
}
