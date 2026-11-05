package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"esp32-monitor/client/internal/logging"
	"esp32-monitor/client/internal/transport/wire"
	mqtt "github.com/mochi-mqtt/server/v2"
	"io"
	"math"
	"time"
)

var errLarge = errors.New("payload_too_large")
var errTimeout = errors.New("session_expired")

type incoming struct {
	topic string
	data  []byte
	seq   uint32
}
type transaction struct {
	body     string
	topic    string
	response any
}
type closeRequest struct {
	reason string
	done   chan struct{}
}
type session struct {
	pendingMQTT        uint16
	snapshotAccepted   bool
	receivedHeartbeats int

	lastMusic map[string]any

	id                                                     string
	pair                                                   Pairing
	transport, state                                       string
	expires                                                time.Time
	ctx                                                    context.Context
	cancel                                                 context.CancelFunc
	client                                                 *mqtt.Client
	stream                                                 io.ReadWriteCloser
	in                                                     chan incoming
	closeReq                                               chan closeRequest
	qosDone                                                chan struct{}
	subscriptions                                          map[string]bool
	proxy                                                  *proxyWorker
	config                                                 *wire.Config
	inputSeq, outputSeq, lastSnapshot                      uint32
	lastReceipt, lastHeartbeat, lastSnapshotAt, lastResync time.Time
	firstSnapshot                                          time.Time
	sentProof                                              map[uint32]bool
	semantic                                               string
	ages                                                   map[string]ageObservation
	transactions                                           map[string]transaction
	rate                                                   tokenBucket
	serialKeys                                             *SerialKeys
	serialInputSeq                                         uint32
}

