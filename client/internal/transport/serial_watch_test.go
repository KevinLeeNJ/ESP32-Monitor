package transport

import (
	"context"
	"io"
	"net"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"esp32-monitor/client/internal/transport/wire"
)

type serialPipeHarness struct {
	mu     sync.Mutex
	ports  []DiscoveredDevice
	opened chan net.Conn
}

func (h *serialPipeHarness) setPorts(ports ...DiscoveredDevice) {
	h.mu.Lock()
	h.ports = ports
	h.mu.Unlock()
}

func (h *serialPipeHarness) enumerate() ([]DiscoveredDevice, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]DiscoveredDevice(nil), h.ports...), nil
}

func (h *serialPipeHarness) open(string) (io.ReadWriteCloser, error) {
	host, device := net.Pipe()
	select {
	case h.opened <- device:
	default:
		device.Close()
		return nil, io.ErrClosedPipe
	}
	return host, nil
}

func newSupervisorBackend(t *testing.T, h *serialPipeHarness) *Backend {
	t.Helper()
	b, err := New(Options{StoreDirectory: filepath.Join(t.TempDir(), "vault"), HostName: "watch-host", BindIP: "127.0.0.1", MQTTAddress: "127.0.0.1:0", HTTPAddress: "127.0.0.1:0", EnumerateSerial: h.enumerate, OpenSerial: h.open})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { b.Close() })
	return b
}

func waitSerialConnected(t *testing.T, b *Backend, device string) {
	t.Helper()
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		if b.SerialConnected(device) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("serial session for %s did not connect", device)
}

func requireSerialIntegration(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("serial integration test")
	}
}

func TestSerialSupervisorConnectsStoredPort(t *testing.T) {
	requireSerialIntegration(t)
	h := &serialPipeHarness{opened: make(chan net.Conn, 8)}
	b := newSupervisorBackend(t, h)
	pair := Pairing{DeviceID: wire.NewID(), DeviceName: "USB Monitor", PairingID: wire.NewID(), DeviceToken: wire.Nonce(), SerialKey: wire.Nonce(), ConsentAt: time.Now().Unix(), WarningVersion: 1, LANConsent: true, SerialPort: "/dev/esp32-watch"}
	if err := b.store.put(pair); err != nil {
		t.Fatal(err)
	}
	h.setPorts(DiscoveredDevice{Name: "USB Monitor", ConnectionType: "serial", Port: pair.SerialPort, VID: "303a", State: "unverified"})
	var conn net.Conn
	select {
	case conn = <-h.opened:
	case <-time.After(8 * time.Second):
		t.Fatal("supervisor did not open the stored port")
	}
	go func() { _ = serveSimulatedSerialLink(conn, b, pair) }()
	waitSerialConnected(t, b, pair.DeviceID)
	if stored, _ := b.store.pair(pair.DeviceID); stored.SerialPort != pair.SerialPort {
		t.Fatal("serial port mapping not retained")
	}
}

func TestSerialSupervisorProbesRenamedPort(t *testing.T) {
	requireSerialIntegration(t)
	h := &serialPipeHarness{opened: make(chan net.Conn, 8)}
	b := newSupervisorBackend(t, h)
	pair := Pairing{DeviceID: wire.NewID(), DeviceName: "USB Monitor", PairingID: wire.NewID(), DeviceToken: wire.Nonce(), SerialKey: wire.Nonce(), ConsentAt: time.Now().Unix(), WarningVersion: 1, LANConsent: true}
	if err := b.store.put(pair); err != nil {
		t.Fatal(err)
	}
	// The stored port is gone; the Monitor now enumerates under a new name.
	const renamed = "/dev/cu.usbmodem2201"
	h.setPorts(DiscoveredDevice{Name: "USB Monitor", ConnectionType: "serial", Port: renamed, VID: "303a", State: "unverified"})
	// The supervisor first sends a read-only HELLO, then opens a fresh link.
	for i := 0; i < 2; i++ {
		var conn net.Conn
		select {
		case conn = <-h.opened:
		case <-time.After(8 * time.Second):
			t.Fatal("supervisor did not probe the renamed port")
		}
		go func() { _ = serveSimulatedSerialLink(conn, b, pair) }()
	}
	waitSerialConnected(t, b, pair.DeviceID)
	if stored, _ := b.store.pair(pair.DeviceID); stored.SerialPort != renamed {
		t.Fatalf("renamed serial port not stored: %q", stored.SerialPort)
	}
}

