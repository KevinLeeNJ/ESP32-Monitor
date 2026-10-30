package agent

import (
	"errors"
	"flag"
	"io"

	"esp32-monitor/client/internal/config"
)

type options struct {
	config        config.Config
	cacheDir      string
	watch         bool
	login         bool
	collectorAPI  string
	collectorScan bool
	ipcSocket     string
}

func parseOptions(args []string, errOut io.Writer) (options, error) {
	var o options
	fs := flag.NewFlagSet("monitor-agent", flag.ContinueOnError)
	fs.SetOutput(errOut)
	c := &o.config
	fs.StringVar(&c.GitHub.Login, "github-user", "", "GitHub login; enables contribution calendar using the application credential store")
	fs.StringVar(&c.GitHub.ClientID, "github-client-id", "", "public GitHub OAuth App client ID (device flow must be enabled)")
	fs.BoolVar(&o.login, "github-login", false, "open the OS default browser for GitHub login, then read the authorized user's calendar")
	fs.BoolVar(&c.Codex.Enabled, "codex", false, "read current Codex App Server account quota")
	fs.StringVar(&c.Codex.Executable, "codex-executable", "codex", "Codex executable path")
	fs.StringVar(&c.Codex.TaskAdapter, "codex-task-adapter", "", "task monitoring adapter: auto or desktop-ipc (explicitly enables task observation)")
	fs.BoolVar(&c.Codex.DesktopEnabled, "codex-desktop", false, "EXPERIMENTAL: passively observe the latest Codex Desktop task using internal IPC v11")
	fs.StringVar(&c.Codex.Home, "codex-home", "", "Codex home directory for desktop IPC (defaults to CODEX_HOME or ~/.codex)")
	fs.Func("codex-thread", "optional override: local desktop task ID to observe (repeatable, max 128); default observes the most recently updated task", func(id string) error { c.Codex.ThreadIDs = append(c.Codex.ThreadIDs, id); return nil })
	fs.StringVar(&o.cacheDir, "cache-dir", "", "optional private cache directory; calendar only, no credentials")
	fs.BoolVar(&c.Music.Enabled, "music", false, "read system current playback in this user session")
	fs.StringVar(&c.Music.Adapter, "music-adapter", "auto", "playback adapter: auto, mediaremote, gsmtc, mpris")
	fs.StringVar(&c.Music.NativeLibrary, "music-native-library", "", "absolute path to macOS read-only MediaRemote bridge")
	fs.BoolVar(&c.Companion.Enabled, "companion", false, "enable Monitor Companion backend on the selected LAN interface")
	fs.StringVar(&c.Companion.BindIP, "companion-ip", "", "selected LAN IPv4 address; required with --companion")
	fs.StringVar(&c.Companion.StateDirectory, "companion-state-dir", "", "private identity/pairing vault; proxy configuration is never persisted")
	fs.BoolVar(&o.watch, "watch", false, "emit snapshots as JSONL until interrupted")
	fs.BoolVar(&c.Collector.Enabled, "collector", false, "enable in-process Core/Native collectors and restore the last committed configuration")
	fs.StringVar(&c.Collector.StateDirectory, "collector-state-dir", "", "private collector state and native bridge descriptor directory")
	fs.StringVar(&c.Collector.UploadURL, "collector-upload-url", "", "Server v2 samples URL (.../v2/collector/samples); HTTPS recommended")
	fs.StringVar(&c.Collector.UploadTokenFile, "collector-upload-token-file", "", "private 0600 Server token file; never returned by the GUI API")
	fs.StringVar(&o.collectorAPI, "collector-api", "", "authenticated native bridge API at 127.0.0.1:PORT; enables collector")
	fs.BoolVar(&o.collectorScan, "collector-scan", false, "print actual Core/Native collector metric capabilities as JSON and exit")
	fs.StringVar(&o.ipcSocket, "ipc-socket", "", "user-authenticated Unix socket for the GUI host; implies persistent mode")
	if err := fs.Parse(args); err != nil {
		return o, err
	}
	if fs.NArg() != 0 {
		return o, errors.New("unexpected_arguments")
	}
	o.config.GitHub.Enabled = o.config.GitHub.Login != ""
	o.config.Collector.Enabled = o.config.Collector.Enabled || o.collectorAPI != "" || o.collectorScan
	return o, nil
}
