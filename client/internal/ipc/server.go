package ipc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"esp32-monitor/client/internal/app"
	"esp32-monitor/client/internal/collector"
	"esp32-monitor/client/internal/diagnostics"
	"esp32-monitor/client/internal/providers"
	"esp32-monitor/client/internal/transport"
)

// Options configures the IPC listener.
type Options struct {
	// SocketPath is a Unix socket path or a Windows named pipe path; the
	// transport decides which validation and ownership rules apply.
	SocketPath string
	// Shutdown asks the Agent to exit gracefully; called at most once after
	// the quit response has been written.
	Shutdown func()
	// Restart asks the elevated Agent to relaunch itself with validated
	// arguments. The callback spawns the replacement before the old process
	// exits, so settings changes never need another UAC prompt.
	Restart func(args []string) error
	// ConfigureLoginTask creates or removes the highest-privilege logon task.
	// The Agent already runs elevated, so the GUI never needs to elevate.
	ConfigureLoginTask func(enabled bool) error
	// Now is injectable for tests.
	Now func() time.Time
}

// Server serves the GUI↔Agent management surface over a private local
// endpoint: a Unix socket on macOS/Linux, a per-user named pipe on Windows.
type Server struct {
	app      *app.Application
	opts     Options
	listener net.Listener
	server   *http.Server
	startAt  time.Time

	closeOnce sync.Once
	quitOnce  sync.Once
}

// Status is the anonymous health answer for the GUI.
type Status struct {
	SchemaVersion    int       `json:"schema_version"`
	PID              int       `json:"pid"`
	StartTime        time.Time `json:"start_time"`
	CollectorEnabled bool      `json:"collector_enabled"`
	CompanionEnabled bool      `json:"companion_enabled"`
	GitHubEnabled    bool      `json:"github_enabled"`
	MusicEnabled     bool      `json:"music_enabled"`
	CodexEnabled     bool      `json:"codex_enabled"`
	SocketPath       string    `json:"socket_path"`
}

type monitorDiscoverRequest struct {
	TimeoutMS int `json:"timeout_ms"`
}

type pairingConfirmRequest struct {
	PairingID string `json:"pairing_id"`
	Approve   bool   `json:"approve"`
}

type deviceRequest struct {
	Device string `json:"device"`
}

type deviceEnabledRequest struct {
	Device  string `json:"device"`
	Enabled bool   `json:"enabled"`
}

type deviceConsentRequest struct {
	Device  string `json:"device"`
	Trusted bool   `json:"trusted"`
}

type lanInterfaceRequest struct {
	IP string `json:"ip"`
}

type serialProbeRequest struct {
	Port string `json:"port"`
}

type serialConnectRequest struct {
	Port                    string `json:"port"`
	DeviceID                string `json:"device_id"`
	TrustPhysicalConnection bool   `json:"trust_physical_connection"`
}

type githubSessionIDRequest struct {
	SessionID string `json:"session_id"`
}

type githubLogoutRequest struct {
	Login string `json:"login"`
}

type restartRequest struct {
	Args []string `json:"args"`
}

type loginTaskRequest struct {
	Enabled bool `json:"enabled"`
}

type serverTestRequest struct {
	BaseURL string `json:"base_url"`
}

type ServerTestResult struct {
	OK         bool   `json:"ok"`
	StatusCode int    `json:"status_code"`
	Error      string `json:"error,omitempty"`
	DurationMS int64  `json:"duration_ms"`
	URL        string `json:"url"`
}

type connContextKey struct{}

