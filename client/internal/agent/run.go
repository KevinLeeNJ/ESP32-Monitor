// Package agent owns the background entry point independently of a WebView window.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"esp32-monitor/client/internal/app"
	"esp32-monitor/client/internal/config"
	"flag"
	"io"
	"time"
)

// Run emits one JSON snapshot by default. --watch emits JSONL every two seconds,
// while each provider independently applies its polling and retry schedule.
func Run(ctx context.Context, args []string, out, errOut io.Writer) error {
	fs := flag.NewFlagSet("monitor-agent", flag.ContinueOnError)
	fs.SetOutput(errOut)
	var c config.Config
	var cacheDir string
	var watch bool
	var login bool
	fs.StringVar(&c.GitHub.Login, "github-user", "", "GitHub login; enables contribution calendar using the application credential store")
	fs.StringVar(&c.GitHub.ClientID, "github-client-id", "", "public GitHub OAuth App client ID (device flow must be enabled)")
	fs.BoolVar(&login, "github-login", false, "open the OS default browser for GitHub login, then read the authorized user's calendar")
	fs.BoolVar(&c.Codex.Enabled, "codex", false, "read current Codex App Server account quota")
	fs.StringVar(&c.Codex.Executable, "codex-executable", "codex", "Codex executable path")
	fs.StringVar(&c.Codex.TaskAdapter, "codex-task-adapter", "", "task monitoring adapter: auto or desktop-ipc (explicitly enables task observation)")
	fs.BoolVar(&c.Codex.DesktopEnabled, "codex-desktop", false, "EXPERIMENTAL: passively observe selected Codex Desktop tasks using internal IPC v11")
	fs.StringVar(&c.Codex.Home, "codex-home", "", "Codex home directory for desktop IPC (defaults to CODEX_HOME or ~/.codex)")
	fs.Func("codex-thread", "local desktop task ID to observe (repeatable, max 128)", func(id string) error { c.Codex.ThreadIDs = append(c.Codex.ThreadIDs, id); return nil })
	fs.StringVar(&cacheDir, "cache-dir", "", "optional private cache directory; calendar only, no credentials")
	fs.BoolVar(&c.Music.Enabled, "music", false, "read system current playback in this user session")
	fs.StringVar(&c.Music.Adapter, "music-adapter", "auto", "playback adapter: auto, mediaremote, gsmtc, mpris")
	fs.StringVar(&c.Music.NativeLibrary, "music-native-library", "", "absolute path to macOS read-only MediaRemote bridge")
	fs.BoolVar(&c.Companion.Enabled, "companion", false, "enable Monitor Companion backend on the selected LAN interface")
	fs.StringVar(&c.Companion.BindIP, "companion-ip", "", "selected LAN IPv4 address; required with --companion")
	fs.StringVar(&c.Companion.StateDirectory, "companion-state-dir", "", "private identity/pairing vault; proxy configuration is never persisted")
	fs.BoolVar(&watch, "watch", false, "emit snapshots as JSONL until interrupted")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("unexpected_arguments")
	}
	c.GitHub.Enabled = c.GitHub.Login != ""
	application, err := app.New(c, cacheDir)
	if err != nil {
		return err
	}
	defer func() {
		if application != nil {
			_ = application.Close()
		}
	}()
	if login {
		session, err := application.GitHubAuth.StartLogin(ctx)
		if err != nil {
			return err
		}
		if err := json.NewEncoder(errOut).Encode(session); err != nil {
			application.GitHubAuth.CancelLogin(session.ID)
			return errors.New("login_status_write_failed")
		}
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for session.State == "awaiting_authorization" {
			select {
			case <-ctx.Done():
				application.GitHubAuth.CancelLogin(session.ID)
				return nil
			case <-ticker.C:
			}
			session, err = application.GitHubAuth.PollLogin(ctx, session.ID)
			if err != nil && session.State != "awaiting_authorization" {
				return err
			}
		}
		if session.State != "authorized" {
			return errors.New("github_login_" + string(session.State))
		}
		c.GitHub.Login = session.Login
		c.GitHub.Enabled = true
		_ = application.Close()
		application, err = app.New(c, cacheDir)
		if err != nil {
			return err
		}
	}
	service := application.Companion
	encoder := json.NewEncoder(out)
	// Provider requests execute on a worker; the snapshot loop stays responsive.
	done := make(chan struct{})
	wake := make(chan struct{}, 1)
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	musicDone := make(chan struct{})
	go func() { defer close(musicDone); service.Music.Run(workerCtx) }()
	tasksDone := make(chan struct{})
	go func() { defer close(tasksDone); service.Codex.RunTasks(workerCtx) }()
	go func() {
		defer close(done)
		for {
			select {
			case <-workerCtx.Done():
				return
			case <-wake:
				service.Refresh(workerCtx)
			}
		}
	}()
	defer func() { cancel(); <-done; <-tasksDone; <-musicDone }()
	if !watch && !c.Companion.Enabled {
		// A one-shot must wait for its requested reads rather than return starting.
		service.Refresh(ctx)
		taskCtx, taskCancel := context.WithTimeout(ctx, 8*time.Second)
		service.Codex.WaitTasks(taskCtx)
		service.Music.WaitReady(taskCtx)
		taskCancel()
		if ctx.Err() != nil {
			return nil
		}
		return encoder.Encode(service.Snapshot())
	}
	wake <- struct{}{}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		if err := encoder.Encode(service.Snapshot()); err != nil {
			return errors.New("snapshot_write_failed")
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
		select {
		case wake <- struct{}{}:
		default:
		}
	}
}
