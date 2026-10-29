package guibridge

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"esp32-monitor/client/internal/ipc"
)

// Supervisor owns the Agent child process. It spawns the binary with settings
// derived flags, restarts it with capped backoff after crashes, and performs
// graceful restarts when settings change.
type Supervisor struct {
	mu        sync.Mutex
	settings  Settings
	socket    string
	agentPath string

	manageCtx    context.Context
	manageCancel context.CancelFunc
	manageDone   chan struct{}
	stopOnce     sync.Once
	stopReq      chan struct{}

	current *currentAgent
	// external means Windows started the Agent through UAC or Task Scheduler.
	// The GUI intentionally never inherits that token.
	external bool
	stopped  bool

	restarts  int
	lastError error

	// restartMu serializes settings-driven external restarts. The save call
	// returns after the Agent acknowledges the request; readiness is tracked in
	// the background so a queued save waits for the replacement instead of
	// racing the old process.
	restartMu       sync.Mutex
	restartInFlight bool
	restartOldPID   int
}

type currentAgent struct {
	cmd    *exec.Cmd
	cancel context.CancelFunc
}

// NewSupervisor validates the agent binary location and socket path.
func NewSupervisor(settings Settings, socketPath string) (*Supervisor, error) {
	agentPath, err := resolveAgentPath()
	if err != nil {
		return nil, err
	}
	return &Supervisor{settings: settings, socket: socketPath, agentPath: agentPath}, nil
}

func resolveAgentPath() (string, error) {
	if env := os.Getenv("MONITOR_AGENT_BIN"); env != "" {
		if _, err := os.Stat(env); err == nil {
			return env, nil
		}
	}
	executable, err := os.Executable()
	if err == nil {
		candidate := filepath.Join(filepath.Dir(executable), agentBinaryName())
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	candidate := filepath.Join("bin", agentBinaryName())
	if _, err := os.Stat(candidate); err == nil {
		return candidate, nil
	}
	return "", errors.New("monitor_agent_binary_not_found")
}

// CurrentSettings returns the active settings snapshot.
func (s *Supervisor) CurrentSettings() Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.settings
}

// Start launches the management loop and waits until the IPC socket answers.
func (s *Supervisor) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.manageDone != nil {
		s.mu.Unlock()
		return errors.New("supervisor_already_started")
	}
	s.manageCtx, s.manageCancel = context.WithCancel(context.WithoutCancel(ctx))
	s.manageDone = make(chan struct{})
	s.stopReq = make(chan struct{})
	s.mu.Unlock()
	// A logon task may already have started the elevated Windows Agent. Reuse
	// it instead of spawning a second process (and a second UAC prompt).
	if s.socketReady(ctx, 800*time.Millisecond) {
		s.mu.Lock()
		s.external = true
		s.mu.Unlock()
		return nil
	}
	if handled, err := s.startPlatformAgent(ctx); handled {
		if err != nil {
			s.setLast(err)
			return err
		}
		s.mu.Lock()
		s.external = true
		s.mu.Unlock()
		if err := s.WaitSocket(ctx, 15*time.Second); err != nil {
			s.setLast(err)
			return err
		}
		return nil
	}
	go s.manage()
	if err := s.WaitSocket(ctx, 15*time.Second); err != nil {
		return err
	}
	return nil
}

func (s *Supervisor) socketReady(ctx context.Context, timeout time.Duration) bool {
	probe, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return s.WaitSocket(probe, timeout) == nil
}

// manage spawns the Agent, waits for exit and restarts after crashes.
func (s *Supervisor) manage() {
	defer close(s.manageDone)
	graceful := false
	for attempt := 0; ; attempt++ {
		if s.isStopped() {
			return
		}
		if attempt > 0 && !graceful {
			backoff := min(time.Duration(1<<min(attempt, 5))*time.Second, 32*time.Second)
			select {
			case <-s.manageCtx.Done():
				return
			case <-s.stopReq:
				return
			case <-time.After(backoff):
			}
		}
		graceful = false
		childCtx, cancel := context.WithCancel(s.manageCtx)
		cmd := exec.CommandContext(childCtx, s.agentPath, s.agentArgs()...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			cancel()
			s.setLast(fmt.Errorf("agent_start_failed: %w", err))
			continue
		}
		s.mu.Lock()
		s.current = &currentAgent{cmd: cmd, cancel: cancel}
		s.mu.Unlock()
		waitCh := make(chan error, 1)
		go func() {
			waitCh <- cmd.Wait()
			cancel()
		}()
		var exitErr error
		select {
		case exitErr = <-waitCh:
		case <-s.stopReq:
			s.stopChild(5 * time.Second)
			exitErr = <-waitCh
		case <-s.manageCtx.Done():
			s.stopChild(5 * time.Second)
			exitErr = <-waitCh
		}
		s.mu.Lock()
		s.current = nil
		s.mu.Unlock()
		if s.isStopped() || s.manageCtx.Err() != nil {
			return
		}
		if exitErr != nil {
			s.setLast(fmt.Errorf("agent_exited: %v", exitErr))
		}
		_ = exitErr
	}
}

