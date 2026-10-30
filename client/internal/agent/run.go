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
	"net"
	"os"
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
	o, err := parseOptions(args, errOut)
	if err != nil {
		return err
	}
	c := o.config
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
	log := logging.L("agent")
	log.Info("agent_start", "pid", os.Getpid(), "collector", c.Collector.Enabled, "companion", c.Companion.Enabled,
		"codex", c.Codex.Enabled, "music", c.Music.Enabled, "github", c.GitHub.Enabled, "ipc", o.ipcSocket != "")
	application, err := app.New(c, o.cacheDir)
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
	if application.Collector != nil {
		if o.collectorScan {
			caps, e := application.DiscoverCollectorMetrics(ctx)
			if e != nil {
				return e
			}
			return json.NewEncoder(out).Encode(caps)
		}
		if err := application.Collector.Restore(ctx); err != nil {
			log.Warn("collector_restore_failed", "error", err)
			_, _ = fmt.Fprintln(errOut, "collector_restore_failed:", err)
		}
		if application.Collector.Capabilities().SchemaVersion == 0 {
			if _, err := application.DiscoverCollectorMetrics(ctx); err != nil {
				log.Warn("collector_discovery_failed", "error", err)
				_, _ = fmt.Fprintln(errOut, "collector_discovery_failed:", err)
			}
		}
		if o.collectorAPI != "" {
			localAPI, err = application.Collector.ListenAPI(o.collectorAPI)
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
