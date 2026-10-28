package guibridge

import (
	"context"
	"errors"
	"time"

	githubauth "esp32-monitor/client/internal/auth/github"
	"esp32-monitor/client/internal/collector"
	"esp32-monitor/client/internal/companion"
	"esp32-monitor/client/internal/diagnostics"
	"esp32-monitor/client/internal/ipc"
	"esp32-monitor/client/internal/nowplaying"
	"esp32-monitor/client/internal/transport"
)

// callTimeout bounds one IPC round trip; GUI requests keep the frontend
// responsive even while the Agent serves a long discovery.
const callTimeout = 45 * time.Second

func call[Out any](ctx context.Context, sup *Supervisor, fn func(ctx context.Context, client *ipc.Client) (Out, error)) (Out, error) {
	var out Out
	err := sup.Call(func(client *ipc.Client) error {
		callCtx, cancel := context.WithTimeout(ctx, callTimeout)
		defer cancel()
		var callErr error
		out, callErr = fn(callCtx, client)
		return callErr
	})
	if err != nil {
		var zero Out
		return zero, err
	}
	return out, nil
}

func callVoid(ctx context.Context, sup *Supervisor, fn func(ctx context.Context, client *ipc.Client) error) error {
	return sup.Call(func(client *ipc.Client) error {
		callCtx, cancel := context.WithTimeout(ctx, callTimeout)
		defer cancel()
		return fn(callCtx, client)
	})
}

// AppService exposes application status, settings and lifecycle to the GUI.
type AppService struct {
	sup        *Supervisor
	quit       func()
	onSettings func(Settings)
}

// NewAppService wires the status service; quit runs after the Agent stops.
func NewAppService(sup *Supervisor, quit func()) *AppService {
	return &AppService{sup: sup, quit: quit}
}

// SetSettingsListener observes persisted settings for GUI-only behaviour such
// as the menu bar icon; it runs before the Agent restart completes.
func (s *AppService) SetSettingsListener(listener func(Settings)) { s.onSettings = listener }

// Status returns Agent health and enabled features.
func (s *AppService) Status(ctx context.Context) (ipc.Status, error) {
	return call(ctx, s.sup, func(ctx context.Context, c *ipc.Client) (ipc.Status, error) { return c.Status(ctx) })
}

// DiagnosticsReport returns the redacted diagnostic snapshot.
func (s *AppService) DiagnosticsReport(ctx context.Context) (diagnostics.Report, error) {
	return call(ctx, s.sup, func(ctx context.Context, c *ipc.Client) (diagnostics.Report, error) { return c.DiagnosticsReport(ctx) })
}

// ServerTest probes a Server health endpoint from the Agent process.
func (s *AppService) ServerTest(ctx context.Context, baseURL string) (ipc.ServerTestResult, error) {
	return call(ctx, s.sup, func(ctx context.Context, c *ipc.Client) (ipc.ServerTestResult, error) {
		return c.ServerTest(ctx, baseURL)
	})
}

// QuitApp stops the Agent and closes the GUI process.
func (s *AppService) QuitApp(context.Context) error {
	if s.quit == nil {
		return errors.New("quit_unavailable")
	}
	s.sup.Stop()
	s.quit()
	return nil
}

// GetSettings returns the currently applied settings.
func (s *AppService) GetSettings() Settings { return s.sup.CurrentSettings() }

// SaveSettings persists settings and restarts the Agent with new flags.
func (s *AppService) SaveSettings(ctx context.Context, settings Settings) (Settings, error) {
	current := s.sup.CurrentSettings()
	if settings.Launch.LaunchAtLogin != current.Launch.LaunchAtLogin {
		if err := setLaunchAtLogin(settings.Launch.LaunchAtLogin); err != nil {
			return current, err
		}
	}
	if err := SaveSettings(settings); err != nil {
		return s.sup.CurrentSettings(), err
	}
	if s.onSettings != nil {
		s.onSettings(settings)
	}
	if err := s.sup.UpdateSettings(ctx, settings); err != nil {
		return s.sup.CurrentSettings(), err
	}
	return s.sup.CurrentSettings(), nil
}

// RestartAgent relaunches the Agent without changing settings.
func (s *AppService) RestartAgent(ctx context.Context) error { return s.sup.Restart(ctx) }

// LastAgentError returns the most recent supervisor failure text.
func (s *AppService) LastAgentError() string {
	if err := s.sup.LastError(); err != nil {
		return err.Error()
	}
	return ""
}

