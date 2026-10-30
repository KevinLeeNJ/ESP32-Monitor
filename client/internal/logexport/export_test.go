package logexport

import (
	"archive/zip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRedactorMasksCredentialsAndPaths(t *testing.T) {
	redactor := NewRedactor("/Users/alice")
	cases := []struct {
		name       string
		input      string
		wantAbsent string
		wantKeep   string
	}{
		{"home path", "open /Users/alice/Library/Logs/client.log", "alice", "~/Library/Logs/client.log"},
		{"bearer", "authorization: Bearer abcdefghijklmnop", "abcdefghijklmnop", "Bearer [redacted]"},
		{"query secret", "GET /v1?token=supersecret&x=1", "supersecret", "token=[redacted]"},
		{"github token", "login ghp_0123456789abcdefghijABCDEFGHIJ012345", "ghp_0123", "[redacted-token]"},
		{"json secret", `{"access_token":"json-secret"}`, "json-secret", `"access_token":"[redacted]"`},
		{"hex digest", "digest=0123456789abcdef0123456789abcdef01234567", "0123456789abcdef0123456789abcdef01234567", "[redacted-hex]"},
		{"uuid", "device=12345678-1234-1234-1234-123456789abc", "123456789abc", "12345678-[redacted-id]"},
		{"url credentials", "url=https://user:pass@example.com/x", ":pass@", "://[redacted]@"},
		{"error code kept", "error_code=quota_read_failed", "[redacted]", "quota_read_failed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := redactor.Text(tc.input)
			if strings.Contains(got, tc.wantAbsent) {
				t.Fatalf("redacted text still contains %q: %s", tc.wantAbsent, got)
			}
			if !strings.Contains(got, tc.wantKeep) {
				t.Fatalf("redacted text lost %q: %s", tc.wantKeep, got)
			}
		})
	}
}

func TestExportWritesRedactedArchive(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	logDir := filepath.Join(home, "logs")
	if err := os.MkdirAll(logDir, 0700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(logDir, "client.log"), "msg=log_ready file="+logDir+"/client.log\nauthorization: Bearer topsecretvalue\n")
	writeFile(t, filepath.Join(logDir, "client.log.1"), "msg=log_ready rotated\n")
	writeFile(t, filepath.Join(logDir, "agent.log"), "msg=ipc_listening path="+logDir+"/agent.sock\n")
	outDir := filepath.Join(root, "out")
	if err := os.MkdirAll(outDir, 0700); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	reportJSON := []byte(`{"schema_version":1,"generated_at":"2026-10-30T12:00:00Z","pid":42}`)
	result, err := Export(Options{
		LogDir:      logDir,
		SavePath:    filepath.Join(outDir, "bundle"),
		AppVersion:  "0.1.0",
		ReportJSON:  reportJSON,
		AgentError:  "spawn failed at " + home,
		RedactPaths: []string{home},
		Now:         now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(result.Path, ".zip") {
		t.Fatalf("save path was not corrected to .zip: %s", result.Path)
	}
	if result.Files != 3 || result.Bytes <= 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
	entries := readArchive(t, result.Path)
	for _, name := range []string{"README.txt", "manifest.json", "diagnostics.json", "logs/client.log", "logs/client.log.1", "logs/agent.log"} {
		if _, ok := entries[name]; !ok {
			t.Fatalf("archive missing %s; entries: %v", name, keys(entries))
		}
	}
	clientLog := entries["logs/client.log"]
	if strings.Contains(clientLog, "topsecretvalue") || strings.Contains(clientLog, "alice") {
		t.Fatalf("client log was not redacted: %s", clientLog)
	}
	if !strings.Contains(clientLog, "file=~/logs/client.log") {
		t.Fatalf("home path was not replaced with ~: %s", clientLog)
	}
	var manifest Manifest
	if err := json.Unmarshal([]byte(entries["manifest.json"]), &manifest); err != nil {
		t.Fatalf("manifest is not valid JSON: %v", err)
	}
	if !manifest.DiagnosticsIncluded || len(manifest.Logs) != 3 || manifest.AppVersion != "0.1.0" {
		t.Fatalf("unexpected manifest: %+v", manifest)
	}
	if strings.Contains(manifest.AgentError, "alice") {
		t.Fatalf("manifest agent error was not redacted: %s", manifest.AgentError)
	}
	// The temporary archive must not survive a successful export.
	leftovers, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(leftovers) != 1 {
		t.Fatalf("temporary files left behind: %v", leftovers)
	}
}

func TestExportWithoutLogDirectoryStillWritesManifest(t *testing.T) {
	outDir := t.TempDir()
	result, err := Export(Options{
		LogDir:   filepath.Join(outDir, "missing"),
		SavePath: filepath.Join(outDir, "bundle.zip"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Files != 0 {
		t.Fatalf("expected no log files, got %+v", result)
	}
	entries := readArchive(t, result.Path)
	if _, ok := entries["diagnostics.json"]; ok {
		t.Fatal("diagnostics.json should be absent without a report")
	}
	var manifest Manifest
	if err := json.Unmarshal([]byte(entries["manifest.json"]), &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Logs) != 0 || len(manifest.Notes) == 0 || manifest.DiagnosticsIncluded {
		t.Fatalf("unexpected manifest: %+v", manifest)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func readArchive(t *testing.T, path string) map[string]string {
	t.Helper()
	reader, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	entries := make(map[string]string, len(reader.File))
	for _, file := range reader.File {
		source, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(source)
		_ = source.Close()
		if err != nil {
			t.Fatal(err)
		}
		entries[file.Name] = string(data)
	}
	return entries
}

func keys(entries map[string]string) []string {
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	return names
}
