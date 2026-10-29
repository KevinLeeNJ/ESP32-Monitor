package desktopipc

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"
)

const maxSessionIndex = 4 << 20

// discoverThread returns the most recently updated local Codex thread. Only id
// and updated_at are decoded; thread names and task content are never retained.
func discoverThread(home string) (string, error) {
	if id, available := discoverDesktopUserThread(home); available {
		if id == "" {
			return "", errors.New("desktop_task_not_found")
		}
		return id, nil
	}
	file, err := os.Open(filepath.Join(home, "session_index.jsonl"))
	if err != nil {
		return "", errors.New("desktop_task_not_found")
	}
	defer file.Close()
	if info, err := file.Stat(); err == nil && info.Size() > maxSessionIndex {
		if _, err := file.Seek(-maxSessionIndex, io.SeekEnd); err != nil {
			return "", errors.New("desktop_session_index_unavailable")
		}
	}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	var bestID string
	var best time.Time
	for scanner.Scan() {
		var entry struct {
			ID        string `json:"id"`
			UpdatedAt string `json:"updated_at"`
		}
		if json.Unmarshal(scanner.Bytes(), &entry) != nil || !desktopID.MatchString(entry.ID) {
			continue
		}
		updated, err := time.Parse(time.RFC3339Nano, entry.UpdatedAt)
		if err != nil {
			updated = time.Time{}
		}
		if bestID == "" || !updated.Before(best) {
			bestID, best = entry.ID, updated
		}
	}
	if err := scanner.Err(); err != nil {
		return "", errors.New("desktop_session_index_unavailable")
	}
	if bestID == "" {
		return "", errors.New("desktop_task_not_found")
	}
	return bestID, nil
}