func (s *session) setState(b *Backend, state string) {
	b.mu.Lock()
	changed := s.state != state
	s.state = state
	var old *session
	reject := false
	if state == "connected" && s.transport == "serial" && b.sessions[s.pair.DeviceID] != s {
		old = b.sessions[s.pair.DeviceID]
		if b.disabled[s.pair.DeviceID] || s.ctx.Err() != nil ||
			(old != nil && old.transport == "serial") || (old == nil && len(b.sessions) >= 4) {
			reject = true
			old = nil
		} else {
			b.sessions[s.pair.DeviceID] = s
			b.serialSeen[s.pair.DeviceID] = time.Now()
		}
	}
	b.mu.Unlock()
	if reject {
		s.cancel()
		return
	}
	if old != nil {
		old.cancel()
		if old.client != nil {
			old.client.Stop(errors.New("mode_changed"))
		}
	}
	if changed {
		logging.L("transport").Info("device_session_state", "device", logging.Ref(s.pair.DeviceID), "state", state)
	}
}
func (s *session) next() uint32 {
	if s.outputSeq == math.MaxUint32 {
		s.cancel()
		return 0
	}
	s.outputSeq++
	return s.outputSeq
}
func (s *session) send(b *Backend, topic string, data any, qos byte) error {
	for {
		b.mu.Lock()
		now := time.Now()
		if s.rate.take(now) && b.globalRate.take(now) {
			b.mu.Unlock()
			break
		}
		b.mu.Unlock()
		select {
		case <-s.ctx.Done():
			return s.ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	if s.transport == "serial" {
		return s.sendSerial(topic, data)
	}
	return b.sendMQTT(s, topic, data, qos)
}
func (s *session) control(b *Backend, topic string, data any, id *string) error {
	if s.transport == "serial" {
		return s.send(b, topic, data, 1)
	}
	raw, err := wire.Marshal(data)
	if err != nil {
		return err
	}
	c := wire.Control{ProtocolVersion: 1, HostID: b.HostID(), DeviceID: s.pair.DeviceID, SessionID: s.id, Seq: s.next(), SentAt: time.Now().Unix(), RequestID: id, Data: raw}
	return s.send(b, topic, c, 1)
}
func (s *session) run(b *Backend) {
	defer b.release(s)
	device := logging.Ref(s.pair.DeviceID)
	logging.L("transport").Info("device_session_open", "device", device, "transport", s.transport)
	defer logging.L("transport").Info("device_session_close", "device", device, "transport", s.transport)
	s.lastReceipt = time.Now()
	s.ages = map[string]ageObservation{}
	s.transactions = map[string]transaction{}
	s.sentProof = map[uint32]bool{}
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case req := <-s.closeReq:
			if s.config != nil {
				_ = s.control(b, "goodbye", wire.Goodbye{ProtocolVersion: 1, SessionID: s.id, Reason: req.reason}, nil)
				if s.transport == "serial" {
					/* USB CDC queues the final write; closing the port at once
					 * can drop the goodbye before the monitor revokes. */
					time.Sleep(500 * time.Millisecond)
				}
			}
			if req.done != nil {
				close(req.done)
			}
			return
		case m := <-s.in:
			if e := s.handle(b, m); e != nil {
				if errors.Is(e, errTimeout) {
					return
				}
				logging.L("transport").Warn("device_message_rejected", "device", device, "topic", m.topic, "error", e.Error())
				if m.topic == "proxy-config" {
					b.mu.Lock()
					if b.configErrors == nil {
						b.configErrors = make(map[string]string)
					}
					b.configErrors[s.pair.DeviceID] = "invalid_config"
					b.mu.Unlock()
				}
				if e = s.control(b, "error", wire.Failure(e.Error(), nil), nil); e != nil {
					return
				}
			}
		case <-ticker.C:
			timeout := 90 * time.Second
			if s.transport == "serial" {
				timeout = 3 * time.Second
				/* Before the first config the monitor may still be stopping
				 * its radio and LAN session; the 15-second session expiry
				 * below bounds that startup phase instead. */
				if s.config == nil {
					timeout = 0
				}
			}
			if timeout > 0 && time.Since(s.lastReceipt) >= timeout {
				return
			}
			if s.state != "connected" && time.Now().After(s.expires) {
				return
			}
			if s.config == nil {
				if time.Now().After(s.expires) {
					return
				}
				continue
			}
			sections := s.project(b)
			sig := semantic(sections)
			interval := 30 * time.Second
			if s.transport == "serial" {
				interval = time.Second
			} else if sections.Music.State == "healthy" || liveTasks(sections) {
				interval = 10 * time.Second
			}
			if s.state != "connected" && s.lastSnapshot != 0 {
				interval = time.Second
			}
			// Snapshots do not prove that the monitor has received a heartbeat.
			// Keep a separate clock so frequent updates cannot starve heartbeats.
			firstHeartbeatDelay := 2 * time.Second
			if s.transport == "serial" {
				firstHeartbeatDelay = 1500 * time.Millisecond
			}
			if s.lastSnapshot != 0 && time.Since(s.firstSnapshot) >= firstHeartbeatDelay &&
				(s.lastHeartbeat.IsZero() || time.Since(s.lastHeartbeat) >= interval) {
				if e := s.heartbeat(b, sections); e != nil {
					return
				}
				continue
			}
			// Semantics still travel as complete snapshots; freshness between
			// them rides on heartbeat source leases instead of periodic resends.
			if (s.lastSnapshot == 0 || sig != s.semantic) && time.Since(s.lastSnapshotAt) >= 2*time.Second {
				if e := s.snapshot(b, sections, sig); e != nil {
					if errors.Is(e, errLarge) {
						_ = s.control(b, "error", wire.Failure("payload_too_large", nil), nil)
						s.lastSnapshotAt = time.Now()
						continue
					}
					if errors.Is(e, errTimeout) && s.transport != "serial" {
						/* The monitor may still be waking from Wi-Fi power save.
						 * Keep the session; the next tick retries and the
						 * 90-second receipt watchdog bounds a dead link. */
						logging.L("transport").Warn("device_snapshot_timeout", "device", device)
						s.lastSnapshotAt = time.Now()
						continue
					}
					logging.L("transport").Warn("device_snapshot_failed", "device", device, "error", e.Error())
					return
				}
				continue
			}
		}
	}
}
func liveTasks(x wire.Sections) bool {
	if d, ok := x.Codex.Data.(map[string]any); ok {
		t, ok := d["tasks"].(wire.Section)
		return ok && t.State == "healthy"
	}
	return false
}
func (s *session) subscribed(b *Backend) bool {
	if s.transport == "serial" {
		return true
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	pre := prefix(b.HostID(), s.pair.DeviceID, s.id) + "/down/"
	if s.subscriptions[pre+"#"] {
		return true
	}
	for t := range downTopics {
		if !s.subscriptions[pre+t] {
			return false
		}
	}
	return true
}
func (s *session) handle(b *Backend, m incoming) error {
	if m.topic == "presence" {
		var p wire.Presence
		if wire.Decode(m.data, &p, 1024) != nil || p.ProtocolVersion != 1 || p.HostID != b.HostID() || p.DeviceID != s.pair.DeviceID || p.SessionID != s.id {
			return wire.ErrMessage
		}
		if p.State == "offline" {
			return errTimeout
		}
		if p.State != "online" || p.Reason != "connected" {
			return wire.ErrMessage
		}
		return nil
	}
	var c wire.Control
	var body []byte
	var req *string
	var seq uint32
	if s.transport == "serial" {
		body = m.data
		if m.topic == "proxy-config" {
			var id wire.Config
			if wire.Decode(body, &id, 4096) == nil {
				req = &id.RequestID
			}
		}
		seq = m.seq
	} else {
		if wire.Decode(m.data, &c, 16384) != nil {
			return wire.ErrMessage
		}
		if c.ProtocolVersion != 1 {
			return errors.New("unsupported_version")
		}
		if c.HostID != b.HostID() || c.DeviceID != s.pair.DeviceID || c.SessionID != s.id || c.Seq == 0 {
			return wire.ErrMessage
		}
		body = c.Data
		req = c.RequestID
		seq = c.Seq
	}
	normalizedBody, canonicalErr := wire.Canonical(body)
	if canonicalErr != nil {
		return wire.ErrMessage
	}
	if seq <= s.inputSeq {
		return nil
	} // replays never renew a watchdog
	s.inputSeq = seq
	if s.transport == "serial" {
		/* Every valid serial frame proves the board is alive; heartbeats are
		 * not the only traffic before the first snapshot is acknowledged. */
		s.lastReceipt = time.Now()
	}
	if req != nil {
		if !wire.UUID(*req) {
			return wire.ErrMessage
		}
		if old, ok := s.transactions[*req]; ok {
			if old.body != string(normalizedBody) || old.topic != m.topic {
				return wire.ErrMessage
			}
			return s.control(b, oldResponseTopic(m.topic), old.response, req)
		}
	}
	remember := func(topic string, response any) error {
		if req != nil {
			if len(s.transactions) >= 64 {
				return errors.New("rate_limited")
			}
			s.transactions[*req] = transaction{string(normalizedBody), m.topic, response}
		}
		if e := s.control(b, topic, response, req); e != nil {
			return e
		}
		if s.transport == "serial" && topic == "config-ack" {
			return s.send(b, "ack", wire.Ack{ProtocolVersion: 1, SessionID: s.id, AckSeq: s.inputSeq, Status: "accepted"}, 0)
		}
		return nil
	}
	switch m.topic {
	case "open":
		var o wire.Open
		if wire.Decode(body, &o, 2048) != nil || o.ProtocolVersion != 1 || o.DeviceID != s.pair.DeviceID || o.PairingID != s.pair.PairingID || o.Transport != "lan" || !wire.UUID(o.RequestID) || value(req) != o.RequestID || !wire.Text(o.DeviceName, 64) || !wire.Text(o.FirmwareVersion, 64) || !s.subscribed(b) {
			return wire.ErrMessage
		}
		if s.config != nil {
			return errors.New("session_conflict")
		}
		s.setState(b, "syncing_config")
		return remember("session", wire.Session{ProtocolVersion: 1, HostID: b.HostID(), DeviceID: s.pair.DeviceID, PairingID: s.pair.PairingID, SessionID: s.id, Transport: s.transport, HeartbeatIntervalMS: 30000, DisconnectTimeoutMS: 90000, ConfigRequired: true})
	case "proxy-config":
		if s.state != "syncing_config" && s.config == nil {
			return errors.New("config_not_ready")
		}
		var cfg wire.Config
		if wire.Decode(body, &cfg, 4096) != nil {
			return errors.New("invalid_config")
		}
		if cfg.DeviceID != s.pair.DeviceID || cfg.SessionID != s.id || s.transport == "lan" && value(req) != cfg.RequestID {
			return wire.ErrMessage
		}
		if e := validateConfig(cfg); e != nil {
			return e
		}
		b.mu.Lock()
		delete(b.configErrors, s.pair.DeviceID)
		b.mu.Unlock()
		normalized := cfg
		normalized.RequestID = ""
		normalized.SessionID = ""
		fingerprint, _ := json.Marshal(normalized)
		if s.config != nil {
			old := *s.config
			old.RequestID = ""
			old.SessionID = ""
			prior, _ := json.Marshal(old)
			if old.Epoch == cfg.Epoch && (cfg.Revision < old.Revision || cfg.Revision == old.Revision && !bytes.Equal(prior, fingerprint)) {
				return errors.New("config_conflict")
			}
			if old.Epoch == cfg.Epoch && cfg.Revision == old.Revision {
				ack := configAck(cfg)
				return remember("config-ack", ack)
			}
		}
		client := b.opts.HTTPClient
		if client == nil {
			client = guardedClient()
		}
		proxy := newProxy(s.ctx, cfg, client)
		old := s.proxy
		s.proxy = proxy
		s.config = &cfg
		if old != nil {
			old.stop()
		}
		s.semantic = ""
		s.lastSnapshot = 0
		s.snapshotAccepted = false
		s.receivedHeartbeats = 0
		s.firstSnapshot = time.Time{}
		if s.state != "connected" {
			s.expires = time.Now().Add(15 * time.Second)
		}
		s.setState(b, "waiting_snapshot")
		ack := configAck(cfg)
		return remember("config-ack", ack)
	case "ack":
		var a wire.Ack
		if wire.Decode(body, &a, 1024) != nil || a.ProtocolVersion != 1 || a.SessionID != s.id || (a.Status != "accepted" && a.Status != "duplicate") || !s.sentProof[a.AckSeq] {
			return wire.ErrMessage
		}
		delete(s.sentProof, a.AckSeq)
		if a.Status == "accepted" {
			s.lastReceipt = time.Now()
			if a.AckSeq == s.lastSnapshot {
				s.snapshotAccepted = true
			}
			ready := s.snapshotAccepted && s.lastSnapshot != 0 && a.AckSeq > s.lastSnapshot && time.Since(s.firstSnapshot) >= time.Second
			if s.transport == "serial" {
				ready = ready && s.receivedHeartbeats >= 2
			}
			if ready {
				s.setState(b, "connected")
				b.mu.Lock()
				delete(b.recovery, s.pair.DeviceID)
				b.mu.Unlock()
			}
		}
		return nil
	case "resync":
		var r wire.Resync
		if wire.Decode(body, &r, 1024) != nil || (r.Reason != "missing_snapshot" && r.Reason != "source_mismatch" && r.Reason != "parse_failed") {
			return wire.ErrMessage
		}
		if s.config == nil {
			return errors.New("config_not_ready")
		}
		if time.Since(s.lastResync) < 10*time.Second {
			return errors.New("rate_limited")
		}
		s.lastResync = time.Now()
		return s.snapshot(b, s.project(b), "")
	case "heartbeat":
		if s.transport != "serial" {
			return wire.ErrMessage
		}
		var h wire.Heartbeat
		if wire.Decode(body, &h, 4096) != nil || h.ProtocolVersion != 1 || h.HostID != b.HostID() || h.DeviceID != s.pair.DeviceID || h.SessionID != s.id || h.Seq != seq {
			return wire.ErrMessage
		}
		s.lastReceipt = time.Now()
		s.receivedHeartbeats++
		return nil
	case "goodbye":
		var g wire.Goodbye
		if wire.Decode(body, &g, 1024) != nil || g.ProtocolVersion != 1 || g.SessionID != s.id {
			return wire.ErrMessage
		}
		if g.Reason == "pairing_revoked" {
			logging.L("transport").Info("device_pairing_revoked", "device", logging.Ref(s.pair.DeviceID))
			if e := b.store.remove(s.pair.DeviceID); e != nil {
				logging.L("transport").Warn("device_pairing_revoke_failed", "device", logging.Ref(s.pair.DeviceID), "error", e.Error())
			}
			b.mu.Lock()
			delete(b.recovery, s.pair.DeviceID)
			b.mu.Unlock()
		}
		return errTimeout
	case "error":
		return nil
	}
	return wire.ErrMessage
}
func oldResponseTopic(topic string) string {
	if topic == "open" {
		return "session"
	}
	return "config-ack"
}
func configAck(c wire.Config) wire.ConfigAck {
	return wire.ConfigAck{ProtocolVersion: 1, RequestID: &c.RequestID, DeviceID: c.DeviceID, SessionID: c.SessionID, Epoch: &c.Epoch, Revision: &c.Revision, State: "applied", WeatherConfigured: c.Weather.Enabled, DevicesConfigured: c.Devices.Enabled}
}
func (s *session) snapshot(b *Backend, sections wire.Sections, sig string) error {
	seq := s.next()
	x := wire.Snapshot{ProtocolVersion: 1, HostID: b.HostID(), DeviceID: s.pair.DeviceID, SessionID: s.id, Seq: seq, SentAt: time.Now().Unix(), Epoch: s.config.Epoch, Revision: s.config.Revision, Sections: sections}
	if s.transport == "serial" {
		s.outputSeq--
	}
	if e := s.send(b, "snapshot", x, 1); e != nil {
		return e
	}
	s.lastSnapshot = seq
	s.lastSnapshotAt = time.Now()
	if s.firstSnapshot.IsZero() {
		s.firstSnapshot = s.lastSnapshotAt
	}
	s.semantic = sig
	if sig == "" {
		s.semantic = semantic(sections)
	}
	s.recordProof(seq)
	if music, ok := sections.Music.Data.(map[string]any); ok {
		s.lastMusic, _ = music["current"].(map[string]any)
	}
	return nil
}
func (s *session) recordProof(seq uint32) {
	if len(s.sentProof) > 64 {
		for k := range s.sentProof {
			delete(s.sentProof, k)
		}
	}
	s.sentProof[seq] = true
}
func (s *session) heartbeat(b *Backend, sections wire.Sections) error {
	seq := s.next()
	/* Every heartbeat repeats the current lease set. Each lease carries the
	 * age recomputed for this send, so repeating does not fake freshness.
	 * Omitting an unchanged lease is not neutral: the monitor falls back to
	 * the snapshot age for that path and can expire a live source between
	 * two observations of a slow provider. */
	h := wire.Heartbeat{ProtocolVersion: 1, HostID: b.HostID(), DeviceID: s.pair.DeviceID, SessionID: s.id, Seq: seq, SentAt: time.Now().Unix(), LastSnapshotSeq: &s.lastSnapshot, Epoch: &s.config.Epoch, Revision: &s.config.Revision, Time: &wire.HeartbeatTime{UnixMS: time.Now().UnixMilli(), Offset: s.config.Time.Offset, Trusted: false}, SourceUpdates: leases(sections, s.lastSnapshot)}
	if s.transport == "serial" {
		s.outputSeq--
	}
	if e := s.send(b, "heartbeat", h, 0); e != nil {
		return e
	}
	s.lastHeartbeat = time.Now()
	s.recordProof(seq)
	return nil
}
