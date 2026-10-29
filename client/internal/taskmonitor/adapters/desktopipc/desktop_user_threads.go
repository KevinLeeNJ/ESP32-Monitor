package desktopipc

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Select only visible user conversations; the session index also contains
// guardian and subagent sessions. No task text or title is read.
func discoverDesktopUserThread(home string) (string, bool) {
	path := filepath.Join(home, "state_5.sqlite")
	if _, err := os.Stat(path); err != nil {
		return "", false
	}
	path = filepath.ToSlash(path)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	uri := (&url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro"}).String()
	db, err := sql.Open("sqlite", uri)
	if err != nil {
		return "", true
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var id string
	err = db.QueryRowContext(ctx, "SELECT id FROM threads WHERE archived=0 AND (thread_source='user' OR (thread_source IS NULL AND source='vscode')) ORDER BY updated_at_ms DESC LIMIT 1").Scan(&id)
	if err != nil || !desktopID.MatchString(id) {
		return "", true
	}
	return id, true
}
