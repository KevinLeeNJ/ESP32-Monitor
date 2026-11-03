// Package app is the composition root shared by the user-session Agent and future GUI bindings.
package app

import (
	"context"
	"errors"
	githubauth "esp32-monitor/client/internal/auth/github"
	"esp32-monitor/client/internal/collector"
	"esp32-monitor/client/internal/companion"
	"esp32-monitor/client/internal/config"
	"esp32-monitor/client/internal/nowplaying"
	"esp32-monitor/client/internal/nowplaying/adapters"
	"esp32-monitor/client/internal/platform"
	"esp32-monitor/client/internal/providers/codex"
	"esp32-monitor/client/internal/providers/github"
	"esp32-monitor/client/internal/transport"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Options controls Agent-only services that the GUI can enable later.
type Options struct {
	// PrepareCollector keeps a collector Manager alive even while collection is
	// disabled, so a Settings change can enable it without rebuilding state.
	PrepareCollector bool
	// CollectorAPI exposes the local native-bridge API (CLI/e2e only).
	CollectorAPI string
	// Transport address overrides are used by tests; empty selects defaults.
	MQTTAddress string
	HTTPAddress string
}

type Application struct {
	Companion  *companion.Service
	GitHubAuth *githubauth.Client

	mu               sync.RWMutex
	cfg              config.Config
	cacheDir         string
	monitor          *transport.Backend
	collector        *collector.Manager
	collectorEnabled bool
	closed           bool

	// ops serializes service lifecycle changes.
	ops          sync.Mutex
	collectorAPI string
	mqttAddress  string
	httpAddress  string
}

func New(c config.Config, cacheDir string) (*Application, error) {
	return NewWithOptions(c, cacheDir, Options{})
}

func NewWithOptions(c config.Config, cacheDir string, opts Options) (*Application, error) {
	localURL, err := transport.NormalizeLocalDevicesURL(c.Companion.LocalDevicesURL)
	if err != nil {
		return nil, err
	}
	c.Companion.LocalDevicesURL = localURL
	cachePath := ""
	if cacheDir != "" && c.GitHub.Enabled {
		cachePath = filepath.Join(cacheDir, "github-"+c.GitHub.Login+".json")
	}
	auth := githubauth.New(githubauth.Options{ClientID: c.GitHub.ClientID, Store: platform.GitHubCredentials{}, OpenBrowser: platform.OpenBrowser})
	githubProvider, err := github.New(github.Options{Enabled: c.GitHub.Enabled, Login: c.GitHub.Login, Token: auth.Token, CachePath: cachePath})
	if err != nil {
		return nil, err
	}
	music, err := adapters.NewSwitchable(adapters.Options{Enabled: c.Music.Enabled, Adapter: c.Music.Adapter, NativeLibrary: c.Music.NativeLibrary})
	if err != nil {
		return nil, err
	}
	application := &Application{
		GitHubAuth:       auth,
		Companion:        &companion.Service{Music: music, GitHub: githubProvider, Codex: codex.NewSwitchable(codex.Options{Enabled: c.Codex.Enabled, Executable: c.Codex.Executable, DesktopEnabled: c.Codex.DesktopEnabled, TaskAdapter: c.Codex.TaskAdapter, Home: c.Codex.Home, ThreadIDs: c.Codex.ThreadIDs})},
		cacheDir:         cacheDir,
		collectorAPI:     opts.CollectorAPI,
		mqttAddress:      opts.MQTTAddress,
		httpAddress:      opts.HTTPAddress,
		collectorEnabled: c.Collector.Enabled,
	}
	// Resolve persistent directories once so later Settings changes reuse them.
	if c.Collector.StateDirectory == "" {
		base, e := platform.AppDataDir()
		if e != nil {
			return nil, e
		}
		c.Collector.StateDirectory = filepath.Join(base, "collector")
	}
	if c.Companion.StateDirectory == "" {
		base, e := platform.AppDataDir()
		if e != nil {
			return nil, e
		}
		c.Companion.StateDirectory = filepath.Join(base, "credentials", "companion")
	}
	application.cfg = c
	if c.Companion.Enabled {
		backend, e := application.startCompanion(c.Companion.BindIP)
		if e != nil {
			return nil, e
		}
		application.monitor = backend
	}
	if c.Collector.Enabled || opts.PrepareCollector {
		manager, e := collector.New(collector.Options{StateDirectory: c.Collector.StateDirectory, UploadURL: c.Collector.UploadURL, UploadToken: collector.TokenFile(c.Collector.UploadTokenFile)})
		if e != nil {
			_ = application.Close()
			return nil, e
		}
		application.collector = manager
		if !c.Collector.Enabled {
			manager.SetCollecting(false)
		}
	}
	return application, nil
}

// Collector returns the current manager, which may be paused but prepared.
func (a *Application) Collector() *collector.Manager {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.collector
}

// Monitor returns the current Companion backend, or nil when disabled.
func (a *Application) Monitor() *transport.Backend {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.monitor
}

// CollectorEnabled reports the Settings-controlled collection state.
func (a *Application) CollectorEnabled() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.collectorEnabled
}