// stopChild interrupts the running Agent and escalates to kill on timeout.
func (s *Supervisor) stopChild(grace time.Duration) {
	s.mu.Lock()
	current := s.current
	s.mu.Unlock()
	if current == nil || current.cmd.Process == nil {
		return
	}
	_ = s.terminateAgent(current.cmd.Process)
	deadline := time.After(grace)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-deadline:
			_ = current.cmd.Process.Kill()
			return
		case <-ticker.C:
			if current.cmd.ProcessState != nil {
				return
			}
			if !processAlive(current.cmd.Process) {
				return
			}
		}
	}
}

func (s *Supervisor) agentArgs() []string {
	s.mu.Lock()
	settings := s.settings
	s.mu.Unlock()
	return s.agentArgsFor(settings)
}

func (s *Supervisor) agentArgsFor(settings Settings) []string {
	args := []string{"--ipc-socket", s.socket}
	c := settings.Collector
	if c.Enabled {
		args = append(args, "--collector")
		if c.UploadURL != "" {
			args = append(args, "--collector-upload-url", c.UploadURL)
		}
		if c.UploadTokenFile != "" {
			args = append(args, "--collector-upload-token-file", c.UploadTokenFile)
		}
	}
	if settings.Companion.Enabled {
		args = append(args, "--companion")
		if settings.Companion.BindIP != "" {
			args = append(args, "--companion-ip", settings.Companion.BindIP)
		}
	}
	if settings.GitHub.Login != "" {
		args = append(args, "--github-user", settings.GitHub.Login)
	}
	if settings.GitHub.ClientID != "" {
		args = append(args, "--github-client-id", settings.GitHub.ClientID)
	}
	if settings.Codex.Enabled {
		args = append(args, "--codex")
	}
	if settings.Music.Enabled {
		args = append(args, "--music")
		if library := s.nativeLibraryPath(); library != "" {
			args = append(args, "--music-native-library", library)
		}
	}
	return args
}

// nativeLibraryPath locates the MediaRemote bridge dylib next to the Agent.
func (s *Supervisor) nativeLibraryPath() string {
	candidate := filepath.Join(filepath.Dir(s.agentPath), "libmonitor-media.dylib")
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	return ""
}

// WaitSocket blocks until the Agent IPC socket answers a Status call.
func (s *Supervisor) WaitSocket(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		client, err := ipc.Dial(s.socket)
		if err == nil {
			_, statusErr := client.Status(ctx)
			_ = client.Close()
			if statusErr == nil {
				s.mu.Lock()
				s.restarts = 0
				s.lastError = nil
				s.mu.Unlock()
				return nil
			}
		}
		if time.Now().After(deadline) {
			return errors.New("agent_socket_unreachable")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(150 * time.Millisecond):
		}
	}
}

// Call runs fn with a fresh IPC client connection.
func (s *Supervisor) Call(fn func(client *ipc.Client) error) error {
	client, err := ipc.Dial(s.socket)
	if err != nil {
		return fmt.Errorf("ipc_unavailable: %w", err)
	}
	defer client.Close()
	return fn(client)
}

// Restart relaunches the Agent with the current settings. An externally
// started (elevated) Agent restarts itself over IPC, inheriting its own token,
// so settings changes never trigger another UAC prompt. A managed child is
// stopped and relaunched by the management loop.
func (s *Supervisor) Restart(ctx context.Context) error {
	s.mu.Lock()
	external := s.external
	managed := s.current != nil
	s.mu.Unlock()
	if managed {
		s.stopChild(5 * time.Second)
		return s.WaitSocket(ctx, 20*time.Second)
	}
	if external {
		return s.restartExternal(ctx)
	}
	return errors.New("agent_not_running")
}

