package transport

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"esp32-monitor/client/internal/transport/wire"
	"io"
	"time"
)

type Hello = wire.Hello
type Auth = wire.Auth
type SerialSelection struct {
	DeviceID                string
	TrustPhysicalConnection bool
}
type serialReader struct {
	frames chan SerialFrame
	err    chan error
	done   chan struct{}
}

func readSerial(ctx context.Context, stream io.ReadWriteCloser) *serialReader {
	r := &serialReader{make(chan SerialFrame, 4), make(chan error, 1), make(chan struct{})}
	go func() {
		defer close(r.done)
		d := SerialDecoder{}
		buf := make([]byte, 2048)
		for {
			n, e := stream.Read(buf)
			if e != nil {
				select {
				case r.err <- errors.New("serial_disconnected"):
				default:
				}
				return
			}
			frames := d.Feed(buf[:n], time.Now())
			frames = append(frames, d.Tick(time.Now())...)
			for _, f := range frames {
				select {
				case r.frames <- f:
				case <-ctx.Done():
					return
				}
			}
			if ctx.Err() != nil {
				return
			}
		}
	}()
	return r
}
func (r *serialReader) next(ctx context.Context, timeout time.Duration) (SerialFrame, error) {
	t := time.NewTimer(timeout)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return SerialFrame{}, ctx.Err()
	case e := <-r.err:
		return SerialFrame{}, e
	case f := <-r.frames:
		return f, nil
	case <-t.C:
		return SerialFrame{}, errTimeout
	}
}

// ConnectSerial owns only the selected port. Unknown ports are never probed.
func (b *Backend) ConnectSerial(ctx context.Context, port string, selection SerialSelection) error {
	b.mu.Lock()
	if b.serialPorts[port] {
		b.mu.Unlock()
		return errors.New("serial_port_busy")
	}
	b.serialPorts[port] = true
	b.mu.Unlock()
	defer func() { b.mu.Lock(); delete(b.serialPorts, port); b.mu.Unlock() }()
	stream, e := OpenSerialPort(port)
	if e != nil {
		return e
	}
	return b.attachSerial(ctx, stream, selection, port)
}

