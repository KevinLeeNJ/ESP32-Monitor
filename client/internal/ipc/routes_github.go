package ipc

import "net/http"

func (s *Server) routeGitHub(w http.ResponseWriter, r *http.Request) {
	switch {
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
	default:
		writeError(w, http.StatusNotFound, "not_found")
	}
}

func (s *Server) routeCompanion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	switch r.URL.Path {
	case "/v1/companion/snapshot":
		writeJSON(w, http.StatusOK, s.app.Companion.Snapshot())
	case "/v1/now-playing":
		writeJSON(w, http.StatusOK, s.app.GetNowPlayingSnapshot())
	default:
		writeError(w, http.StatusNotFound, "not_found")
	}
}