// SetCollectorSettings applies collection state and the upload target in place.
// The manager, its state-directory lock and its persisted selection survive.
func (a *Application) SetCollectorSettings(enabled bool, uploadURL, tokenFile string) error {
	a.ops.Lock()
	defer a.ops.Unlock()
	a.mu.RLock()
	manager, previous, closed := a.collector, a.cfg.Collector, a.closed
	a.mu.RUnlock()
	if closed {
		return errors.New("application_closed")
	}
	if manager == nil {
		return errors.New("collector_unavailable")
	}
	if previous.UploadURL != uploadURL || previous.UploadTokenFile != tokenFile {
		if err := manager.SetUploadTarget(uploadURL, tokenFile); err != nil {
			return err
		}
	}
	manager.SetCollecting(enabled)
	a.mu.Lock()
	a.cfg.Collector.Enabled = enabled
	a.cfg.Collector.UploadURL = uploadURL
	a.cfg.Collector.UploadTokenFile = tokenFile
	a.collectorEnabled = enabled
	a.mu.Unlock()
	return nil
}

// SetCompanionSettings starts, stops or rebinds the Companion backend in place.
func (a *Application) SetCompanionSettings(enabled bool, bindIP, localURL string) error {
	localURL, err := transport.NormalizeLocalDevicesURL(localURL)
	if err != nil {
		return err
	}
	a.ops.Lock()
	defer a.ops.Unlock()
	a.mu.RLock()
	backend, closed, previousURL := a.monitor, a.closed, a.cfg.Companion.LocalDevicesURL
	a.mu.RUnlock()
	if closed {
		return errors.New("application_closed")
	}
	if !enabled {
		if backend != nil {
			_ = backend.Close()
		}
		a.mu.Lock()
		a.monitor = nil
		a.cfg.Companion.Enabled = false
		a.cfg.Companion.LocalDevicesURL = localURL
		a.mu.Unlock()
		return nil
	}
	if backend != nil {
		if bindIP != "" {
			if err := backend.UpdateLANInterface(bindIP); err != nil {
				return err
			}
		}
		a.mu.Lock()
		a.cfg.Companion.Enabled = true
		a.cfg.Companion.BindIP = bindIP
		a.cfg.Companion.LocalDevicesURL = localURL
		a.mu.Unlock()
		if previousURL != localURL {
			backend.InvalidateProxySessions()
		}
		return nil
	}
	a.mu.Lock()
	a.cfg.Companion.LocalDevicesURL = localURL
	a.mu.Unlock()
	started, err := a.startCompanion(bindIP)
	if err != nil {
		a.mu.Lock()
		a.cfg.Companion.LocalDevicesURL = previousURL
		a.mu.Unlock()
		return err
	}
	a.mu.Lock()
	a.monitor = started
	a.cfg.Companion.Enabled = true
	a.cfg.Companion.BindIP = bindIP
	a.mu.Unlock()
	return nil
}

func (a *Application) startCompanion(bindIP string) (*transport.Backend, error) {
	a.mu.RLock()
	cfg := a.cfg.Companion
	mqttAddress, httpAddress := a.mqttAddress, a.httpAddress
	a.mu.RUnlock()
	if bindIP == "" {
		// The GUI has no interface picker; without an IP transport.New would
		// reject the whole agent. Pick the default-route IPv4 locally — a UDP
		// "connect" consults the routing table and sends no packets.
		bindIP = transport.PreferredLANIPv4(defaultRouteIPv4())
		if bindIP == "" {
			return nil, errors.New("select_lan_interface")
		}
	}
	name, _ := os.Hostname()
	return transport.New(transport.Options{StoreDirectory: cfg.StateDirectory, HostName: name, BindIP: bindIP, MQTTAddress: mqttAddress, HTTPAddress: httpAddress, Providers: a.Companion,
		AllowHTTPServer: func(raw string) bool {
			a.mu.RLock()
			allowed := a.cfg.Companion.LocalDevicesURL
			a.mu.RUnlock()
			return allowed != "" && raw == allowed
		},
		AllowPrivateServer: func(host string) bool {
			a.mu.RLock()
			allowed := a.cfg.Companion.LocalDevicesURL
			a.mu.RUnlock()
			if allowed == "" {
				return false
			}
			u, err := url.Parse(allowed)
			return err == nil && host == u.Hostname()
		},
	})
}

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

