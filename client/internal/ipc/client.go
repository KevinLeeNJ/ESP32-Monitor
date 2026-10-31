package ipc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	githubauth "esp32-monitor/client/internal/auth/github"
	"esp32-monitor/client/internal/collector"
	"esp32-monitor/client/internal/companion"
	"esp32-monitor/client/internal/diagnostics"
	"esp32-monitor/client/internal/nowplaying"
	"esp32-monitor/client/internal/transport"
)

// APIError carries the server error_code so the GUI can branch on the same
// literals the Agent application layer defines.
type APIError struct {
	Status int    `json:"status"`
	Code   string `json:"error_code"`
}

func (e *APIError) Error() string { return e.Code }

// Client talks to the Agent IPC endpoint. Every call opens its own connection
// and validates that the Agent runs as the same OS user.
type Client struct {
	socketPath string
	http       *http.Client
}

// Dial validates the endpoint and returns a client.
func Dial(socketPath string) (*Client, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, err := dialValidatedIPC(ctx, socketPath)
	if err != nil {
		return nil, fmt.Errorf("ipc_dial_failed: %w", err)
	}
	_ = conn.Close()
	return &Client{
		socketPath: socketPath,
		http: &http.Client{
			Transport: &http.Transport{
				// One request per connection: the server-side peer check and
				// the client-side endpoint check both run per connection.
				DisableKeepAlives: true,
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					return dialValidatedIPC(ctx, socketPath)
				},
			},
		},
	}, nil
}

func dialValidatedIPC(ctx context.Context, path string) (net.Conn, error) {
	conn, err := dialIPC(ctx, path)
	if err != nil {
		return nil, err
	}
	if !ipcServerPeerOK(conn) {
		_ = conn.Close()
		return nil, ErrPeerCheckRequired
	}
	return conn, nil
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://ipc"+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, MaxBody))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		var reply errorReply
		_ = json.Unmarshal(payload, &reply)
		if reply.ErrorCode == "" {
			reply.ErrorCode = fmt.Sprintf("http_%d", resp.StatusCode)
		}
		return &APIError{Status: resp.StatusCode, Code: reply.ErrorCode}
	}
	if out == nil {
		return nil
	}
	if len(bytes.TrimSpace(payload)) == 0 {
		return errors.New("empty_response")
	}
	return json.Unmarshal(payload, out)
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	return c.do(ctx, http.MethodGet, path, nil, out)
}

func (c *Client) post(ctx context.Context, path string, body, out any) error {
	return c.do(ctx, http.MethodPost, path, body, out)
}

// Status returns the Agent health answer.
func (c *Client) Status(ctx context.Context) (Status, error) {
	var status Status
	err := c.get(ctx, "/v1/app/status", &status)
	return status, err
}

// Quit asks the Agent to exit gracefully.
func (c *Client) Quit(ctx context.Context) error {
	return c.post(ctx, "/v1/app/quit", map[string]bool{}, nil)
}

// Restart asks the elevated Agent to relaunch itself with new arguments.
func (c *Client) Restart(ctx context.Context, args []string) error {
	return c.post(ctx, "/v1/app/restart", restartRequest{Args: args}, nil)
}

// SetLoginTask creates or removes the highest-privilege logon task.
func (c *Client) SetLoginTask(ctx context.Context, enabled bool) error {
	return c.post(ctx, "/v1/app/login-task", loginTaskRequest{Enabled: enabled}, nil)
}

// ServerTest probes the Server health endpoint from the Agent process.
func (c *Client) ServerTest(ctx context.Context, baseURL string) (ServerTestResult, error) {
	var result ServerTestResult
	err := c.post(ctx, "/v1/app/server-test", serverTestRequest{BaseURL: baseURL}, &result)
	return result, err
}

// DiagnosticsReport returns the redacted diagnostic snapshot.
func (c *Client) DiagnosticsReport(ctx context.Context) (diagnostics.Report, error) {
	var report diagnostics.Report
	err := c.get(ctx, "/v1/diagnostics/report", &report)
	return report, err
}

