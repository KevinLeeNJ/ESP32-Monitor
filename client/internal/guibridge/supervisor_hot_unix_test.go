//go:build !windows

package guibridge

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"esp32-monitor/client/internal/app"
	"esp32-monitor/client/internal/config"
	"esp32-monitor/client/internal/ipc"
)

func shortSocketPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "m")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, "a.sock")
}

func newHotHarness(t *testing.T) (*app.Application, *Supervisor, *ipc.Client) {
	t.Helper()
	data := t.TempDir()
	application, err := app.NewWithOptions(config.Config{
		Collector: config.Collector{StateDirectory: filepath.Join(data, "collector")},
		Companion: config.Companion{StateDirectory: filepath.Join(data, "companion")},
	}, t.TempDir(), app.Options{PrepareCollector: true, MQTTAddress: "127.0.0.1:0", HTTPAddress: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = application.Close() })
	runCtx, cancel := context.WithCancel(context.Background())
	musicDone := make(chan struct{})
	go func() { defer close(musicDone); application.Companion.Music.Run(runCtx) }()
	tasksDone := make(chan struct{})
	go func() { defer close(tasksDone); application.Companion.Codex.RunTasks(runCtx) }()
	t.Cleanup(func() {
		cancel()
		<-musicDone
		<-tasksDone
	})
	socket := shortSocketPath(t)
	server, err := ipc.NewServer(application, ipc.Options{SocketPath: socket})
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Listen(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	client, err := ipc.Dial(socket)
	if err != nil {
		t.Fatal(err)
	}
	return application, &Supervisor{socket: socket}, client
}

func waitStatus(t *testing.T, client *ipc.Client, check func(ipc.Status) bool) ipc.Status {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		status, err := client.Status(context.Background())
		if err == nil && check(status) {
			return status
		}
		if time.Now().After(deadline) {
			t.Fatalf("status timeout: %+v %v", status, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestSettingsChangesDoNotRestartAgent(t *testing.T) {
	_, supervisor, client := newHotHarness(t)
	ctx := context.Background()
	before, err := client.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	tokenPath := filepath.Join(t.TempDir(), "server-token")
	if err := os.WriteFile(tokenPath, []byte(strings.Repeat("a", 64)), 0600); err != nil {
		t.Fatal(err)
	}
	next := Settings{}
	steps := []struct {
		name  string
		apply func()
		check func(ipc.Status) bool
	}{
		{"github login", func() { next.GitHub.Login = "octocat" }, func(s ipc.Status) bool { return s.GitHubEnabled }},
		{"music on", func() { next.Music.Enabled = true }, func(s ipc.Status) bool { return s.MusicEnabled }},
		{"codex on", func() { next.Codex.Enabled = true }, func(s ipc.Status) bool { return s.CodexEnabled }},
		{"companion on", func() { next.Companion = CompanionSettings{Enabled: true, BindIP: "127.0.0.1"} }, func(s ipc.Status) bool { return s.CompanionEnabled }},
		{"collector on", func() { next.Collector.Enabled = true }, func(s ipc.Status) bool { return s.CollectorEnabled }},
		{"collector target", func() {
			next.Collector.UploadURL = "https://127.0.0.1:9/v2/collector/samples"
			next.Collector.UploadTokenFile = tokenPath
		}, func(s ipc.Status) bool { return s.CollectorEnabled }},
		{"collector off", func() { next.Collector.Enabled = false }, func(s ipc.Status) bool { return !s.CollectorEnabled }},
		{"companion off", func() { next.Companion = CompanionSettings{} }, func(s ipc.Status) bool { return !s.CompanionEnabled }},
		{"codex off", func() { next.Codex.Enabled = false }, func(s ipc.Status) bool { return !s.CodexEnabled }},
		{"music off", func() { next.Music.Enabled = false }, func(s ipc.Status) bool { return !s.MusicEnabled }},
		{"github off", func() { next.GitHub.Login = "" }, func(s ipc.Status) bool { return !s.GitHubEnabled }},
	}
	for _, step := range steps {
		step.apply()
		start := time.Now()
		if err := supervisor.UpdateSettings(ctx, next); err != nil {
			t.Fatalf("%s: %v", step.name, err)
		}
		if elapsed := time.Since(start); elapsed > time.Second {
			t.Fatalf("%s took %v, want under 1s", step.name, elapsed)
		}
		status := waitStatus(t, client, step.check)
		if status.PID != before.PID {
			t.Fatalf("%s restarted the Agent: %d -> %d", step.name, before.PID, status.PID)
		}
	}
}