func (a *Application) SetGitHubLogin(login string) error {
	path := ""
	if a.cacheDir != "" && login != "" {
		path = filepath.Join(a.cacheDir, "github-"+login+".json")
	}
	return a.Companion.GitHub.SetLogin(login, path)
}

func (a *Application) SetMusicEnabled(enabled bool) error {
	return a.Companion.Music.(*adapters.Switchable).SetEnabled(enabled)
}

func (a *Application) SetCodexEnabled(enabled bool) {
	a.Companion.Codex.(*codex.Switchable).SetEnabled(enabled)
}

// GetNowPlayingSnapshot returns a nonblocking, detached DTO for the GUI.
func (a *Application) GetNowPlayingSnapshot() nowplaying.Snapshot {
	return a.Companion.Music.Snapshot()
}

// These are local application binding APIs; never exposed as LAN management routes.
func (a *Application) DiscoverMonitors(ctx context.Context) (transport.DiscoveryResult, error) {
	backend := a.Monitor()
	if backend == nil {
		return transport.DiscoveryResult{}, errors.New("companion_disabled")
	}
	return backend.DiscoverDevices(ctx), nil
}
func (a *Application) GetMonitorDevices() ([]transport.DeviceStatus, error) {
	backend := a.Monitor()
	if backend == nil {
		return nil, errors.New("companion_disabled")
	}
	d, _ := backend.Devices()
	return d, nil
}
func (a *Application) BeginMonitorPairing(device string, trustLAN bool) error {
	backend := a.Monitor()
	if backend == nil {
		return errors.New("companion_disabled")
	}
	return backend.BeginPairing(device, trustLAN)
}
func (a *Application) GetMonitorPairingStatus() (transport.PairingStatus, error) {
	backend := a.Monitor()
	if backend == nil {
		return transport.PairingStatus{}, errors.New("companion_disabled")
	}
	return backend.PairingStatus(), nil
}
func (a *Application) ConfirmMonitorPairing(pairing string, approve bool) error {
	backend := a.Monitor()
	if backend == nil {
		return errors.New("companion_disabled")
	}
	return backend.ConfirmPairing(pairing, approve)
}
func (a *Application) CancelMonitorPairing() error {
	backend := a.Monitor()
	if backend == nil {
		return errors.New("companion_disabled")
	}
	return backend.CancelPairing()
}
func (a *Application) RevokeMonitor(device string) error {
	backend := a.Monitor()
	if backend == nil {
		return errors.New("companion_disabled")
	}
	return backend.Revoke(device)
}
func (a *Application) ConnectMonitorSerial(ctx context.Context, port string, selection transport.SerialSelection) error {
	backend := a.Monitor()
	if backend == nil {
		return errors.New("companion_disabled")
	}
	done := make(chan error, 1)
	go func() { done <- backend.ConnectSerial(context.WithoutCancel(ctx), port, selection) }()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case err := <-done:
			return err
		case <-ticker.C:
			if backend.SerialConnected(selection.DeviceID) {
				// The serial session owns the port for its whole lifetime.
				// Return once it is established; later disconnects surface
				// through device status instead of the connect call.
				return nil
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
func (a *Application) ListMonitorSerialPorts() ([]transport.DiscoveredDevice, error) {
	backend := a.Monitor()
	if backend == nil {
		return nil, errors.New("companion_disabled")
	}
	return backend.ListSerialPorts()
}
func (a *Application) ProbeMonitorSerial(ctx context.Context, port string) (transport.DiscoveredDevice, error) {
	backend := a.Monitor()
	if backend == nil {
		return transport.DiscoveredDevice{}, errors.New("companion_disabled")
	}
	return backend.ProbeSerial(ctx, port)
}
func (a *Application) SetMonitorEnabled(device string, enabled bool) error {
	backend := a.Monitor()
	if backend == nil {
		return errors.New("companion_disabled")
	}
	return backend.SetDeviceEnabled(device, enabled)
}
func (a *Application) ConfirmMonitorLANConsent(device string, trusted bool) error {
	backend := a.Monitor()
	if backend == nil {
		return errors.New("companion_disabled")
	}
	return backend.ConfirmLANConsent(device, trusted)
}
func (a *Application) SetMonitorLANInterface(ip string) error {
	backend := a.Monitor()
	if backend == nil {
		return errors.New("companion_disabled")
	}
	return backend.UpdateLANInterface(ip)
}

func (a *Application) Close() error {
	a.ops.Lock()
	defer a.ops.Unlock()
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return nil
	}
	a.closed = true
	manager, backend := a.collector, a.monitor
	a.collector, a.monitor = nil, nil
	a.mu.Unlock()
	var errs []error
	if manager != nil {
		errs = append(errs, manager.Close())
	}
	if backend != nil {
		errs = append(errs, backend.Close())
	}
	return errors.Join(errs...)
}