// CompanionSnapshot returns the GitHub/Codex/Music aggregate snapshot.
func (c *Client) CompanionSnapshot(ctx context.Context) (companion.Snapshot, error) {
	var snapshot companion.Snapshot
	err := c.get(ctx, "/v1/companion/snapshot", &snapshot)
	return snapshot, err
}

// NowPlaying returns the current playback snapshot.
func (c *Client) NowPlaying(ctx context.Context) (nowplaying.Snapshot, error) {
	var snapshot nowplaying.Snapshot
	err := c.get(ctx, "/v1/now-playing", &snapshot)
	return snapshot, err
}

// SetGitHubLogin switches the contribution provider without restarting the Agent.
func (c *Client) SetGitHubLogin(ctx context.Context, login string) error {
	return c.post(ctx, "/v1/github/account", githubLogoutRequest{Login: login}, nil)
}

func (c *Client) SetMusicEnabled(ctx context.Context, enabled bool) error {
	return c.post(ctx, "/v1/companion/music", loginTaskRequest{Enabled: enabled}, nil)
}

func (c *Client) SetCodexEnabled(ctx context.Context, enabled bool) error {
	return c.post(ctx, "/v1/companion/codex", loginTaskRequest{Enabled: enabled}, nil)
}

// GitHubSession returns the current login session state.
func (c *Client) GitHubSession(ctx context.Context) (githubauth.Session, error) {
	var session githubauth.Session
	err := c.get(ctx, "/v1/github/session", &session)
	return session, err
}

// GitHubLoginStart opens the default browser for device authorization.
func (c *Client) GitHubLoginStart(ctx context.Context) (githubauth.Session, error) {
	var session githubauth.Session
	err := c.post(ctx, "/v1/github/login/start", map[string]bool{}, &session)
	return session, err
}

// GitHubLoginPoll waits for the user to authorize in the browser.
func (c *Client) GitHubLoginPoll(ctx context.Context, sessionID string) (githubauth.Session, error) {
	var session githubauth.Session
	err := c.post(ctx, "/v1/github/login/poll", githubSessionIDRequest{SessionID: sessionID}, &session)
	return session, err
}

// GitHubLoginCancel cancels a pending login.
func (c *Client) GitHubLoginCancel(ctx context.Context, sessionID string) (githubauth.Session, error) {
	var session githubauth.Session
	err := c.post(ctx, "/v1/github/login/cancel", githubSessionIDRequest{SessionID: sessionID}, &session)
	return session, err
}

// GitHubLogout removes stored credentials for the given login.
func (c *Client) GitHubLogout(ctx context.Context, login string) error {
	return c.post(ctx, "/v1/github/logout", githubLogoutRequest{Login: login}, nil)
}

// CollectorCapabilities returns the last capability scan.
func (c *Client) CollectorCapabilities(ctx context.Context) (collector.Capabilities, error) {
	var caps collector.Capabilities
	err := c.get(ctx, "/v1/collector/capabilities", &caps)
	return caps, err
}

// CollectorRescan reruns the capability discovery.
func (c *Client) CollectorRescan(ctx context.Context) (collector.Capabilities, error) {
	var caps collector.Capabilities
	err := c.post(ctx, "/v1/collector/rescan", map[string]bool{}, &caps)
	return caps, err
}

// CollectorStatus returns the current collector snapshot.
func (c *Client) CollectorStatus(ctx context.Context) (collector.Snapshot, error) {
	var snapshot collector.Snapshot
	err := c.get(ctx, "/v1/collector/status", &snapshot)
	return snapshot, err
}

// ApplyCollectorConfig submits a candidate collector configuration.
func (c *Client) ApplyCollectorConfig(ctx context.Context, config collector.Config) (collector.Snapshot, error) {
	var snapshot collector.Snapshot
	err := c.do(ctx, http.MethodPut, "/v1/collector/config", config, &snapshot)
	return snapshot, err
}

