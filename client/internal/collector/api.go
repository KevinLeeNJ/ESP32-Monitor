package collector

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

func readPrivateFile(path string, limit int64) ([]byte, error) {
	st, err := os.Lstat(path)
	if err != nil || !st.Mode().IsRegular() || st.Size() > limit || (runtime.GOOS != "windows" && st.Mode().Perm() != 0600) {
		return nil, errors.New("invalid_private_file")
	}
	return os.ReadFile(path)
}

// Handler is a loopback-only authenticated backend API for a trusted native GUI
// bridge. No CORS is enabled; browser-origin requests are rejected. Tokens and
// generated TOML are never included in capability/configuration/snapshot DTOs.
func (m *Manager) Handler(token string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		ip := net.ParseIP(host)
		if err != nil || ip == nil || !ip.IsLoopback() || r.Header.Get("Origin") != "" || len(token) < 32 || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+token)) != 1 {
			apiError(w, 401, "unauthorized")
			return
		}
		var result any
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/collector/capabilities":
			result = m.Capabilities()
			if m.Capabilities().SchemaVersion == 0 {
				apiError(w, 409, "capability_scan_required")
				return
			}
		case "POST /v1/collector/rescan":
			result, err = m.Discover(r.Context())
		case "GET /v1/collector/status":
			result = m.Snapshot()
		case "PUT /v1/collector/config":
			dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
			dec.DisallowUnknownFields()
			var c Config
			if dec.Decode(&c) != nil {
				apiError(w, 400, "invalid_config_json")
				return
			}
			var extra any
			if dec.Decode(&extra) != io.EOF {
				apiError(w, 400, "invalid_config_json")
				return
			}
			result, err = m.Apply(r.Context(), c)
		default:
			apiError(w, 404, "not_found")
			return
		}
		if err != nil {
			apiError(w, 422, err.Error())
			return
		}
		_ = json.NewEncoder(w).Encode(result)
	})
}
func apiError(w http.ResponseWriter, status int, code string) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error_code": code})
}

type API struct {
	server *http.Server
	path   string
	once   sync.Once
	err    error
}

// ListenAPI writes a 0600 native-bridge connection descriptor into the private
// state directory. Only the OS user/native process should read this credential.
func (m *Manager) ListenAPI(address string) (*API, error) {
	m.op.Lock()
	defer m.op.Unlock()
	if m.closed {
		return nil, errClosed
	}
	if m.api != nil {
		return nil, errors.New("collector_api_already_started")
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil || host != "127.0.0.1" {
		return nil, errors.New("collector_api_requires_ipv4_loopback")
	}
	listener, err := net.Listen("tcp4", address)
	if err != nil {
		return nil, err
	}
	token := randomToken()
	path := filepath.Join(m.opts.StateDirectory, "collector-api.json")
	body, _ := json.Marshal(map[string]string{"base_url": "http://" + listener.Addr().String(), "token": token})
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(err, os.ErrExist) {
		if _, err = readPrivateFile(path, 4096); err == nil {
			err = os.Remove(path)
		}
		if err == nil {
			file, err = os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		}
	}
	if err == nil {
		_, err = file.Write(body)
		closeErr := file.Close()
		if err == nil {
			err = closeErr
		}
	}
	if err != nil {
		listener.Close()
		return nil, errors.New("collector_api_descriptor_failed")
	}
	api := &API{server: &http.Server{Handler: m.Handler(token), ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 2 * time.Minute, WriteTimeout: 2 * time.Minute, MaxHeaderBytes: 8192}, path: path}
	m.api = api
	go func() { _ = api.server.Serve(listener) }()
	return api, nil
}
func (a *API) Close() error {
	a.once.Do(func() { a.err = a.server.Close(); _ = os.Remove(a.path) })
	return a.err
}
