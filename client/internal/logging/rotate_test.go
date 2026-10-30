package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRotatingWriterKeepsBoundedFiles(t *testing.T) {
	dir := t.TempDir()
	writer, err := NewRotatingWriter(dir, "test", 100, 2)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		if _, err := fmt.Fprintf(writer, "%02d %s\n", i, strings.Repeat("a", 40)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, entry := range entries {
		names[entry.Name()] = true
	}
	for _, name := range []string{"test.log", "test.log.1", "test.log.2"} {
		if !names[name] {
			t.Fatalf("missing %s after rotation: %v", name, names)
		}
	}
	if names["test.log.3"] {
		t.Fatalf("rotation kept more backups than configured: %v", names)
	}

	newest, err := os.ReadFile(filepath.Join(dir, "test.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(newest), "19 ") {
		t.Fatalf("newest file does not contain the latest write: %q", newest)
	}
	if strings.Contains(string(newest), "00 ") {
		t.Fatalf("oldest write was not rotated out: %q", newest)
	}
}

func TestRotatingWriterReopensAfterRestart(t *testing.T) {
	dir := t.TempDir()
	first, err := NewRotatingWriter(dir, "test", 1<<20, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.Write([]byte("first\n")); err != nil {
		t.Fatal(err)
	}
	_ = first.Close()

	second, err := NewRotatingWriter(dir, "test", 1<<20, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := second.Write([]byte("second\n")); err != nil {
		t.Fatal(err)
	}
	_ = second.Close()

	data, err := os.ReadFile(filepath.Join(dir, "test.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "first\nsecond\n") {
		t.Fatalf("reopen lost or reordered data: %q", data)
	}
}