// MonitorDevices returns the known Monitor device states.
func (c *Client) MonitorDevices(ctx context.Context) ([]transport.DeviceStatus, error) {
	var devices []transport.DeviceStatus
	err := c.get(ctx, "/v1/monitors/devices", &devices)
	return devices, err
}

// DiscoverMonitors scans LAN and serial for reachable Monitors.
func (c *Client) DiscoverMonitors(ctx context.Context, timeoutMS int) (transport.DiscoveryResult, error) {
	var result transport.DiscoveryResult
	err := c.post(ctx, "/v1/monitors/discover", monitorDiscoverRequest{TimeoutMS: timeoutMS}, &result)
	return result, err
}

// MonitorPairingStatus returns the current pairing window state.
func (c *Client) MonitorPairingStatus(ctx context.Context) (transport.PairingStatus, error) {
	var status transport.PairingStatus
	err := c.get(ctx, "/v1/monitors/pairing/status", &status)
	return status, err
}

// BeginMonitorPairing opens a pairing window for the selected device.
func (c *Client) BeginMonitorPairing(ctx context.Context, device string, trustLAN bool) (transport.PairingStatus, error) {
	var status transport.PairingStatus
	err := c.post(ctx, "/v1/monitors/pairing/begin", map[string]any{"device": device, "trust_lan": trustLAN}, &status)
	return status, err
}

// ConfirmMonitorPairing approves or rejects a pending pairing.
func (c *Client) ConfirmMonitorPairing(ctx context.Context, pairingID string, approve bool) (transport.PairingStatus, error) {
	var status transport.PairingStatus
	err := c.post(ctx, "/v1/monitors/pairing/confirm", pairingConfirmRequest{PairingID: pairingID, Approve: approve}, &status)
	return status, err
}

// RevokeMonitor removes the pairing record for a device.
func (c *Client) RevokeMonitor(ctx context.Context, device string) error {
	return c.post(ctx, "/v1/monitors/revoke", deviceRequest{Device: device}, nil)
}

// SetMonitorEnabled toggles an already paired device.
func (c *Client) SetMonitorEnabled(ctx context.Context, device string, enabled bool) error {
	return c.post(ctx, "/v1/monitors/enabled", deviceEnabledRequest{Device: device, Enabled: enabled}, nil)
}

// ConfirmLANConsent records the LAN trust decision for a device.
func (c *Client) ConfirmLANConsent(ctx context.Context, device string, trusted bool) error {
	return c.post(ctx, "/v1/monitors/lan/consent", deviceConsentRequest{Device: device, Trusted: trusted}, nil)
}

// SetLANInterface switches the LAN listener to another IPv4 address.
func (c *Client) SetLANInterface(ctx context.Context, ip string) error {
	return c.post(ctx, "/v1/monitors/lan/interface", lanInterfaceRequest{IP: ip}, nil)
}

// ListSerialPorts enumerates local serial ports without probing them.
func (c *Client) ListSerialPorts(ctx context.Context) ([]transport.DiscoveredDevice, error) {
	var ports []transport.DiscoveredDevice
	err := c.get(ctx, "/v1/monitors/serial/ports", &ports)
	return ports, err
}

// ProbeSerial sends one HELLO on the selected port.
func (c *Client) ProbeSerial(ctx context.Context, port string) (transport.DiscoveredDevice, error) {
	var device transport.DiscoveredDevice
	err := c.post(ctx, "/v1/monitors/serial/probe", serialProbeRequest{Port: port}, &device)
	return device, err
}

// ConnectSerial attaches an already paired serial Monitor.
func (c *Client) ConnectSerial(ctx context.Context, port, deviceID string, trustPhysicalConnection bool) error {
	return c.post(ctx, "/v1/monitors/serial/connect", serialConnectRequest{Port: port, DeviceID: deviceID, TrustPhysicalConnection: trustPhysicalConnection}, nil)
}
