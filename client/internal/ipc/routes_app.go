package ipc

import (
	"net/http"

	"esp32-monitor/client/internal/diagnostics"
)

func (s *Server) routeApp(w http.ResponseWriter, r *http.Request) {
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
	default:
		writeError(w, http.StatusNotFound, "not_found")
	}
}

func (s *Server) routeDiagnostics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	writeJSON(w, http.StatusOK, diagnostics.Build(s.app, s.opts.Now()))
}
