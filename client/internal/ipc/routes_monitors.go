package ipc

import (
	"context"
	"net/http"
	"time"

	"esp32-monitor/client/internal/transport"
)

func (s *Server) routeMonitors(w http.ResponseWriter, r *http.Request) {
	switch {
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
