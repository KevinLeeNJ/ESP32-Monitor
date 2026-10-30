package ipc

import (
	"net/http"

	"esp32-monitor/client/internal/collector"
)

func (s *Server) routeCollector(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/v1/collector/capabilities":
		caps, err := s.app.GetCollectorCapabilities()
		if err != nil {
			writeAppError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, caps)
	case r.Method == http.MethodPost && r.URL.Path == "/v1/collector/rescan":
		caps, err := s.app.DiscoverCollectorMetrics(r.Context())
		if err != nil {
			writeAppError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, caps)
	case r.Method == http.MethodGet && r.URL.Path == "/v1/collector/status":
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
	default:
		writeError(w, http.StatusNotFound, "not_found")
	}
}
