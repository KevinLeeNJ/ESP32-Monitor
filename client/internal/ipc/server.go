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
	"esp32-monitor/client/internal/platform"
	"esp32-monitor/client/internal/providers"
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
	case strings.HasPrefix(r.URL.Path, "/v1/app/"):
		s.routeApp(w, r)
	case r.URL.Path == "/v1/diagnostics/report":
		s.routeDiagnostics(w, r)
	case strings.HasPrefix(r.URL.Path, "/v1/collector/"):
		s.routeCollector(w, r)
	case strings.HasPrefix(r.URL.Path, "/v1/github/"):
		s.routeGitHub(w, r)
	case strings.HasPrefix(r.URL.Path, "/v1/companion/"), r.URL.Path == "/v1/now-playing":
		s.routeCompanion(w, r)
	case strings.HasPrefix(r.URL.Path, "/v1/monitors/"):
		s.routeMonitors(w, r)
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
	if _, err := platform.ParseHTTPURL(base); err != nil {
		result.Error = "invalid_server_url"
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