// AttachSerial also supports an owned duplex test stream; no physical-board claim.
func (b *Backend) AttachSerial(ctx context.Context, stream io.ReadWriteCloser, selection SerialSelection) error {
	return b.attachSerial(ctx, stream, selection, "")
}
func (b *Backend) attachSerial(ctx context.Context, stream io.ReadWriteCloser, selection SerialSelection, port string) error {
	b.mu.Lock()
	if b.ctx.Err() != nil {
		b.mu.Unlock()
		stream.Close()
		return errors.New("companion_closed")
	}
	b.wg.Add(1)
	b.mu.Unlock()
	defer b.wg.Done()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer stream.Close()
	stop := context.AfterFunc(b.ctx, cancel)
	defer stop()
	closed := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			stream.Close()
		case <-closed:
		}
	}()
	defer close(closed)
	r := readSerial(ctx, stream)
	defer func() { cancel(); stream.Close(); <-r.done }()
	var seq uint32
	write := func(t byte, data any, key []byte) error {
		seq++
		return writeSerialFrame(ctx, stream, t, seq, data, key)
	}
	var hello Hello
	hostNonce := wire.Nonce()
	request := Hello{ProtocolVersion: 1, Step: "request", HostID: b.HostID(), HostName: b.opts.HostName, HostNonce: hostNonce, SupportedVersions: []int{1}}
	var f SerialFrame
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		if err = write(SerialHello, request, nil); err != nil {
			return err
		}
		f, err = r.next(ctx, 2*time.Second)
		if err == nil {
			break
		}
	}
	if err != nil {
		return err
	}
	plain, err := VerifySerial(f, nil, 2)
	if err != nil || f.Type != SerialHello || wire.Decode(plain, &hello, 2048) != nil || hello.ProtocolVersion != 1 || hello.Step != "response" || hello.HostID != b.HostID() || hello.HostNonce != hostNonce || !wire.UUID(hello.DeviceID) || !wire.Text(hello.DeviceName, 64) || !wire.Text(hello.FirmwareVersion, 64) || hello.MaxPayloadBytes != 16368 {
		return wire.ErrMessage
	}
	if _, err = wire.RawNonce(hello.DeviceNonce); err != nil {
		return err
	}
	supported := false
	for _, v := range hello.SupportedVersions {
		supported = supported || v == 1
	}
	if !supported {
		return errors.New("unsupported_version")
	}
	if selection.DeviceID != "" && selection.DeviceID != hello.DeviceID {
		return errors.New("unauthorized")
	}
	if port != "" {
		b.mu.Lock()
		b.serialObserved[port] = DiscoveredDevice{DeviceID: hello.DeviceID, Name: hello.DeviceName, ConnectionType: "serial", Port: port, State: "identified"}
		b.mu.Unlock()
	}
	pair, ok := b.store.pair(hello.DeviceID)
	if !ok {
		if !selection.TrustPhysicalConnection || selection.DeviceID != hello.DeviceID {
			return errors.New("physical_pairing_confirmation_required")
		}
		b.pairing.mu.Lock()
		if w := b.pairing.window; w != nil && time.Now().Before(w.expires) {
			b.pairing.mu.Unlock()
			return errors.New("session_conflict")
		}
		b.pairing.window = &pairWindow{deviceID: hello.DeviceID, consent: time.Now(), expires: time.Now().Add(120 * time.Second), sourceIP: "serial"}
		b.pairing.mu.Unlock()
		for {
			f, err = r.next(ctx, 120*time.Second)
			if err != nil {
				return err
			}
			if f.Type != SerialPair {
				return wire.ErrMessage
			}
			plain, err = VerifySerial(f, nil, 2)
			var req wire.PairRequest
			if err != nil || wire.Decode(plain, &req, 2048) != nil || req.ProtocolVersion != 1 || !wire.UUID(req.RequestID) {
				return wire.ErrMessage
			}
			p := b.pairing
			p.mu.Lock()
			w := p.window
			if w == nil || !time.Now().Before(w.expires) {
				p.mu.Unlock()
				return errors.New("pairing_expired")
			}
			switch req.Step {
			case "begin":
				if req.DeviceID != hello.DeviceID || req.DeviceNonce != hello.DeviceNonce || req.DeviceName != hello.DeviceName {
					p.mu.Unlock()
					return wire.ErrMessage
				}
				if w.response == nil {
					w.request = req
					w.response = &wire.PairResponse{ProtocolVersion: 1, PairingID: wire.NewID(), State: "pending", HostID: b.HostID(), HostName: b.opts.HostName, HostNonce: hostNonce, PairingTicket: wire.Nonce()}
				}
			case "status", "confirm":
				if w.response == nil || req.PairingID != w.response.PairingID || subtle.ConstantTimeCompare([]byte(req.PairingTicket), []byte(w.response.PairingTicket)) != 1 {
					p.mu.Unlock()
					return errors.New("unauthorized")
				}
				if req.Step == "confirm" {
					if !req.DeviceConfirmed {
						p.mu.Unlock()
						return wire.ErrMessage
					}
					w.deviceConfirmed = true
					if err = p.approve(w); err != nil {
						p.mu.Unlock()
						return err
					}
				}
			default:
				p.mu.Unlock()
				return wire.ErrMessage
			}
			response := *w.response
			response.ExpiresInMS = max(0, time.Until(w.expires).Milliseconds())
			p.mu.Unlock()
			if err = write(SerialPair, response, nil); err != nil {
				return err
			}
			if response.State == "approved" {
				pair, _ = b.store.pair(hello.DeviceID)
				break
			}
			if response.State == "rejected" {
				return errors.New("pairing_closed")
			}
		}
	} else if value(hello.PairingID) != pair.PairingID {
		return errors.New("unauthorized")
	}
	sid := wire.NewID()
	keys, err := DeriveSerialKeys(b.HostID(), pair.DeviceID, pair.PairingID, sid, hostNonce, hello.DeviceNonce, pair.SerialKey)
	if err != nil {
		return err
	}
	auth := Auth{ProtocolVersion: 1, Step: "challenge", HostID: b.HostID(), DeviceID: pair.DeviceID, PairingID: pair.PairingID, SessionID: sid, HostNonce: hostNonce, DeviceNonce: hello.DeviceNonce}
	if err = write(SerialAuth, auth, nil); err != nil {
		return err
	}
	f, err = r.next(ctx, 2*time.Second)
	if err != nil {
		return err
	}
	plain, err = VerifySerial(f, nil, 2)
	var response Auth
	if err != nil || f.Type != SerialAuth || wire.Decode(plain, &response, 2048) != nil {
		return errors.New("unauthorized")
	}
	expected := auth
	expected.Step = "response"
	expected.Proof = &keys.DeviceProof
	a, _ := json.Marshal(expected)
	c, _ := json.Marshal(response)
	if subtle.ConstantTimeCompare(a, c) != 1 {
		return errors.New("unauthorized")
	}
	// Only authenticated serial candidates may retire an active LAN session.
	b.mu.Lock()
	old := b.sessions[pair.DeviceID]
	if old != nil && old.transport == "serial" {
		b.mu.Unlock()
		return errors.New("session_conflict")
	}
	if old != nil {
		old.cancel()
		if old.client != nil {
			old.client.Stop(errors.New("mode_changed"))
		}
		delete(b.sessions, pair.DeviceID)
	}
	if len(b.sessions) >= 4 {
		b.mu.Unlock()
		return errors.New("capacity_exceeded")
	}
	b.mu.Unlock()
	auth.Step = "result"
	auth.Proof = &keys.HostProof
	auth.Accepted = wire.Ptr(true)
	if err = write(SerialAuth, auth, nil); err != nil {
		return err
	}
	resultSeq := seq
	f, err = r.next(ctx, 2*time.Second)
	if err != nil {
		return err
	}
	plain, err = VerifySerial(f, keys.Frame, 2)
	var ack wire.Ack
	if err != nil || f.Type != SerialAck || wire.Decode(plain, &ack, 1024) != nil || ack.ProtocolVersion != 1 || ack.SessionID != sid || ack.AckSeq != resultSeq || ack.Status != "accepted" {
		return errors.New("unauthorized")
	}
	sessionCtx, sessionCancel := context.WithCancel(ctx)
	s := &session{id: sid, pair: pair, transport: "serial", state: "syncing_config", expires: time.Now().Add(15 * time.Second), ctx: sessionCtx, cancel: sessionCancel, in: make(chan incoming, 4), qosDone: make(chan struct{}, 1), stream: stream, serialKeys: &keys, inputSeq: f.Seq, outputSeq: seq, rate: tokenBucket{tokens: 5, burst: 5, rate: 2, last: time.Now()}}
	b.mu.Lock()
	if b.sessions[pair.DeviceID] != nil || len(b.sessions) >= 4 {
		b.mu.Unlock()
		sessionCancel()
		return errors.New("session_conflict")
	}
	b.sessions[pair.DeviceID] = s
	b.mu.Unlock()
	done := make(chan struct{})
	b.wg.Add(1)
	go func() { defer b.wg.Done(); defer close(done); s.run(b) }()
	defer func() { sessionCancel(); <-done }()
	for {
		f, err = r.next(sessionCtx, 3*time.Second)
		if err != nil {
			return err
		}
		if f.Type == SerialHello || f.Type == SerialAuth || f.Type == SerialPair {
			return errors.New("session_expired")
		}
		plain, err = VerifySerial(f, keys.Frame, 2)
		if err != nil {
			return err
		}
		if f.Seq <= s.serialInputSeq {
			continue
		}
		s.serialInputSeq = f.Seq
		topic := ""
		switch f.Type {
		case SerialConfig:
			topic = "proxy-config"
			plain, err = DecryptSerialConfig(keys.Config, f.Seq, sid, plain)
			if err != nil {
				return err
			}
		case SerialHeartbeat:
			topic = "heartbeat"
		case SerialAck:
			topic = "ack"
		case SerialGoodbye:
			topic = "goodbye"
		case SerialError:
			topic = "error"
		default:
			return wire.ErrMessage
		}
		select {
		case s.in <- incoming{topic, plain, f.Seq}:
		case <-sessionCtx.Done():
			return nil
		}
	}
}
func writeSerialFrame(ctx context.Context, stream io.ReadWriteCloser, t byte, seq uint32, data any, key []byte) error {
	p, e := wire.Marshal(data)
	if e != nil {
		return wire.ErrMessage
	}
	frame, e := EncodeSerial(t, seq, p, key, 1)
	if e != nil {
		return e
	}
	if float64(len(frame))/11520 >= 2 {
		return errors.New("serial_link_capacity")
	}
	done := make(chan error, 1)
	go func() {
		for len(frame) > 0 {
			n, e := stream.Write(frame)
			if e != nil {
				done <- errors.New("serial_write_failed")
				return
			}
			if n == 0 {
				done <- io.ErrShortWrite
				return
			}
			frame = frame[n:]
		}
		done <- nil
	}()
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case e := <-done:
		return e
	case <-ctx.Done():
		stream.Close()
		<-done
		return ctx.Err()
	case <-timer.C:
		stream.Close()
		<-done
		return errTimeout
	}
}
func (s *session) sendSerial(topic string, data any) error {
	types := map[string]byte{"config-ack": SerialConfigAck, "snapshot": SerialSnapshot, "heartbeat": SerialHeartbeat, "ack": SerialAck, "goodbye": SerialGoodbye, "error": SerialError}
	t, ok := types[topic]
	if !ok {
		return wire.ErrMessage
	}
	seq := s.next()
	switch x := data.(type) {
	case wire.Snapshot:
		x.Seq = seq
		data = x
	case wire.Heartbeat:
		x.Seq = seq
		data = x
	}
	data = stampWallClock(data, time.Now())
	return writeSerialFrame(s.ctx, s.stream, t, seq, data, s.serialKeys.Frame)
}