// CollectorService mirrors the frontend CollectorBridge contract.
type CollectorService struct{ sup *Supervisor }

// NewCollectorService binds collector operations.
func NewCollectorService(sup *Supervisor) *CollectorService { return &CollectorService{sup: sup} }

// DiscoverCollectorMetrics reruns capability discovery.
func (s *CollectorService) DiscoverCollectorMetrics(ctx context.Context) (collector.Capabilities, error) {
	return call(ctx, s.sup, func(ctx context.Context, c *ipc.Client) (collector.Capabilities, error) {
		return c.CollectorRescan(ctx)
	})
}

// GetCollectorCapabilities returns the last capability scan.
func (s *CollectorService) GetCollectorCapabilities(ctx context.Context) (collector.Capabilities, error) {
	return call(ctx, s.sup, func(ctx context.Context, c *ipc.Client) (collector.Capabilities, error) {
		return c.CollectorCapabilities(ctx)
	})
}

// GetCollectorSnapshot returns the live collector snapshot.
func (s *CollectorService) GetCollectorSnapshot(ctx context.Context) (collector.Snapshot, error) {
	return call(ctx, s.sup, func(ctx context.Context, c *ipc.Client) (collector.Snapshot, error) { return c.CollectorStatus(ctx) })
}

// ApplyCollectorConfig submits a candidate collector configuration.
func (s *CollectorService) ApplyCollectorConfig(ctx context.Context, config collector.Config) (collector.Snapshot, error) {
	return call(ctx, s.sup, func(ctx context.Context, c *ipc.Client) (collector.Snapshot, error) {
		return c.ApplyCollectorConfig(ctx, config)
	})
}

// CompanionService exposes GitHub/Codex/Music provider state.
type CompanionService struct{ sup *Supervisor }

// NewCompanionService binds companion provider operations.
func NewCompanionService(sup *Supervisor) *CompanionService { return &CompanionService{sup: sup} }

// GetCompanionSnapshot returns the provider aggregate snapshot.
func (s *CompanionService) GetCompanionSnapshot(ctx context.Context) (companion.Snapshot, error) {
	return call(ctx, s.sup, func(ctx context.Context, c *ipc.Client) (companion.Snapshot, error) { return c.CompanionSnapshot(ctx) })
}

// GetNowPlaying returns the current playback snapshot.
func (s *CompanionService) GetNowPlaying(ctx context.Context) (nowplaying.Snapshot, error) {
	return call(ctx, s.sup, func(ctx context.Context, c *ipc.Client) (nowplaying.Snapshot, error) { return c.NowPlaying(ctx) })
}

// GitHubSession returns the login session state.
func (s *CompanionService) GitHubSession(ctx context.Context) (githubauth.Session, error) {
	return call(ctx, s.sup, func(ctx context.Context, c *ipc.Client) (githubauth.Session, error) { return c.GitHubSession(ctx) })
}

// GitHubLoginStart opens the browser for device authorization.
func (s *CompanionService) GitHubLoginStart(ctx context.Context) (githubauth.Session, error) {
	return call(ctx, s.sup, func(ctx context.Context, c *ipc.Client) (githubauth.Session, error) { return c.GitHubLoginStart(ctx) })
}

// GitHubLoginPoll waits until the browser authorization resolves.
func (s *CompanionService) GitHubLoginPoll(ctx context.Context, sessionID string) (githubauth.Session, error) {
	return call(ctx, s.sup, func(ctx context.Context, c *ipc.Client) (githubauth.Session, error) {
		return c.GitHubLoginPoll(ctx, sessionID)
	})
}

// GitHubLoginCancel cancels a pending login.
func (s *CompanionService) GitHubLoginCancel(ctx context.Context, sessionID string) (githubauth.Session, error) {
	return call(ctx, s.sup, func(ctx context.Context, c *ipc.Client) (githubauth.Session, error) {
		return c.GitHubLoginCancel(ctx, sessionID)
	})
}

// GitHubLogout removes stored credentials for a login.
func (s *CompanionService) GitHubLogout(ctx context.Context, login string) error {
	return callVoid(ctx, s.sup, func(ctx context.Context, c *ipc.Client) error { return c.GitHubLogout(ctx, login) })
}

// MonitorService exposes ESP32 Monitor discovery, pairing and serial attach.
type MonitorService struct{ sup *Supervisor }