func (s *Supervisor) restartExternal(ctx context.Context) error {
	s.restartMu.Lock()
	defer s.restartMu.Unlock()
	if s.restartInFlight {
		// A save moments ago already replaced the Agent. Wait for the new
		// process before sending another restart request.
		err := s.waitNewPID(ctx, s.restartOldPID, 40*time.Second)
		s.restartInFlight = false
		if err != nil {
			s.setLast(err)
			return err
		}
	}
	client, err := ipc.Dial(s.socket)
	if err != nil {
		return fmt.Errorf("ipc_unavailable: %w", err)
	}
	defer client.Close()
	callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	status, err := client.Status(callCtx)
	if err != nil {
		cancel()
		return err
	}
	err = client.Restart(callCtx, s.agentArgs())
	cancel()
	if err != nil {
		return err
	}
	s.restartOldPID = status.PID
	s.restartInFlight = true
	// Saving settings must feel immediate; the replacement serves while
	// collectors warm up, and readiness is observed in the background.
	go func(oldPID int) {
		waitCtx, waitCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer waitCancel()
		err := s.waitNewPID(waitCtx, oldPID, 60*time.Second)
		s.restartMu.Lock()
		if s.restartOldPID == oldPID {
			s.restartInFlight = false
		}
		s.restartMu.Unlock()
		if err != nil {
			s.setLast(err)
		}
	}(status.PID)
	return nil
}

// awaitRestart blocks until a restart requested by an earlier save has
// produced a reachable Agent.
func (s *Supervisor) awaitRestart(ctx context.Context) error {
	s.restartMu.Lock()
	inFlight, oldPID := s.restartInFlight, s.restartOldPID
	s.restartMu.Unlock()
	if !inFlight {
		return nil
	}
	if err := s.waitNewPID(ctx, oldPID, 40*time.Second); err != nil {
		s.setLast(err)
		return err
	}
	return nil
}

// waitNewPID confirms the replacement Agent owns the endpoint. The old
// process keeps serving briefly after the restart acknowledgement, so socket
// availability alone would wrongly report success.
func (s *Supervisor) waitNewPID(ctx context.Context, oldPID int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		client, err := ipc.Dial(s.socket)
		if err == nil {
			status, statusErr := client.Status(probeCtx)
			_ = client.Close()
			if statusErr == nil && status.PID != 0 && status.PID != oldPID {
				cancel()
				s.mu.Lock()
				s.restarts = 0
				s.lastError = nil
				s.mu.Unlock()
				return nil
			}
		}
		cancel()
		if time.Now().After(deadline) {
			return errors.New("agent_restart_timeout")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(150 * time.Millisecond):
		}
	}
}

// SetLoginTask creates or removes the highest-privilege logon task through the
// elevated Agent, so toggling launch-at-login needs no UAC prompt.
func (s *Supervisor) SetLoginTask(ctx context.Context, enabled bool) error {
	if err := s.awaitRestart(ctx); err != nil {
		return err
	}
	return s.Call(func(client *ipc.Client) error {
		callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		return client.SetLoginTask(callCtx, enabled)
	})
}

// UpdateSettings replaces settings and restarts the Agent only when the
// settings that decide its flags changed. UI-only preferences keep it alive.
func (s *Supervisor) UpdateSettings(ctx context.Context, settings Settings) error {
	s.mu.Lock()
	restart := !slices.Equal(s.agentArgsFor(s.settings), s.agentArgsFor(settings))
	s.settings = settings
	s.mu.Unlock()
	if !restart {
		return nil
	}
	return s.Restart(ctx)
}

// Stop terminates the Agent and the management loop.
func (s *Supervisor) Stop() {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	s.stopped = true
	done := s.manageDone
	external := s.external
	s.mu.Unlock()
	if external {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if client, err := ipc.Dial(s.socket); err == nil {
			_ = client.Quit(ctx)
			_ = client.Close()
		}
		return
	}
	s.stopOnce.Do(func() { close(s.stopReq) })
	s.stopChild(5 * time.Second)
	if s.manageCancel != nil {
		s.manageCancel()
	}
	if done == nil {
		return
	}
	select {
	case <-done:
	case <-time.After(8 * time.Second):
	}
}

func (s *Supervisor) isStopped() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopped
}

func (s *Supervisor) setLast(err error) {
	s.mu.Lock()
	s.lastError = err
	s.restarts++
	s.mu.Unlock()
}

// LastError reports the most recent supervisor failure for the GUI.
func (s *Supervisor) LastError() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastError
}