func TestSerialPendingBlocksLANInvite(t *testing.T) {
	b := testBackend(t)
	pair := Pairing{DeviceID: wire.NewID(), DeviceName: "board", PairingID: wire.NewID(), DeviceToken: wire.Nonce(), SerialKey: wire.Nonce(), ConsentAt: time.Now().Unix(), WarningVersion: 1, LANConsent: true}
	if err := b.store.put(pair); err != nil {
		t.Fatal(err)
	}
	b.mu.Lock()
	b.serialPending[pair.DeviceID] = true
	b.mu.Unlock()
	if _, err := b.invite(pair); err == nil || err.Error() != "session_conflict" {
		t.Fatalf("LAN invite while serial pending: %v", err)
	}
}

func TestSerialReenablePrefersKnownPort(t *testing.T) {
	requireSerialIntegration(t)
	h := &serialPipeHarness{opened: make(chan net.Conn, 8)}
	b := newSupervisorBackend(t, h)
	pair := Pairing{DeviceID: wire.NewID(), DeviceName: "USB Monitor", PairingID: wire.NewID(), DeviceToken: wire.Nonce(), SerialKey: wire.Nonce(), ConsentAt: time.Now().Unix(), WarningVersion: 1, LANConsent: true, SerialPort: "/dev/esp32-watch"}
	if err := b.store.put(pair); err != nil {
		t.Fatal(err)
	}
	h.setPorts(DiscoveredDevice{Name: "USB Monitor", ConnectionType: "serial", Port: pair.SerialPort, VID: "303a"})
	if err := b.SetDeviceEnabled(pair.DeviceID, false); err != nil {
		t.Fatal(err)
	}
	if err := b.SetDeviceEnabled(pair.DeviceID, true); err != nil {
		t.Fatal(err)
	}
	b.mu.Lock()
	pending := b.serialPending[pair.DeviceID]
	b.mu.Unlock()
	if !pending {
		t.Fatal("re-enable did not claim the known USB port before LAN")
	}
	var conn net.Conn
	select {
	case conn = <-h.opened:
	case <-time.After(time.Second):
		t.Fatal("re-enable did not open the known port promptly")
	}
	go func() { _ = serveSimulatedSerialLink(conn, b, pair) }()
	waitSerialConnected(t, b, pair.DeviceID)
}

func TestObservedSerialIdentityWinsOverStoredPort(t *testing.T) {
	b := testBackend(t)
	port := "/dev/cu.test"
	pair := Pairing{DeviceID: wire.NewID(), DeviceName: "old monitor", PairingID: wire.NewID(), DeviceToken: wire.Nonce(), SerialKey: wire.Nonce(), ConsentAt: time.Now().Unix(), WarningVersion: 1, SerialPort: port}
	if err := b.store.put(pair); err != nil {
		t.Fatal(err)
	}
	observed := wire.NewID()
	b.mu.Lock()
	b.serialObserved[port] = DiscoveredDevice{DeviceID: observed, Name: "new monitor", Port: port}
	b.mu.Unlock()
	ports := b.enrichSerialPorts([]DiscoveredDevice{{Port: port}})
	if len(ports) != 1 || ports[0].DeviceID != observed || ports[0].Paired {
		t.Fatalf("stale stored port overrode probe identity: %+v", ports)
	}
}

