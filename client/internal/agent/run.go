// Package agent owns the background entry point independently of a WebView window.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"esp32-monitor/client/internal/app"
	"esp32-monitor/client/internal/collector"
	"esp32-monitor/client/internal/companion"
	"esp32-monitor/client/internal/config"
	"esp32-monitor/client/internal/ipc"
	"flag"
	"fmt"
	"io"
	"net"
	"sync/atomic"
	"time"
)

func defaultRouteIPv4() string {
	conn, err := net.Dial("udp4", "8.8.8.8:80")
	if err != nil {
		return ""
	}
	defer conn.Close()
	if addr, ok := conn.LocalAddr().(*net.UDPAddr); ok {
		return addr.IP.String()
	}
	return ""
}

// Run emits one JSON snapshot by default. --watch, --collector, --companion
// and --ipc-socket stay in the background and emit JSONL while services own
// their own schedules.
func Run(ctx context.Context, args []string, out, errOut io.Writer) error {
	// A self-restarted Agent must not open collector/companion state while the
	// previous instance is still shutting down.
	waitForPredecessor()
	runCtx, shutdown := context.WithCancel(ctx)
	defer shutdown()
	fs := flag.NewFlagSet("monitor-agent", flag.ContinueOnError)
	fs.SetOutput(errOut)
	var c config.Config
	var cacheDir string
	var watch bool
	var login bool
	var collectorAPI string
	var collectorScan bool
	var ipcSocket string
	fs.StringVar(&c.GitHub.Login, "github-user", "", "GitHub login; enables contribution calendar using the application credential store")
	fs.StringVar(&c.GitHub.ClientID, "github-client-id", "", "public GitHub OAuth App client ID (device flow must be enabled)")
	fs.BoolVar(&login, "github-login", false, "open the OS default browser for GitHub login, then read the authorized user's calendar")
	fs.BoolVar(&c.Codex.Enabled, "codex", false, "read current Codex App Server account quota")
	fs.StringVar(&c.Codex.Executable, "codex-executable", "codex", "Codex executable path")
	fs.StringVar(&c.Codex.TaskAdapter, "codex-task-adapter", "", "task monitoring adapter: auto or desktop-ipc (explicitly enables task observation)")
	fs.BoolVar(&c.Codex.DesktopEnabled, "codex-desktop", false, "EXPERIMENTAL: passively observe the latest Codex Desktop task using internal IPC v11")
	fs.StringVar(&c.Codex.Home, "codex-home", "", "Codex home directory for desktop IPC (defaults to CODEX_HOME or ~/.codex)")
	fs.Func("codex-thread", "optional override: local desktop task ID to observe (repeatable, max 128); default observes the most recently updated task", func(id string) error { c.Codex.ThreadIDs = append(c.Codex.ThreadIDs, id); return nil })
	fs.StringVar(&cacheDir, "cache-dir", "", "optional private cache directory; calendar only, no credentials")
	fs.BoolVar(&c.Music.Enabled, "music", false, "read system current playback in this user session")
	fs.StringVar(&c.Music.Adapter, "music-adapter", "auto", "playback adapter: auto, mediaremote, gsmtc, mpris")
	fs.StringVar(&c.Music.NativeLibrary, "music-native-library", "", "absolute path to macOS read-only MediaRemote bridge")
	fs.BoolVar(&c.Companion.Enabled, "companion", false, "enable Monitor Companion backend on the selected LAN interface")
	fs.StringVar(&c.Companion.BindIP, "companion-ip", "", "selected LAN IPv4 address; required with --companion")
	fs.StringVar(&c.Companion.StateDirectory, "companion-state-dir", "", "private identity/pairing vault; proxy configuration is never persisted")
	fs.BoolVar(&watch, "watch", false, "emit snapshots as JSONL until interrupted")
	fs.BoolVar(&c.Collector.Enabled, "collector", false, "enable in-process Core/Native collectors and restore the last committed configuration")
	fs.StringVar(&c.Collector.StateDirectory, "collector-state-dir", "", "private collector state and native bridge descriptor directory")
	fs.StringVar(&c.Collector.UploadURL, "collector-upload-url", "", "Server v2 samples URL (.../v2/collector/samples); HTTPS recommended")
	fs.StringVar(&c.Collector.UploadTokenFile, "collector-upload-token-file", "", "private 0600 Server token file; never returned by the GUI API")
	fs.StringVar(&collectorAPI, "collector-api", "", "authenticated native bridge API at 127.0.0.1:PORT; enables collector")
	fs.BoolVar(&collectorScan, "collector-scan", false, "print actual Core/Native collector metric capabilities as JSON and exit")
	fs.StringVar(&ipcSocket, "ipc-socket", "", "user-authenticated Unix socket for the GUI host; implies persistent mode")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("unexpected_arguments")
	}
	c.GitHub.Enabled = c.GitHub.Login != ""
	c.Collector.Enabled = c.Collector.Enabled || collectorAPI != "" || collectorScan
	if c.Companion.Enabled && c.Companion.BindIP == "" {
		// The GUI has no interface picker; without an IP transport.New would
		// reject the whole agent. Pick the default-route IPv4 locally — a UDP
		// "connect" consults the routing table and sends no packets.
		ip := defaultRouteIPv4()
		if ip == "" {
			return errors.New("select_lan_interface")
		}
		c.Companion.BindIP = ip
	}
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
	// Serve the GUI as soon as the application exists; collector restore and
	// discovery keep running behind the IPC endpoint so a settings-driven
	// restart is acknowledged quickly.
	var ipcServer *ipc.Server
	if ipcSocket != "" {
		var restarting atomic.Bool
		opts := ipc.Options{SocketPath: ipcSocket, Shutdown: shutdown}
		opts.Restart = func(args []string) error {
			return restartAgent(ipcSocket, args, shutdown, &restarting)
		}
		configureLoginTask(&opts)
		ipcServer, err = ipc.NewServer(application, opts)
		if err != nil {
			return err
		}
		if err := listenIPCWithRetry(ipcServer); err != nil {
			return err
		}
		defer func() {
			_ = ipcServer.Close()
			_, _ = fmt.Fprintln(errOut, "ipc_closed")
		}()
		go refreshLoginTask()
		_, _ = fmt.Fprintln(errOut, "ipc_listening:", ipcSocket)
	}
	var localAPI *collector.API
	if application.Collector != nil {
		if collectorScan {
			caps, e := application.DiscoverCollectorMetrics(ctx)
			if e != nil {
				return e
			}
			return json.NewEncoder(out).Encode(caps)
		}
		if err := application.Collector.Restore(ctx); err != nil {
			_, _ = fmt.Fprintln(errOut, "collector_restore_failed:", err)
		}
		if application.Collector.Capabilities().SchemaVersion == 0 {
			if _, err := application.DiscoverCollectorMetrics(ctx); err != nil {
				_, _ = fmt.Fprintln(errOut, "collector_discovery_failed:", err)
			}
		}
		if collectorAPI != "" {
			localAPI, err = application.Collector.ListenAPI(collectorAPI)
			if err != nil {
				return err
			}
			defer localAPI.Close()
		}
	}
	service := application.Companion
	writeSnapshot := func() error {
		if application.Collector == nil {
			return json.NewEncoder(out).Encode(service.Snapshot())
		}
		return json.NewEncoder(out).Encode(struct {
			companion.Snapshot
			Collector collector.Snapshot `json:"collector"`
		}{Snapshot: service.Snapshot(), Collector: application.Collector.Snapshot()})
	}
	// Provider requests execute on a worker; the snapshot loop stays responsive.
	done := make(chan struct{})
	wake := make(chan struct{}, 1)
	workerCtx, cancel := context.WithCancel(runCtx)
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
	if !watch && !c.Companion.Enabled && collectorAPI == "" && !c.Collector.Enabled && ipcServer == nil {
		// A one-shot must wait for its requested reads rather than return starting.
		service.Refresh(ctx)
		taskCtx, taskCancel := context.WithTimeout(ctx, 8*time.Second)
		service.Codex.WaitTasks(taskCtx)
		service.Music.WaitReady(taskCtx)
		taskCancel()
		if ctx.Err() != nil {
			return nil
		}
		return writeSnapshot()
	}
	wake <- struct{}{}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		if err := writeSnapshot(); err != nil {
			return errors.New("snapshot_write_failed")
		}
		select {
		case <-runCtx.Done():
			return nil
		case <-ticker.C:
		}
		select {
		case wake <- struct{}{}:
		default:
		}
	}
}