// ProbeSerial reads a selected candidate's protocol name without authenticating
// or switching modes. It uses the same bounded HELLO retry policy as connection.
func (b *Backend) ProbeSerial(ctx context.Context, port string) (DiscoveredDevice, error) {
	b.mu.Lock()
	if b.serialPorts[port] {
		b.mu.Unlock()
		return DiscoveredDevice{}, errors.New("serial_port_busy")
	}
	b.serialPorts[port] = true
	b.mu.Unlock()
	defer func() { b.mu.Lock(); delete(b.serialPorts, port); b.mu.Unlock() }()
	stream, e := OpenSerialPort(port)
	if e != nil {
		return DiscoveredDevice{}, e
	}
	defer stream.Close()
	probeCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	r := readSerial(probeCtx, stream)
	defer func() { cancel(); stream.Close(); <-r.done }()
	stop := context.AfterFunc(probeCtx, func() { stream.Close() })
	defer stop()
	hn := wire.Nonce()
	request := Hello{ProtocolVersion: 1, Step: "request", HostID: b.HostID(), HostName: b.opts.HostName, HostNonce: hn, SupportedVersions: []int{1}}
	for seq := uint32(1); seq <= 3; seq++ {
		if e = writeSerialFrame(probeCtx, stream, SerialHello, seq, request, nil); e != nil {
			return DiscoveredDevice{}, e
		}
		f, err := r.next(probeCtx, 2*time.Second)
		if err != nil {
			continue
		}
		p, err := VerifySerial(f, nil, 2)
		var hello Hello
		if err != nil || f.Type != SerialHello || wire.Decode(p, &hello, 2048) != nil || hello.ProtocolVersion != 1 || hello.Step != "response" || hello.HostNonce != hn || hello.HostID != b.HostID() || !wire.UUID(hello.DeviceID) || !wire.Text(hello.DeviceName, 64) {
			return DiscoveredDevice{}, wire.ErrMessage
		}
		d := DiscoveredDevice{DeviceID: hello.DeviceID, Name: hello.DeviceName, ConnectionType: "serial", Port: port, State: "identified"}
		_, d.Paired = b.store.pair(hello.DeviceID)
		b.mu.Lock()
		b.serialObserved[port] = d
		b.mu.Unlock()
		return d, nil
	}
	return DiscoveredDevice{}, errTimeout
}

type USBIdentity struct {
	VID          string
	PID          string
	SerialNumber string
}

// WatchSerial reconnects a selected USB identity after unplug/rename. A bare COM
// number is deliberately insufficient to enable unattended probing/reconnection.
// It is reserved for USB reconnect and currently has no production caller.
func (b *Backend) WatchSerial(ctx context.Context, id USBIdentity, selection SerialSelection) error {
	if id.SerialNumber == "" || selection.DeviceID == "" {
		return errors.New("stable_usb_identity_required")
	}
	attempt := 0
	for {
		if ctx.Err() != nil {
			return nil
		}
		if b.ctx.Err() != nil {
			return nil
		}
		ports, e := b.ListSerialPorts()
		if e != nil {
			return e
		}
		for _, p := range ports {
			if p.USBSerial == id.SerialNumber && p.VID == id.VID && p.PID == id.PID {
				_ = b.ConnectSerial(ctx, p.Port, selection)
				break
			}
		}
		delay := time.Duration(min(60, 1<<min(attempt, 6))) * time.Second
		attempt++
		select {
		case <-ctx.Done():
			return nil
		case <-b.ctx.Done():
			return nil
		case <-time.After(delay):
		}
	}
}