// serveSimulatedSerialLink answers a read-only probe with HELLO and, when the
// host continues with AUTH, completes enough of the serial session for the
// backend to report connected.
func serveSimulatedSerialLink(conn net.Conn, b *Backend, pair Pairing) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	defer conn.Close()
	reader := readSerial(ctx, conn)
	defer func() { cancel(); conn.Close(); <-reader.done }()
	var seq uint32
	send := func(typ byte, data any, key []byte) error {
		seq++
		p, err := wire.Marshal(data)
		if err != nil {
			return err
		}
		frame, err := EncodeSerial(typ, seq, p, key, 2)
		if err != nil {
			return err
		}
		_, err = conn.Write(frame)
		return err
	}
	recv := func(key []byte) (SerialFrame, []byte, error) {
		f, err := reader.next(ctx, 5*time.Second)
		if err != nil {
			return SerialFrame{}, nil, err
		}
		p, err := VerifySerial(f, key, 1)
		return f, p, err
	}
	f, p, err := recv(nil)
	if err != nil || f.Type != SerialHello {
		return err
	}
	var hello Hello
	if err = wire.Decode(p, &hello, 2048); err != nil {
		return err
	}
	hello.Step = "response"
	hello.DeviceID = pair.DeviceID
	hello.DeviceName = pair.DeviceName
	hello.FirmwareVersion = "watch-sim"
	hello.DeviceNonce = wire.Nonce()
	hello.PairingID = &pair.PairingID
	hello.MaxPayloadBytes = 16368
	if err = send(SerialHello, hello, nil); err != nil {
		return err
	}
	f, p, err = recv(nil)
	if err != nil || f.Type != SerialAuth {
		// A probe reads only the HELLO response and closes the link.
		return nil
	}
	var challenge Auth
	if err = wire.Decode(p, &challenge, 2048); err != nil {
		return err
	}
	keys, err := DeriveSerialKeys(b.HostID(), pair.DeviceID, pair.PairingID, challenge.SessionID, hello.HostNonce, hello.DeviceNonce, pair.SerialKey)
	if err != nil {
		return err
	}
	challenge.Step = "response"
	challenge.Proof = &keys.DeviceProof
	if err = send(SerialAuth, challenge, nil); err != nil {
		return err
	}
	f, p, err = recv(nil)
	if err != nil || f.Type != SerialAuth {
		return err
	}
	var result Auth
	if err = wire.Decode(p, &result, 2048); err != nil || result.Accepted == nil || !*result.Accepted {
		return err
	}
	if err = send(SerialAck, wire.Ack{ProtocolVersion: 1, SessionID: challenge.SessionID, AckSeq: f.Seq, Status: "accepted"}, keys.Frame); err != nil {
		return err
	}
	cfg := disabledConfig()
	cfg.DeviceID = pair.DeviceID
	cfg.SessionID = challenge.SessionID
	data, err := wire.Marshal(cfg)
	if err != nil {
		return err
	}
	wrapped, err := EncryptSerialConfig(keys.Config, seq+1, challenge.SessionID, cfg.RequestID, data)
	if err != nil {
		return err
	}
	if err = send(SerialConfig, wrapped, keys.Frame); err != nil {
		return err
	}
	var lastSnapshot uint32
	for {
		f, p, err = recv(keys.Frame)
		if err != nil {
			return nil
		}
		switch f.Type {
		case SerialConfigAck, SerialAck:
		case SerialSnapshot:
			var snapshot wire.Snapshot
			if err = wire.Decode(p, &snapshot, 16336); err != nil {
				return err
			}
			lastSnapshot = snapshot.Seq
			if err = send(SerialAck, wire.Ack{ProtocolVersion: 1, SessionID: challenge.SessionID, AckSeq: f.Seq, Status: "accepted"}, keys.Frame); err != nil {
				return err
			}
		case SerialHeartbeat:
			if err = send(SerialHeartbeat, wire.Heartbeat{ProtocolVersion: 1, HostID: b.HostID(), DeviceID: pair.DeviceID, SessionID: challenge.SessionID, Seq: seq + 1, SentAt: time.Now().Unix(), LastSnapshotSeq: &lastSnapshot, Epoch: &cfg.Epoch, Revision: &cfg.Revision}, keys.Frame); err != nil {
				return err
			}
			if err = send(SerialAck, wire.Ack{ProtocolVersion: 1, SessionID: challenge.SessionID, AckSeq: f.Seq, Status: "accepted"}, keys.Frame); err != nil {
				return err
			}
		case SerialGoodbye:
			return nil
		}
	}
}