// NewMonitorService binds Monitor device operations.
func NewMonitorService(sup *Supervisor) *MonitorService { return &MonitorService{sup: sup} }

// DiscoverMonitors scans LAN and serial for reachable Monitors.
func (s *MonitorService) DiscoverMonitors(ctx context.Context, timeoutMS int) (transport.DiscoveryResult, error) {
	return call(ctx, s.sup, func(ctx context.Context, c *ipc.Client) (transport.DiscoveryResult, error) {
		return c.DiscoverMonitors(ctx, timeoutMS)
	})
}

// GetMonitorDevices returns the known device states.
func (s *MonitorService) GetMonitorDevices(ctx context.Context) ([]transport.DeviceStatus, error) {
	return call(ctx, s.sup, func(ctx context.Context, c *ipc.Client) ([]transport.DeviceStatus, error) {
		return c.MonitorDevices(ctx)
	})
}

// GetPairingStatus returns the current pairing window state.
func (s *MonitorService) GetPairingStatus(ctx context.Context) (transport.PairingStatus, error) {
	return call(ctx, s.sup, func(ctx context.Context, c *ipc.Client) (transport.PairingStatus, error) {
		return c.MonitorPairingStatus(ctx)
	})
}

// BeginPairing opens a pairing window for the selected device.
func (s *MonitorService) BeginPairing(ctx context.Context, device string, trustLAN bool) (transport.PairingStatus, error) {
	return call(ctx, s.sup, func(ctx context.Context, c *ipc.Client) (transport.PairingStatus, error) {
		return c.BeginMonitorPairing(ctx, device, trustLAN)
	})
}

// ConfirmPairing approves or rejects a pending pairing.
func (s *MonitorService) ConfirmPairing(ctx context.Context, pairingID string, approve bool) (transport.PairingStatus, error) {
	return call(ctx, s.sup, func(ctx context.Context, c *ipc.Client) (transport.PairingStatus, error) {
		return c.ConfirmMonitorPairing(ctx, pairingID, approve)
	})
}

// RevokeDevice removes the pairing record for a device.
func (s *MonitorService) RevokeDevice(ctx context.Context, device string) error {
	return callVoid(ctx, s.sup, func(ctx context.Context, c *ipc.Client) error { return c.RevokeMonitor(ctx, device) })
}

// SetDeviceEnabled toggles an already paired device.
func (s *MonitorService) SetDeviceEnabled(ctx context.Context, device string, enabled bool) error {
	return callVoid(ctx, s.sup, func(ctx context.Context, c *ipc.Client) error { return c.SetMonitorEnabled(ctx, device, enabled) })
}

// ConfirmLANConsent records the LAN trust decision for a device.
func (s *MonitorService) ConfirmLANConsent(ctx context.Context, device string, trusted bool) error {
	return callVoid(ctx, s.sup, func(ctx context.Context, c *ipc.Client) error { return c.ConfirmLANConsent(ctx, device, trusted) })
}

// SetLANInterface switches the LAN listener to another IPv4 address.
func (s *MonitorService) SetLANInterface(ctx context.Context, ip string) error {
	return callVoid(ctx, s.sup, func(ctx context.Context, c *ipc.Client) error { return c.SetLANInterface(ctx, ip) })
}

// ListSerialPorts enumerates local serial ports without probing them.
func (s *MonitorService) ListSerialPorts(ctx context.Context) ([]transport.DiscoveredDevice, error) {
	return call(ctx, s.sup, func(ctx context.Context, c *ipc.Client) ([]transport.DiscoveredDevice, error) {
		return c.ListSerialPorts(ctx)
	})
}

// ProbeSerial sends one HELLO on the selected port.
func (s *MonitorService) ProbeSerial(ctx context.Context, port string) (transport.DiscoveredDevice, error) {
	return call(ctx, s.sup, func(ctx context.Context, c *ipc.Client) (transport.DiscoveredDevice, error) {
		return c.ProbeSerial(ctx, port)
	})
}

// ConnectSerial attaches an already paired serial Monitor.
func (s *MonitorService) ConnectSerial(ctx context.Context, port, deviceID string, trustPhysicalConnection bool) error {
	return callVoid(ctx, s.sup, func(ctx context.Context, c *ipc.Client) error {
		return c.ConnectSerial(ctx, port, deviceID, trustPhysicalConnection)
	})
}
