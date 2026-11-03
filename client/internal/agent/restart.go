package agent

import (
	"errors"
	"flag"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"esp32-monitor/client/internal/ipc"
	"esp32-monitor/client/internal/platform"
)

var restartClientPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

// validateRestartArgs accepts only the flags the GUI is allowed to change.
// Paths that could redirect privileged reads or execute another binary are
// deliberately absent.
func validateRestartArgs(currentSocket string, args []string) error {
	fs := flag.NewFlagSet("monitor-agent-restart", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var socket, uploadURL, tokenFile, companionIP, githubLogin, githubClientID, nativeLibrary, taskAdapter string
	var collector, companion, codex, music bool
	fs.StringVar(&socket, "ipc-socket", "", "")
	fs.BoolVar(&collector, "collector", false, "")
	fs.StringVar(&uploadURL, "collector-upload-url", "", "")
	fs.StringVar(&tokenFile, "collector-upload-token-file", "", "")
	fs.BoolVar(&companion, "companion", false, "")
	fs.StringVar(&companionIP, "companion-ip", "", "")
	fs.StringVar(&githubLogin, "github-user", "", "")
	fs.StringVar(&githubClientID, "github-client-id", "", "")
	fs.BoolVar(&codex, "codex", false, "")
	fs.StringVar(&taskAdapter, "codex-task-adapter", "", "")
	fs.BoolVar(&music, "music", false, "")
	fs.StringVar(&nativeLibrary, "music-native-library", "", "")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return errors.New("restart_args_invalid")
	}
	if socket == "" || socket != currentSocket {
		return errors.New("restart_socket_mismatch")
	}
	if !collector && (uploadURL != "" || tokenFile != "") {
		return errors.New("restart_collector_args_invalid")
	}
	if taskAdapter != "" && taskAdapter != "auto" && taskAdapter != "desktop-ipc" {
		return errors.New("restart_task_adapter_invalid")
	}
	if uploadURL != "" {
		if _, err := platform.ParseHTTPURL(uploadURL); err != nil {
			return errors.New("restart_upload_url_invalid")
		}
	}
	if tokenFile != "" {
		root, err := platform.AppDataDir()
		if err != nil {
			return errors.New("restart_token_path_unavailable")
		}
		clean, err := filepath.Abs(filepath.Clean(tokenFile))
		if err != nil {
			return errors.New("restart_token_path_invalid")
		}
		rel, err := filepath.Rel(root, clean)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
			return errors.New("restart_token_path_invalid")
		}
	}
	if !companion && companionIP != "" {
		return errors.New("restart_companion_args_invalid")
	}
	if companionIP != "" && net.ParseIP(companionIP).To4() == nil {
		return errors.New("restart_companion_ip_invalid")
	}
	if githubLogin != "" && !platform.ValidGitHubLogin(githubLogin) {
		return errors.New("restart_github_login_invalid")
	}
	if githubClientID != "" && !restartClientPattern.MatchString(githubClientID) {
		return errors.New("restart_github_client_invalid")
	}
	if nativeLibrary != "" {
		executable, err := os.Executable()
		if err != nil || nativeLibrary != filepath.Join(filepath.Dir(executable), "libmonitor-media.dylib") {
			return errors.New("restart_native_library_invalid")
		}
	}
	return nil
}

// restartAgent validates the new arguments, spawns its own replacement with
// the same elevation token and then lets this instance exit. No UAC prompt is
// involved: the child inherits the parent's elevated token.
func restartAgent(currentSocket string, args []string, shutdown func(), restarting *atomic.Bool) error {
	if err := validateRestartArgs(currentSocket, args); err != nil {
		return err
	}
	if !restarting.CompareAndSwap(false, true) {
		return errors.New("restart_in_progress")
	}
	if err := spawnRestart(args); err != nil {
		restarting.Store(false)
		return err
	}
	// Give the IPC response time to flush before the listener closes.
	go func() {
		time.Sleep(500 * time.Millisecond)
		shutdown()
	}()
	return nil
}

func spawnRestart(args []string) error {
	executable, err := os.Executable()
	if err != nil {
		return errors.New("restart_executable_unavailable")
	}
	cmd := exec.Command(executable, args...)
	cmd.Env = append(os.Environ(),
		"MONITOR_AGENT_RESTART=1",
		"MONITOR_AGENT_RESTART_FROM="+strconv.Itoa(os.Getpid()),
	)
	cmd.Dir = filepath.Dir(executable)
	cmd.SysProcAttr = restartSysProcAttr()
	if err := cmd.Start(); err != nil {
		return errors.New("restart_spawn_failed")
	}
	return cmd.Process.Release()
}

// waitForPredecessor keeps the replacement from opening the same collector,
// companion and credential state while the previous Agent is still shutting
// down. The old process exits shortly after spawning its successor.
func waitForPredecessor() {
	if os.Getenv("MONITOR_AGENT_RESTART") != "1" {
		return
	}
	pid, err := strconv.Atoi(os.Getenv("MONITOR_AGENT_RESTART_FROM"))
	if err != nil || pid <= 0 {
		return
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if !predecessorAlive(pid) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// listenIPCWithRetry lets a replacement Agent wait for its predecessor to
// release the endpoint. First launches keep the fail-fast behaviour.
func listenIPCWithRetry(server *ipc.Server) error {
	err := server.Listen()
	if err == nil || !errors.Is(err, ipc.ErrAlreadyRunning) || os.Getenv("MONITOR_AGENT_RESTART") != "1" {
		return err
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(200 * time.Millisecond)
		if err = server.Listen(); err == nil {
			return nil
		}
		if !errors.Is(err, ipc.ErrAlreadyRunning) {
			return err
		}
	}
	return err
}