// NewServer validates options; Call Listen to bind and serve.
func NewServer(application *app.Application, opts Options) (*Server, error) {
	if opts.SocketPath == "" {
		return nil, errors.New("ipc_socket_path_required")
	}
	if err := validateIPCPath(opts.SocketPath); err != nil {
		return nil, err
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Server{app: application, opts: opts}, nil
}

// Listen binds the local endpoint and starts serving in the background. Only
// one listener is allowed per process and per endpoint.
func (s *Server) Listen() error {
	if s.listener != nil {
		return ErrAlreadyRunning
	}
	listener, err := listenIPC(s.opts.SocketPath)
	if err != nil {
		return err
	}
	s.listener = listener
	s.startAt = s.opts.Now()
	s.server = &http.Server{
		Handler: http.HandlerFunc(s.serveHTTP),
		ConnContext: func(ctx context.Context, c net.Conn) context.Context {
			return context.WithValue(ctx, connContextKey{}, c)
		},
		ReadHeaderTimeout: 3 * time.Second,
		ReadTimeout:       5 * time.Minute,
		WriteTimeout:      5 * time.Minute,
		MaxHeaderBytes:    8192,
	}
	go func() { _ = s.server.Serve(listener) }()
	return nil
}

// Close stops serving and releases the endpoint.
func (s *Server) Close() error {
	var err error
	s.closeOnce.Do(func() {
		if s.server != nil {
			err = s.server.Close()
		}
		if s.listener != nil {
			err = errors.Join(err, closeIPCListener(s.listener, s.opts.SocketPath))
		}
	})
	return err
}

func (s *Server) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Origin") != "" || r.Header.Get("Sec-Fetch-Site") != "" {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	conn, _ := r.Context().Value(connContextKey{}).(net.Conn)
	if conn == nil || !ipcPeerOK(conn) {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	s.route(w, r)
}

func (s *Server) route(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/v1/app/status":
		writeJSON(w, http.StatusOK, s.status())
	case r.Method == http.MethodPost && r.URL.Path == "/v1/app/quit":
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		s.quitOnce.Do(func() {
			if s.opts.Shutdown != nil {
				go s.opts.Shutdown()
			}
		})
	case r.Method == http.MethodPost && r.URL.Path == "/v1/app/restart":
		var req restartRequest
		if err := decodeBody(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json")
			return
		}
		if s.opts.Restart == nil {
			writeError(w, http.StatusNotImplemented, "restart_unsupported")
			return
		}
		if err := s.opts.Restart(req.Args); err != nil {
			writeAppError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	case r.Method == http.MethodPost && r.URL.Path == "/v1/app/login-task":
		var req loginTaskRequest
		if err := decodeBody(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json")
			return
		}
		if s.opts.ConfigureLoginTask == nil {
			writeError(w, http.StatusNotImplemented, "login_task_unsupported")
			return
		}
		if err := s.opts.ConfigureLoginTask(req.Enabled); err != nil {
			writeAppError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	case r.Method == http.MethodPost && r.URL.Path == "/v1/app/server-test":
		var req serverTestRequest
		if err := decodeBody(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json")
			return
		}
		writeJSON(w, http.StatusOK, s.serverTest(req))
	case r.Method == http.MethodGet && r.URL.Path == "/v1/diagnostics/report":
		writeJSON(w, http.StatusOK, diagnostics.Build(s.app, s.opts.Now()))
	case r.Method == http.MethodGet && r.URL.Path == "/v1/companion/snapshot":
		writeJSON(w, http.StatusOK, s.app.Companion.Snapshot())
	case r.Method == http.MethodGet && r.URL.Path == "/v1/now-playing":
		writeJSON(w, http.StatusOK, s.app.GetNowPlayingSnapshot())
	case r.Method == http.MethodGet && r.URL.Path == "/v1/github/session":
		writeJSON(w, http.StatusOK, s.app.GitHubAuth.Status())
	case r.Method == http.MethodPost && r.URL.Path == "/v1/github/login/start":
		session, err := s.app.GitHubAuth.StartLogin(r.Context())
		if err != nil {
			writeAppError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, session)
	case r.Method == http.MethodPost && r.URL.Path == "/v1/github/login/poll":
		var req githubSessionIDRequest
		if err := decodeBody(r, &req); err != nil || req.SessionID == "" {
			writeError(w, http.StatusBadRequest, "invalid_request")
			return
		}
		session, err := s.app.GitHubAuth.PollLogin(r.Context(), req.SessionID)
		if err != nil {
			writeAppError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, session)
	case r.Method == http.MethodPost && r.URL.Path == "/v1/github/login/cancel":
		var req githubSessionIDRequest
		if err := decodeBody(r, &req); err != nil || req.SessionID == "" {
			writeError(w, http.StatusBadRequest, "invalid_request")
			return
		}
		writeJSON(w, http.StatusOK, s.app.GitHubAuth.CancelLogin(req.SessionID))
	case r.Method == http.MethodPost && r.URL.Path == "/v1/github/logout":
		var req githubLogoutRequest
		if err := decodeBody(r, &req); err != nil || req.Login == "" {
			writeError(w, http.StatusBadRequest, "invalid_request")
			return
		}
		if err := s.app.GitHubAuth.Logout(r.Context(), req.Login); err != nil {
			writeAppError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	case r.Method == http.MethodGet && r.URL.Path == "/v1/collector/capabilities":
		if s.app.Collector == nil {
			writeError(w, http.StatusConflict, "collector_disabled")
			return
		}
		caps, err := s.app.GetCollectorCapabilities()
		if err != nil {
			writeAppError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, caps)
	case r.Method == http.MethodPost && r.URL.Path == "/v1/collector/rescan":
		if s.app.Collector == nil {
			writeError(w, http.StatusConflict, "collector_disabled")
			return
		}
		caps, err := s.app.DiscoverCollectorMetrics(r.Context())
		if err != nil {
			writeAppError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, caps)
	case r.Method == http.MethodGet && r.URL.Path == "/v1/collector/status":
		if s.app.Collector == nil {
			writeError(w, http.StatusConflict, "collector_disabled")
			return
		}
		snapshot, err := s.app.GetCollectorSnapshot()
		if err != nil {
			writeAppError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, snapshot)
	case r.Method == http.MethodPut && r.URL.Path == "/v1/collector/config":
		if s.app.Collector == nil {
			writeError(w, http.StatusConflict, "collector_disabled")
			return
		}
		var config collector.Config
		if err := decodeBody(r, &config); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_config_json")
			return
		}
		snapshot, err := s.app.ApplyCollectorConfig(r.Context(), config)
		if err != nil {
			writeAppError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, snapshot)
	case r.Method == http.MethodGet && r.URL.Path == "/v1/monitors/devices":
		devices, err := s.app.GetMonitorDevices()
		if err != nil {
			writeAppError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, devices)
	case r.Method == http.MethodPost && r.URL.Path == "/v1/monitors/discover":
		var req monitorDiscoverRequest
		if err := decodeBody(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request")
			return
		}
		timeout := time.Duration(req.TimeoutMS) * time.Millisecond
		if timeout <= 0 || timeout > 30*time.Second {
			timeout = 10 * time.Second
		}
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()
		result, err := s.app.DiscoverMonitors(ctx)
		if err != nil {
			writeAppError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	case r.Method == http.MethodGet && r.URL.Path == "/v1/monitors/pairing/status":
		status, err := s.app.GetMonitorPairingStatus()
		if err != nil {
			writeAppError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, status)
	case r.Method == http.MethodPost && r.URL.Path == "/v1/monitors/pairing/begin":
		var req struct {
			Device   string `json:"device"`
			TrustLAN bool   `json:"trust_lan"`
		}
		if err := decodeBody(r, &req); err != nil || req.Device == "" {
			writeError(w, http.StatusBadRequest, "invalid_request")
			return
		}
		if err := s.app.BeginMonitorPairing(req.Device, req.TrustLAN); err != nil {
			writeAppError(w, err)
			return
		}
		status, err := s.app.GetMonitorPairingStatus()
		if err != nil {
			writeAppError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, status)
	case r.Method == http.MethodPost && r.URL.Path == "/v1/monitors/pairing/confirm":
		var req pairingConfirmRequest
		if err := decodeBody(r, &req); err != nil || req.PairingID == "" {
			writeError(w, http.StatusBadRequest, "invalid_request")
			return
		}
		if err := s.app.ConfirmMonitorPairing(req.PairingID, req.Approve); err != nil {
			writeAppError(w, err)
			return
		}
		status, err := s.app.GetMonitorPairingStatus()
		if err != nil {
			writeAppError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, status)
	case r.Method == http.MethodPost && r.URL.Path == "/v1/monitors/revoke":
		var req deviceRequest
		if err := decodeBody(r, &req); err != nil || req.Device == "" {
			writeError(w, http.StatusBadRequest, "invalid_request")
			return
		}
		if err := s.app.RevokeMonitor(req.Device); err != nil {
			writeAppError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	case r.Method == http.MethodPost && r.URL.Path == "/v1/monitors/enabled":
		var req deviceEnabledRequest
		if err := decodeBody(r, &req); err != nil || req.Device == "" {
			writeError(w, http.StatusBadRequest, "invalid_request")
			return
		}
		if err := s.app.SetMonitorEnabled(req.Device, req.Enabled); err != nil {
			writeAppError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	case r.Method == http.MethodPost && r.URL.Path == "/v1/monitors/lan/consent":
		var req deviceConsentRequest
		if err := decodeBody(r, &req); err != nil || req.Device == "" {
			writeError(w, http.StatusBadRequest, "invalid_request")
			return
		}
		if err := s.app.ConfirmMonitorLANConsent(req.Device, req.Trusted); err != nil {
			writeAppError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	case r.Method == http.MethodPost && r.URL.Path == "/v1/monitors/lan/interface":
		var req lanInterfaceRequest
		if err := decodeBody(r, &req); err != nil || req.IP == "" {
			writeError(w, http.StatusBadRequest, "invalid_request")
			return
		}
		if err := s.app.SetMonitorLANInterface(req.IP); err != nil {
			writeAppError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	case r.Method == http.MethodGet && r.URL.Path == "/v1/monitors/serial/ports":
		ports, err := s.app.ListMonitorSerialPorts()
		if err != nil {
			writeAppError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, ports)
	case r.Method == http.MethodPost && r.URL.Path == "/v1/monitors/serial/probe":
		var req serialProbeRequest
		if err := decodeBody(r, &req); err != nil || req.Port == "" {
			writeError(w, http.StatusBadRequest, "invalid_request")
			return
		}
		device, err := s.app.ProbeMonitorSerial(r.Context(), req.Port)
		if err != nil {
			writeAppError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, device)
	case r.Method == http.MethodPost && r.URL.Path == "/v1/monitors/serial/connect":
		var req serialConnectRequest
		if err := decodeBody(r, &req); err != nil || req.Port == "" {
			writeError(w, http.StatusBadRequest, "invalid_request")
			return
		}
		selection := transport.SerialSelection{DeviceID: req.DeviceID, TrustPhysicalConnection: req.TrustPhysicalConnection}
		if err := s.app.ConnectMonitorSerial(r.Context(), req.Port, selection); err != nil {
			writeAppError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	default:
		writeError(w, http.StatusNotFound, "not_found")
	}
}

func (s *Server) status() Status {
	status := Status{
		SchemaVersion:    SchemaVersion,
		PID:              os.Getpid(),
		StartTime:        s.startAt,
		CollectorEnabled: s.app.Collector != nil,
		CompanionEnabled: s.app.Monitor != nil,
		SocketPath:       s.opts.SocketPath,
	}
	snapshot := s.app.Companion.Snapshot()
	status.GitHubEnabled = snapshot.GitHub.Health.State != providers.Disabled
	status.MusicEnabled = snapshot.Music.Health.State != providers.Disabled
	status.CodexEnabled = snapshot.Codex.Quota.Health.State != providers.Disabled
	return status
}

func (s *Server) serverTest(req serverTestRequest) ServerTestResult {
	base := strings.TrimRight(strings.TrimSpace(req.BaseURL), "/")
	result := ServerTestResult{URL: base + "/healthz"}
	loopback := strings.HasPrefix(base, "http://127.0.0.1") || strings.HasPrefix(base, "http://localhost") || strings.HasPrefix(base, "http://[::1]")
	if !strings.HasPrefix(base, "https://") && !loopback {
		result.Error = "https_or_loopback_required"
		return result
	}
	client := &http.Client{Timeout: 5 * time.Second}
	start := time.Now()
	resp, err := client.Get(result.URL)
	result.DurationMS = time.Since(start).Milliseconds()
	if err != nil {
		result.Error = "request_failed"
		return result
	}
	defer resp.Body.Close()
	result.StatusCode = resp.StatusCode
	result.OK = resp.StatusCode == http.StatusOK
	if !result.OK {
		result.Error = fmt.Sprintf("unexpected_status_%d", resp.StatusCode)
	}
	return result
}
