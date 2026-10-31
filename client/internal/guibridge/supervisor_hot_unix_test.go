//go:build !windows

package guibridge

import (
	"context"
	"path/filepath"
	"testing"

	"esp32-monitor/client/internal/app"
	"esp32-monitor/client/internal/config"
	"esp32-monitor/client/internal/ipc"
)

func TestGitHubAccountSwitchDoesNotRestartAgent(t *testing.T) {
	application, err := app.New(config.Config{}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer application.Close()
	socket := filepath.Join(t.TempDir(), "agent.sock")
	server, err := ipc.NewServer(application, ipc.Options{SocketPath: socket})
	if err != nil {
		t.Fatal(err)
	}
	if err = server.Listen(); err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	supervisor := &Supervisor{socket: socket}
	client, err := ipc.Dial(socket)
	if err != nil {
		t.Fatal(err)
	}
	before, err := client.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	next := Settings{GitHub: GitHubSettings{Login: "octocat"}}
	if err := supervisor.UpdateSettings(context.Background(), next); err != nil {
		t.Fatal(err)
	}
	status, err := client.Status(context.Background())
	if err != nil || !status.GitHubEnabled || status.PID != before.PID {
		t.Fatalf("login: %+v %v", status, err)
	}
	next.GitHub.Login = ""
	if err := supervisor.UpdateSettings(context.Background(), next); err != nil {
		t.Fatal(err)
	}
	status, err = client.Status(context.Background())
	if err != nil || status.GitHubEnabled || status.PID != before.PID {
		t.Fatalf("logout: %+v %v", status, err)
	}
	if err := client.SetGitHubLogin(context.Background(), "../bad"); err == nil {
		t.Fatal("invalid login accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { application.Companion.Music.Run(ctx); close(done) }()
	next.Music.Enabled = true
	if err := supervisor.UpdateSettings(context.Background(), next); err != nil {
		t.Fatal(err)
	}
	status, err = client.Status(context.Background())
	if err != nil || !status.MusicEnabled || status.PID != before.PID {
		t.Fatalf("music enabled: %+v %v", status, err)
	}
	next.Music.Enabled = false
	if err := supervisor.UpdateSettings(context.Background(), next); err != nil {
		t.Fatal(err)
	}
	status, err = client.Status(context.Background())
	if err != nil || status.MusicEnabled || status.PID != before.PID {
		t.Fatalf("music disabled: %+v %v", status, err)
	}
	cancel()
	<-done
	taskCtx, taskCancel := context.WithCancel(context.Background())
	taskDone := make(chan struct{})
	go func() { application.Companion.Codex.RunTasks(taskCtx); close(taskDone) }()
	next.Codex.Enabled = true
	if err := supervisor.UpdateSettings(context.Background(), next); err != nil {
		t.Fatal(err)
	}
	status, err = client.Status(context.Background())
	if err != nil || !status.CodexEnabled || status.PID != before.PID {
		t.Fatalf("codex enabled: %+v %v", status, err)
	}
	next.Codex.Enabled = false
	if err := supervisor.UpdateSettings(context.Background(), next); err != nil {
		t.Fatal(err)
	}
	status, err = client.Status(context.Background())
	if err != nil || status.CodexEnabled || status.PID != before.PID {
		t.Fatalf("codex disabled: %+v %v", status, err)
	}
	taskCancel()
	<-taskDone
}
