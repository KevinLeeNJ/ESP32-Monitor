package transport

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"esp32-monitor/client/internal/logging"
	"esp32-monitor/client/internal/transport/wire"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"
)

type pairWindow struct {
	deviceID        string
	consent         time.Time
	expires         time.Time
	response        *wire.PairResponse
	request         wire.PairRequest
	hostConfirmed   bool
	deviceConfirmed bool
	rejected        bool
	sourceIP        string
	lastStatus      time.Time
}
type PairingStatus struct {
	DeviceID    string `json:"device_id"`
	PairingID   string `json:"pairing_id"`
	Code        string `json:"code"`
	State       string `json:"state"`
	ExpiresInMS int64  `json:"expires_in_ms"`
}
type pairingServer struct {
	mu       sync.Mutex
	service  *Backend
	window   *pairWindow
	server   *http.Server
	listener net.Listener
	attempts map[string][]time.Time
	all      []time.Time
}

// BeginPairing is a local API: consent must be an explicit per-device action.
// The selected discovery candidate is re-probed before any invitation is sent.
func (b *Backend) BeginPairing(ctxDevice string, trusted bool) error {
	if !trusted {
		return errors.New("lan_consent_required")
	}
	if !wire.UUID(ctxDevice) {
		return wire.ErrMessage
	}
	if _, ok := b.store.pair(ctxDevice); ok {
		return errors.New("session_conflict")
	}
	return b.beginLANPairing(ctxDevice)
}
func (p *pairingServer) open(device, ip string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.window != nil && time.Now().Before(p.window.expires) {
		return errors.New("session_conflict")
	}
	if len(p.service.store.pairs()) >= 8 {
		return errors.New("capacity_exceeded")
	}
	if p.listener == nil {
		l, e := net.Listen("tcp4", p.service.httpAddress())
		if e != nil {
			return errors.New("pairing_listen_failed")
		}
		p.listener = l
		p.server = &http.Server{Handler: p, ReadHeaderTimeout: 4 * time.Second, ReadTimeout: 4 * time.Second, WriteTimeout: 4 * time.Second, IdleTimeout: 4 * time.Second, MaxHeaderBytes: 4096}
		go p.server.Serve(l)
	}
	now := time.Now()
	p.window = &pairWindow{deviceID: device, sourceIP: ip, consent: now, expires: now.Add(120 * time.Second)}
	logging.L("transport").Info("pairing_opened", "device", logging.Ref(device), "source", ip)
	return nil
}
func (p *pairingServer) port() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.listener == nil {
		return 0
	}
	return p.listener.Addr().(*net.TCPAddr).Port
}
func (p *pairingServer) close() {
	p.mu.Lock()
	server := p.server
	p.server = nil
	p.listener = nil
	p.window = nil
	p.mu.Unlock()
	if server != nil {
		_ = server.Close()
	}
}
func (p *pairingServer) expire() {
	p.mu.Lock()
	expired := p.window != nil && !time.Now().Before(p.window.expires)
	p.mu.Unlock()
	if expired {
		p.close()
	}
}
func (b *Backend) PairingStatus() PairingStatus {
	p := b.pairing
	p.mu.Lock()
	defer p.mu.Unlock()
	w := p.window
	if w == nil {
		return PairingStatus{State: "closed"}
	}
	s := PairingStatus{DeviceID: w.deviceID, State: "pending", ExpiresInMS: max(0, time.Until(w.expires).Milliseconds())}
	if w.response != nil {
		s.PairingID = w.response.PairingID
		s.Code = pairCode(w.request.DeviceNonce, w.response.HostNonce, w.deviceID, b.store.HostID(), s.PairingID)
		s.State = w.response.State
	}
	return s
}
func (b *Backend) ConfirmPairing(id string, approve bool) error {
	p := b.pairing
	p.mu.Lock()
	defer p.mu.Unlock()
	w := p.window
	if w == nil || w.response == nil || w.response.PairingID != id || !time.Now().Before(w.expires) {
		return errors.New("pairing_expired")
	}
	if !approve {
		w.rejected = true
		w.response.State = "rejected"
		logging.L("transport").Info("pairing_rejected", "device", logging.Ref(w.deviceID))
		return nil
	}
	w.hostConfirmed = true
	return p.approve(w)
}
func (p *pairingServer) approve(w *pairWindow) error {
	if !w.hostConfirmed || !w.deviceConfirmed || w.rejected || w.response.State == "approved" {
		return nil
	}
	r := w.response
	token, key := wire.Nonce(), wire.Nonce()
	pair := Pairing{DeviceID: w.deviceID, DeviceName: w.request.DeviceName, PairingID: r.PairingID, DeviceToken: token, SerialKey: key, ConsentAt: w.consent.Unix(), WarningVersion: 1, LANConsent: w.sourceIP != "serial"}
	if e := p.service.store.put(pair); e != nil {
		return e
	}
	r.State = "approved"
	r.DeviceToken = &token
	r.SerialKey = &key
	logging.L("transport").Info("pairing_approved", "device", logging.Ref(w.deviceID))
	return nil
}
func pairCode(dn, hn, d, h, p string) string {
	db, _ := wire.RawNonce(dn)
	hb, _ := wire.RawNonce(hn)
	x := sha256.New()
	x.Write([]byte("ESP32MON-PAIR-V1"))
	x.Write(db)
	x.Write(hb)
	x.Write([]byte(d + h + p))
	return fmt.Sprintf("%06d", binary.BigEndian.Uint32(x.Sum(nil)[:4])%1000000)
}
func (p *pairingServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", wire.ContentType)
	w.Header().Set("Cache-Control", "no-store")
	fail := func(code string, status int) {
		writeProto(w, status, wire.Failure(code, nil))
	}
	size := len(r.Method) + len(r.RequestURI) + len(r.Proto) + 4
	for k, vs := range r.Header {
		for _, v := range vs {
			size += len(k) + len(v) + 4
		}
	}
	if size > 4096 {
		fail("payload_too_large", 413)
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	win := p.window
	if win == nil {
		fail("pairing_closed", 403)
		return
	}
	if !time.Now().Before(win.expires) {
		fail("pairing_expired", 410)
		return
	}
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	if ip != win.sourceIP {
		fail("unauthorized", 401)
		return
	}
	if r.URL.Path == "/companion/v1/capabilities" && r.Method == "GET" {
		writeProto(w, http.StatusOK, p.service.capabilitiesLocked())
		return
	}
	if r.URL.Path != "/companion/v1/pair" || r.Method != "POST" {
		fail("invalid_message", 404)
		return
	}
	if r.Header.Get("Content-Type") != wire.ContentType {
		fail("invalid_message", 415)
		return
	}
	if r.Header.Get("Content-Encoding") != "" && r.Header.Get("Content-Encoding") != "identity" {
		fail("invalid_message", 400)
		return
	}
	data, e := io.ReadAll(io.LimitReader(r.Body, 4097))
	if e != nil || len(data) > 4096 {
		fail("payload_too_large", 413)
		return
	}
	var req wire.PairRequest
	if wire.Decode(data, &req, 4096) != nil || !wire.UUID(req.RequestID) {
		fail("invalid_message", 400)
		return
	}
	if req.ProtocolVersion != 1 {
		fail("unsupported_version", 426)
		return
	}
	status := http.StatusOK
	switch req.Step {
	case "begin":
		if req.DeviceID != win.deviceID || !wire.Text(req.DeviceName, 64) || req.DeviceName == "" {
			fail("unauthorized", 401)
			return
		}
		if _, e := wire.RawNonce(req.DeviceNonce); e != nil {
			fail("invalid_message", 400)
			return
		}
		if win.response != nil {
			if win.request != req {
				fail("session_conflict", 409)
				return
			}
		} else {
			cutoff := time.Now().Add(-time.Minute)
			keep := func(xs []time.Time) []time.Time {
				out := xs[:0]
				for _, x := range xs {
					if x.After(cutoff) {
						out = append(out, x)
					}
				}
				return out
			}
			p.all = keep(p.all)
			p.attempts[ip] = keep(p.attempts[ip])
			if len(p.all) >= 10 || len(p.attempts[ip]) >= 5 {
				w.Header().Set("Retry-After", "60")
				fail("rate_limited", 429)
				return
			}
			p.all = append(p.all, time.Now())
			p.attempts[ip] = append(p.attempts[ip], time.Now())
			win.request = req
			win.response = &wire.PairResponse{ProtocolVersion: 1, PairingID: wire.NewID(), State: "pending", HostID: p.service.store.HostID(), HostName: p.service.opts.HostName, HostNonce: wire.Nonce(), PairingTicket: wire.Nonce()}
		}
		status = http.StatusCreated
	case "status", "confirm":
		if win.response == nil || req.PairingID != win.response.PairingID || subtle.ConstantTimeCompare([]byte(req.PairingTicket), []byte(win.response.PairingTicket)) != 1 {
			fail("unauthorized", 401)
			return
		}
		if req.Step == "status" {
			if time.Since(win.lastStatus) < time.Second {
				w.Header().Set("Retry-After", "1")
				fail("rate_limited", 429)
				return
			}
			win.lastStatus = time.Now()
			status = http.StatusAccepted
		} else {
			if !req.DeviceConfirmed {
				fail("invalid_message", 400)
				return
			}
			win.deviceConfirmed = true
			if e := p.approve(win); e != nil {
				fail("internal_error", 500)
				return
			}
		}
	default:
		fail("invalid_message", 400)
		return
	}
	response := *win.response
	response.ExpiresInMS = max(0, time.Until(win.expires).Milliseconds())
	writeProto(w, status, response)
}
func writeProto(w http.ResponseWriter, status int, value any) {
	data, err := wire.Marshal(value)
	if err != nil || len(data) > 4096 {
		status = http.StatusInternalServerError
		data, _ = wire.Marshal(wire.Failure("internal_error", nil))
	}
	w.WriteHeader(status)
	_, _ = w.Write(data)
}
func (b *Backend) capabilitiesLocked() any {
	return wire.Capabilities{ProtocolVersion: 1, SupportedVersions: []int{1}, HostID: b.store.HostID(), HostName: b.opts.HostName, AppVersion: "1.0.0", PairingOpen: true, Transports: []string{"lan", "serial"}, Features: []string{"proxy_config_v1", "mqtt311_lan", "protobuf_v2"}, MaxSnapshotBytes: 16384,
		MQTT: wire.MQTTCapabilities{Protocol: "3.1.1", Port: b.mqttPort(), KeepAliveSeconds: 60, IdleHeartbeatMS: 30000, LiveHeartbeatMS: 10000, DisconnectTimeoutMS: 90000}}
}
