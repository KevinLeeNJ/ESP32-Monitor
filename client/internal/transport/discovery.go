package transport

import (
	"context"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"esp32-monitor/client/internal/logging"
	"esp32-monitor/client/internal/transport/wire"
	"net"
	"strconv"
	"strings"
	"time"
)

func lp(s string) []byte {
	b := make([]byte, 2+len(s))
	binary.LittleEndian.PutUint16(b, uint16(len(s)))
	copy(b[2:], s)
	return b
}
func DiscoveryMAC(m wire.Discovery, key string) string {
	raw, _ := wire.RawNonce(key)
	salt := sha256.Sum256(append(append(lp(m.HostID), lp(value(m.DeviceID))...), lp(value(m.PairingID))...))
	kd, _ := hkdf.Key(sha256.New, raw, salt[:], "ESP32MON-DISCOVERY-V1", 32)
	h := hmac.New(sha256.New, kd)
	fields := []string{"ESP32MON-DISCOVERY-V1", m.Type, strconv.Itoa(m.ProtocolVersion), m.RequestID, m.HostID, value(m.DeviceID), value(m.PairingID), m.Nonce, value(m.ResponseNonce), number(m.HostPort), number(m.MQTTPort), value(m.SessionID), value(m.DeviceName), value(m.FirmwareVersion), boolean(m.PairingOpen)}
	for _, v := range fields {
		h.Write(lp(v))
	}
	return hex.EncodeToString(h.Sum(nil))
}
func value(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
func number(n *int) string {
	if n == nil {
		return ""
	}
	return strconv.Itoa(*n)
}
func boolean(b *bool) string {
	if b == nil {
		return ""
	}
	if *b {
		return "1"
	}
	return "0"
}

type candidate struct {
	Message       wire.Discovery
	Addr          *net.UDPAddr
	Seen          time.Time
	Authenticated bool
}

func (b *Backend) discoveryTarget() string {
	if b.opts.DiscoveryTarget != "" {
		return b.opts.DiscoveryTarget
	}
	ip := net.ParseIP(b.bindIP()).To4()
	ifaces, _ := net.Interfaces()
	for _, i := range ifaces {
		as, _ := i.Addrs()
		for _, a := range as {
			n, ok := a.(*net.IPNet)
			if ok && n.IP.Equal(ip) {
				v := make(net.IP, 4)
				for j := range v {
					v[j] = ip[j] | ^n.Mask[j]
				}
				return net.JoinHostPort(v.String(), "18444")
			}
		}
	}
	return ""
}

func (b *Backend) query(ctx context.Context, target string, pair *Pairing, duration time.Duration, offer bool) ([]candidate, error) {
	b.discoveryMu.Lock()
	defer b.discoveryMu.Unlock()
	a, e := net.ResolveUDPAddr("udp4", target)
	if e != nil {
		return nil, errors.New("network_unreachable")
	}
	listenIP := net.ParseIP(b.bindIP())
	// Windows rejects a LAN-bound socket sending to loopback; an explicit
	// loopback target (local simulator) must use a loopback
	// socket. Pairing still opens its listener on the bound LAN interface.
	if a.IP.IsLoopback() {
		listenIP = net.IPv4(127, 0, 0, 1)
	}
	conn, e := net.ListenUDP("udp4", &net.UDPAddr{IP: listenIP})
	if e != nil {
		return nil, errors.New("discovery_listen_failed")
	}
	defer conn.Close()
	m := wire.Discovery{ProtocolVersion: 1, Type: "monitor_discover", RequestID: wire.NewID(), HostID: b.store.HostID(), Nonce: wire.Nonce()}
	if pair != nil {
		m.DeviceID = &pair.DeviceID
		m.PairingID = &pair.PairingID
		m.MAC = wire.Ptr(DiscoveryMAC(m, pair.SerialKey))
	}
	data, encodeErr := wire.Marshal(m)
	if encodeErr != nil {
		return nil, encodeErr
	}
	if _, e = conn.WriteToUDP(data, a); e != nil {
		logging.L("transport").Warn("discovery_send_failed", "error", e.Error())
		return nil, errors.New("network_unreachable")
	}
	// Some Wi-Fi networks discard subnet-directed broadcasts but deliver the
	// IPv4 local broadcast. Use the same bound interface, request ID and nonce
	// for both; replies are still validated against this discovery round.
	if b.opts.DiscoveryTarget == "" && !a.IP.IsLoopback() && target == b.discoveryTarget() {
		_, _ = conn.WriteToUDP(data, &net.UDPAddr{IP: net.IPv4bcast, Port: a.Port})
	}
	deadline := time.Now().Add(duration)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetReadDeadline(deadline)
	cancelDone := make(chan struct{})
	defer close(cancelDone)
	go func() {
		select {
		case <-ctx.Done():
			conn.Close()
		case <-cancelDone:
		}
	}()
	out := []candidate{}
	seen := make(map[string]bool)
	pairingLostObserved := false
	buf := make([]byte, 1025)
	for {
		n, addr, e := conn.ReadFromUDP(buf)
		if e != nil {
			if len(out) > 0 {
				return out, nil
			}
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return out, nil
		}
		var r wire.Discovery
		if n > 1024 || wire.Decode(buf[:n], &r, 1024) != nil || r.ProtocolVersion != 1 || r.Type != "monitor_announce" || r.RequestID != m.RequestID || r.HostID != m.HostID || r.Nonce != m.Nonce || r.DeviceID == nil || !wire.UUID(*r.DeviceID) || r.ResponseNonce == nil || r.DeviceName == nil || !wire.Text(*r.DeviceName, 64) || r.FirmwareVersion == nil || !wire.Text(*r.FirmwareVersion, 64) || r.PairingOpen == nil || r.HostPort != nil || r.MQTTPort != nil || r.SessionID != nil {
			continue
		}
		if _, e = wire.RawNonce(*r.ResponseNonce); e != nil {
			continue
		}
		if pair != nil && b.discoveryRevokes(r, pair) {
			return out, nil
		}
		if pair != nil && *r.DeviceID == pair.DeviceID && *r.PairingOpen && r.PairingID == nil && r.MAC == nil {
			/* A previously paired device answered with a fresh unpaired
			 * announcement. It cannot be authenticated, so this only surfaces a
			 * re-pair hint; stored credentials are never deleted from discovery. */
			if !pairingLostObserved {
				b.notePairingLost(*r.DeviceID)
				pairingLostObserved = true
			}
			continue
		}
		if pair != nil {
			if *r.DeviceID != pair.DeviceID || value(r.PairingID) != pair.PairingID || *r.PairingOpen || r.MAC == nil || !hmac.Equal([]byte(*r.MAC), []byte(DiscoveryMAC(r, pair.SerialKey))) {
				continue
			}
			b.clearPairingLost(*r.DeviceID)
		} else if !*r.PairingOpen || r.PairingID != nil || r.MAC != nil {
			continue
		}
		if pair == nil && offer {
			b.mu.Lock()
			selected := b.pairSelection
			b.mu.Unlock()
			if *r.DeviceID != selected {
				continue
			}
		}
		c := candidate{Message: r, Addr: addr, Seen: time.Now(), Authenticated: pair != nil}
		b.mu.Lock()
		if _, ok := b.candidates[*r.DeviceID]; ok || len(b.candidates) < 32 {
			b.candidates[*r.DeviceID] = c
		} else {
			b.discoveryOverflow = true
		}
		b.mu.Unlock()
		if !seen[*r.DeviceID] && len(out) < 32 {
			seen[*r.DeviceID] = true
			out = append(out, c)
		}
		if offer {
			o := r
			o.Type = "monitor_connect_offer"
			if pair == nil {
				o.HostPort = wire.Ptr(b.pairing.port())
			} else {
				sid, e := b.invite(*pair)
				if e != nil {
					return nil, e
				}
				o.MQTTPort = wire.Ptr(b.mqttPort())
				o.SessionID = &sid
				o.MAC = wire.Ptr(DiscoveryMAC(o, pair.SerialKey))
			}
			payload, encodeErr := wire.Marshal(o)
			if encodeErr != nil {
				return nil, encodeErr
			}
			if len(payload) > 1024 {
				return nil, errors.New("payload_too_large")
			}
			_, e = conn.WriteToUDP(payload, addr)
			return out, e
		}
		if pair != nil {
			return out, nil
		}
	}
}

// Discover performs one five-second round on the selected IPv4 interface.
func (b *Backend) Discover(ctx context.Context) error {
	b.mu.Lock()
	if time.Since(b.lastDiscovery) < 5*time.Second {
		b.mu.Unlock()
		return errors.New("rate_limited")
	}
	b.lastDiscovery = time.Now()
	for id, c := range b.candidates {
		if _, paired := b.store.pair(id); !paired && time.Since(c.Seen) > 10*time.Second {
			delete(b.candidates, id)
		}
	}
	b.discoveryOverflow = false
	b.mu.Unlock()
	target := b.discoveryTarget()
	if target == "" {
		return errors.New("network_unreachable")
	}
	_, e := b.query(ctx, target, nil, 5*time.Second, false)
	return e
}
func (b *Backend) beginLANPairing(device string) error {
	b.mu.Lock()
	c, ok := b.candidates[device]
	b.mu.Unlock()
	if ok && time.Since(c.Seen) > 10*time.Second {
		// Risk-confirmation outlives the discovery TTL at human pace. Re-verify
		// the cached address before rejecting; a fresh announce also refreshes
		// the candidate, keeping the unicast identity check authoritative.
		ctx, cancel := context.WithTimeout(b.ctx, 3*time.Second)
		defer cancel()
		out, e := b.query(ctx, c.Addr.String(), nil, 2*time.Second, false)
		if e != nil || len(out) == 0 {
			return errors.New("device_not_observed")
		}
		b.mu.Lock()
		c, ok = b.candidates[device]
		b.mu.Unlock()
	}
	if !ok || time.Since(c.Seen) > 10*time.Second {
		return errors.New("device_not_observed")
	}
	if e := b.pairing.open(device, c.Addr.IP.String()); e != nil {
		return e
	}
	// Re-probe with an exact selected-device filter: another responder must never
	// receive an invitation just because it shared the old address.
	ctx, cancel := context.WithTimeout(b.ctx, 4*time.Second)
	defer cancel()
	e := b.offerSelected(ctx, c, device)
	if e != nil {
		b.pairing.close()
	}
	return e
}
func (b *Backend) offerSelected(ctx context.Context, c candidate, device string) error {
	// query must validate the selected UUID before sending a first-pair offer.
	b.mu.Lock()
	b.pairSelection = device
	b.mu.Unlock()
	defer func() { b.mu.Lock(); b.pairSelection = ""; b.mu.Unlock() }()
	out, e := b.query(ctx, c.Addr.String(), nil, 3*time.Second, true)
	if e != nil {
		return e
	}
	if len(out) == 0 {
		return errors.New("network_unreachable")
	}
	return nil
}
func (b *Backend) recoverDevice(ctx context.Context, p Pairing) {
	start := time.Now()
	b.mu.Lock()
	c, ok := b.candidates[p.DeviceID]
	b.mu.Unlock()
	if ok {
		out, e := b.query(ctx, c.Addr.String(), &p, lanRecoveryProbeTimeout, true)
		if e == nil && len(out) > 0 {
			logging.L("transport").Info("lan_recovery_connected", "device", logging.Ref(p.DeviceID),
				"path", "candidate", "elapsed_ms", time.Since(start).Milliseconds())
			return
		}
	}
	target := b.discoveryTarget()
	if target != "" {
		out, e := b.query(ctx, target, &p, lanRecoveryProbeTimeout, true)
		if e == nil && len(out) > 0 {
			logging.L("transport").Info("lan_recovery_connected", "device", logging.Ref(p.DeviceID),
				"path", "broadcast", "elapsed_ms", time.Since(start).Milliseconds())
			return
		}
	}
	logging.L("transport").Debug("lan_recovery_miss", "device", logging.Ref(p.DeviceID),
		"elapsed_ms", time.Since(start).Milliseconds())
}

/* A revoked monitor answers a paired probe with pairing_open=true, the old
 * pairing ID and a MAC made with the old discovery key. This is the only
 * discovery response allowed to delete credentials: an unauthenticated
 * pairing-open announcement remains a hint. */
func (b *Backend) discoveryRevokes(r wire.Discovery, pair *Pairing) bool {
	if pair == nil || r.DeviceID == nil || r.PairingOpen == nil || !*r.PairingOpen ||
		r.PairingID == nil || *r.PairingID != pair.PairingID || *r.DeviceID != pair.DeviceID ||
		r.MAC == nil || !hmac.Equal([]byte(*r.MAC), []byte(DiscoveryMAC(r, pair.SerialKey))) {
		return false
	}
	b.revokeFromDiscovery(*r.DeviceID)
	return true
}

func (b *Backend) revokeFromDiscovery(device string) {
	logging.L("transport").Info("device_pairing_revoked_offline", "device", logging.Ref(device))
	b.mu.Lock()
	s := b.sessions[device]
	delete(b.sessions, device)
	delete(b.candidates, device)
	delete(b.recovery, device)
	delete(b.pairingLost, device)
	delete(b.serialSeen, device)
	b.mu.Unlock()
	if e := b.store.remove(device); e != nil {
		logging.L("transport").Warn("device_pairing_revoke_failed", "device", logging.Ref(device), "error", e.Error())
	}
	if s != nil {
		s.cancel()
		if s.client != nil {
			s.client.Stop(errors.New("pairing_revoked"))
		}
	}
}

func (b *Backend) notePairingLost(deviceID string) {
	now := time.Now()
	b.mu.Lock()
	s := b.pairingLost[deviceID]
	if now.Sub(s.last) > 30*time.Second {
		s = pairingLostState{}
	}
	s.last = now
	s.count++
	b.pairingLost[deviceID] = s
	b.mu.Unlock()
	logging.L("transport").Warn("device_pairing_lost", "device", logging.Ref(deviceID))
}

func (b *Backend) clearPairingLost(deviceID string) {
	b.mu.Lock()
	delete(b.pairingLost, deviceID)
	b.mu.Unlock()
}

func pairingLostActive(s pairingLostState, now time.Time) bool {
	return s.count >= 2 && !s.last.IsZero() && now.Sub(s.last) <= pairingLostWindow
}

// SerialConnected reports whether the serial session for this device is up.
func (b *Backend) SerialConnected(device string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.sessions[device]
	return s != nil && s.transport == "serial" && s.state == "connected"
}

func (b *Backend) Devices() ([]DeviceStatus, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	out := []DeviceStatus{}
	seen := map[string]bool{}
	for _, p := range b.store.pairs() {
		c := b.candidates[p.DeviceID]
		var lastSeen time.Time
		if p.LastSeen > 0 {
			lastSeen = time.Unix(p.LastSeen, 0)
		}
		s := DeviceStatus{DeviceID: p.DeviceID, Name: p.DeviceName, Paired: true, Enabled: !b.disabled[p.DeviceID], ConnectionState: "disconnected", LastSeen: lastSeen}
		if c.Seen.After(s.LastSeen) {
			s.LastSeen = c.Seen
		}
		s.ConfigError = b.configErrors[p.DeviceID]
		if c.Addr != nil {
			s.Address = c.Addr.IP.String()
		}
		s.IdentityVerified = c.Authenticated
		s.ConfirmedOnline = !c.Seen.IsZero() && now.Sub(c.Seen) <= 10*time.Second
		if last := b.serialSeen[p.DeviceID]; last.After(s.LastSeen) {
			s.LastSeen = last
		}
		if x := b.sessions[p.DeviceID]; x != nil {
			s.SessionID = x.id
			s.ConnectionState = x.state
			s.Transport = x.transport
			s.IdentityVerified = true
			s.LastSeen = now
			// A session being authenticated or synchronized does not yet prove
			// the monitor has accepted the current snapshot and heartbeat.
			s.ConfirmedOnline = x.state == "connected"
		} else if lost, ok := b.pairingLost[p.DeviceID]; ok && pairingLostActive(lost, now) {
			/* The device answered but no longer recognises this pairing. Keep
			 * the credential; surface a re-pair hint only. */
			s.ConnectionState = "pairing_lost"
			s.PairingLost = true
			s.IdentityVerified = false
			s.ConfirmedOnline = true
			s.LastSeen = lost.last
		}
		out = append(out, s)
		seen[p.DeviceID] = true
	}
	for id, c := range b.candidates {
		if seen[id] {
			continue
		}
		out = append(out, DeviceStatus{DeviceID: id, Name: value(c.Message.DeviceName), Enabled: !b.disabled[id], Address: c.Addr.IP.String(), ConfirmedOnline: time.Since(c.Seen) <= 10*time.Second, ConnectionState: "discovered", LastSeen: c.Seen})
	}
	return out, b.discoveryOverflow
}
func prefix(host, device, session string) string {
	return strings.Join([]string{"emon", "v1", host, device, session}, "/")
}
