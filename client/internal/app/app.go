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
	"os"
	"path/filepath"
)

type Application struct {
	Companion  *companion.Service
	GitHubAuth *githubauth.Client
	Monitor    *transport.Backend
	Collector  *collector.Manager
}

func New(c config.Config, cacheDir string) (*Application, error) {
	cachePath := ""
	if cacheDir != "" && c.GitHub.Enabled {
		cachePath = filepath.Join(cacheDir, "github-"+c.GitHub.Login+".json")
	}
	auth := githubauth.New(githubauth.Options{ClientID: c.GitHub.ClientID, Store: platform.GitHubCredentials{}, OpenBrowser: platform.OpenBrowser})
	githubProvider, err := github.New(github.Options{Enabled: c.GitHub.Enabled, Login: c.GitHub.Login, Token: auth.Token, CachePath: cachePath})
	if err != nil {
		return nil, err
	}
	music, err := adapters.New(adapters.Options{Enabled: c.Music.Enabled, Adapter: c.Music.Adapter, NativeLibrary: c.Music.NativeLibrary})
	if err != nil {
		return nil, err
	}
	application := &Application{GitHubAuth: auth, Companion: &companion.Service{Music: music, GitHub: githubProvider, Codex: codex.New(codex.Options{Enabled: c.Codex.Enabled, Executable: c.Codex.Executable, DesktopEnabled: c.Codex.DesktopEnabled, TaskAdapter: c.Codex.TaskAdapter, Home: c.Codex.Home, ThreadIDs: c.Codex.ThreadIDs})}}
	if c.Companion.Enabled {
		dir := c.Companion.StateDirectory
		if dir == "" {
			base, e := os.UserConfigDir()
			if e != nil {
				return nil, e
			}
			dir = filepath.Join(base, "ESP32-Monitor", "credentials", "companion")
		}
		name, _ := os.Hostname()
		backend, e := transport.New(transport.Options{StoreDirectory: dir, HostName: name, BindIP: c.Companion.BindIP, Providers: application.Companion})
		if e != nil {
			return nil, e
		}
		application.Monitor = backend
	}
	if c.Collector.Enabled {
		dir := c.Collector.StateDirectory
		if dir == "" {
			base, e := os.UserConfigDir()
			if e != nil {
				_ = application.Close()
				return nil, e
			}
			dir = filepath.Join(base, "ESP32-Monitor", "collector")
		}
		manager, e := collector.New(collector.Options{Executable: c.Collector.Executable, StateDirectory: dir, UploadURL: c.Collector.UploadURL, UploadToken: collector.TokenFile(c.Collector.UploadTokenFile)})
		if e != nil {
			_ = application.Close()
			return nil, e
		}
		application.Collector = manager
	}
	return application, nil
}

// GetNowPlayingSnapshot is a nonblocking, detached DTO for future GUI bindings.
// The user-session Agent owns Companion.Music.Run(ctx); this getter never starts
// a process, reads native APIs, changes settings, or uploads media data.
func (a *Application) GetNowPlayingSnapshot() nowplaying.Snapshot {
	return a.Companion.Music.Snapshot()
}

// These are local application binding APIs; never exposed as LAN management routes.
func (a *Application) DiscoverMonitors(ctx context.Context) (transport.DiscoveryResult, error) {
	if a.Monitor == nil {
		return transport.DiscoveryResult{}, errors.New("companion_disabled")
	}
	return a.Monitor.DiscoverDevices(ctx), nil
}
func (a *Application) GetMonitorDevices() ([]transport.DeviceStatus, error) {
	if a.Monitor == nil {
		return nil, errors.New("companion_disabled")
	}
	d, _ := a.Monitor.Devices()
	return d, nil
}
func (a *Application) BeginMonitorPairing(device string, trustLAN bool) error {
	if a.Monitor == nil {
		return errors.New("companion_disabled")
	}
	return a.Monitor.BeginPairing(device, trustLAN)
}
func (a *Application) GetMonitorPairingStatus() (transport.PairingStatus, error) {
	if a.Monitor == nil {
		return transport.PairingStatus{}, errors.New("companion_disabled")
	}
	return a.Monitor.PairingStatus(), nil
}
func (a *Application) ConfirmMonitorPairing(pairing string, approve bool) error {
	if a.Monitor == nil {
		return errors.New("companion_disabled")
	}
	return a.Monitor.ConfirmPairing(pairing, approve)
}
func (a *Application) RevokeMonitor(device string) error {
	if a.Monitor == nil {
		return errors.New("companion_disabled")
	}
	return a.Monitor.Revoke(device)
}
func (a *Application) ConnectMonitorSerial(ctx context.Context, port string, selection transport.SerialSelection) error {
	if a.Monitor == nil {
		return errors.New("companion_disabled")
	}
	return a.Monitor.ConnectSerial(ctx, port, selection)
}
func (a *Application) Close() error {
	var errs []error
	if a.Collector != nil {
		errs = append(errs, a.Collector.Close())
	}
	if a.Monitor != nil {
		errs = append(errs, a.Monitor.Close())
	}
	return errors.Join(errs...)
}

func (a *Application) ListMonitorSerialPorts() ([]transport.DiscoveredDevice, error) {
	if a.Monitor == nil {
		return nil, errors.New("companion_disabled")
	}
	return a.Monitor.ListSerialPorts()
}
func (a *Application) ProbeMonitorSerial(ctx context.Context, port string) (transport.DiscoveredDevice, error) {
	if a.Monitor == nil {
		return transport.DiscoveredDevice{}, errors.New("companion_disabled")
	}
	return a.Monitor.ProbeSerial(ctx, port)
}
func (a *Application) SetMonitorEnabled(device string, enabled bool) error {
	if a.Monitor == nil {
		return errors.New("companion_disabled")
	}
	return a.Monitor.SetDeviceEnabled(device, enabled)
}
func (a *Application) ConfirmMonitorLANConsent(device string, trusted bool) error {
	if a.Monitor == nil {
		return errors.New("companion_disabled")
	}
	return a.Monitor.ConfirmLANConsent(device, trusted)
}
func (a *Application) SetMonitorLANInterface(ip string) error {
	if a.Monitor == nil {
		return errors.New("companion_disabled")
	}
	return a.Monitor.UpdateLANInterface(ip)
}
