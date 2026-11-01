// Package agent owns the background entry point independently of a WebView window.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"esp32-monitor/client/internal/app"
	"esp32-monitor/client/internal/collector"
	"esp32-monitor/client/internal/companion"
	"esp32-monitor/client/internal/ipc"
	"esp32-monitor/client/internal/logging"
	"fmt"
	"io"
	"os"
	"sync/atomic"
	"time"
)

// Run emits one JSON snapshot by default. --watch, --collector, --companion
// and --ipc-socket stay in the background and emit JSONL while services own
// their own schedules.
func Run(ctx context.Context, args []string, out, errOut io.Writer) error {
	// A self-restarted Agent must not open collector/companion state while the
	// previous instance is still shutting down.
	waitForPredecessor()
	runCtx, shutdown := context.WithCancel(ctx)
	defer shutdown()
	o, err := parseOptions(args, errOut)
	if err != nil {
		return err
	}
	c := o.config
	log := logging.L("agent")
	log.Info("agent_start", "pid", os.Getpid(), "collector", c.Collector.Enabled, "companion", c.Companion.Enabled,
		"codex", c.Codex.Enabled, "music", c.Music.Enabled, "github", c.GitHub.Enabled, "ipc", o.ipcSocket != "")
	// A GUI-capable Agent keeps the collector Manager prepared even when the
	// Settings switch is off, so enabling collection stays a hot update.
	application, err := app.NewWithOptions(c, o.cacheDir, app.Options{PrepareCollector: o.ipcSocket != "", CollectorAPI: o.collectorAPI})
	if err != nil {
		return err
	}
	defer func() {
		if application != nil {
			_ = application.Close()
		}
	}()
	if o.login {
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
		application, err = app.New(c, o.cacheDir)
		if err != nil {
			return err
		}
	}
	// Serve the GUI as soon as the application exists; collector restore and
	// discovery keep running behind the IPC endpoint so a settings-driven
	// restart is acknowledged quickly.
	var ipcServer *ipc.Server
	if o.ipcSocket != "" {
		var restarting atomic.Bool
		opts := ipc.Options{SocketPath: o.ipcSocket, Shutdown: shutdown}
		opts.Restart = func(args []string) error {
			return restartAgent(o.ipcSocket, args, shutdown, &restarting)
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
		log.Info("ipc_listening")
		_, _ = fmt.Fprintln(errOut, "ipc_listening:", o.ipcSocket)
	}
	var localAPI *collector.API
	if manager := application.Collector(); manager != nil {
		if o.collectorScan {
			caps, e := application.DiscoverCollectorMetrics(ctx)
			if e != nil {
				return e
			}
			return json.NewEncoder(out).Encode(caps)
		}
		if application.CollectorEnabled() {
			if err := manager.Restore(ctx); err != nil {
				log.Warn("collector_restore_failed", "error", err)
				_, _ = fmt.Fprintln(errOut, "collector_restore_failed:", err)
			}
		} else if err := manager.Load(); err != nil {
			log.Warn("collector_load_failed", "error", err)
			_, _ = fmt.Fprintln(errOut, "collector_load_failed:", err)
		}
		// Warm capabilities even while paused so a Settings enable starts
		// without paying a fresh hardware discovery on the request path.
		if manager.Capabilities().SchemaVersion == 0 {
			if _, err := manager.Discover(ctx); err != nil {
				log.Warn("collector_discovery_failed", "error", err)
				_, _ = fmt.Fprintln(errOut, "collector_discovery_failed:", err)
			}
		}
		if o.collectorConfig != "" && application.CollectorEnabled() && manager.Snapshot().Revision == 0 {
			data, err := os.ReadFile(o.collectorConfig)
			if err != nil {
				return err
			}
			var config collector.Config
			if err := json.Unmarshal(data, &config); err != nil {
				return errors.New("invalid_collector_config_file")
			}
			if _, err := manager.Apply(ctx, config); err != nil {
				return err
			}
		}
		if o.collectorAPI != "" {
			localAPI, err = manager.ListenAPI(o.collectorAPI)
			if err != nil {
				return err
			}
			defer localAPI.Close()
		}
	}
	service := application.Companion
	writeSnapshot := func() error {
		manager := application.Collector()
		if manager == nil || !application.CollectorEnabled() {
			return json.NewEncoder(out).Encode(service.Snapshot())
		}
		return json.NewEncoder(out).Encode(struct {
			companion.Snapshot
			Collector collector.Snapshot `json:"collector"`
		}{Snapshot: service.Snapshot(), Collector: manager.Snapshot()})
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
	if !o.watch && !c.Companion.Enabled && o.collectorAPI == "" && !c.Collector.Enabled && ipcServer == nil {
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
