package transport

import (
	"context"
	"esp32-monitor/client/internal/transport/wire"
	mqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/packets"
	"io"
	"net"
	"path/filepath"
	"testing"
	"time"
)

func testBackend(t *testing.T) *Backend {
	t.Helper()
	b, e := New(Options{StoreDirectory: filepath.Join(t.TempDir(), "vault"), HostName: "test-host", BindIP: "127.0.0.1", MQTTAddress: "127.0.0.1:0", HTTPAddress: "127.0.0.1:0"})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { b.Close() })
	return b
}
func TestBrokerACLAndRejectBeforeTakeover(t *testing.T) {
	b := testBackend(t)
	pair := Pairing{DeviceID: wire.NewID(), DeviceName: "board", PairingID: wire.NewID(), DeviceToken: wire.Nonce(), SerialKey: wire.Nonce(), ConsentAt: time.Now().Unix(), WarningVersion: 1, LANConsent: true}
	if e := b.store.put(pair); e != nil {
		t.Fatal(e)
	}
	sid, e := b.invite(pair)
	if e != nil {
		t.Fatal(e)
	}
	b.mu.Lock()
	s := b.sessions[pair.DeviceID]
	cl := &mqtt.Client{ID: "emon-" + pair.DeviceID}
	s.client = cl
	b.mu.Unlock()
	defer func() { b.mu.Lock(); delete(b.sessions, pair.DeviceID); b.mu.Unlock(); s.cancel() }()
	h := &brokerHook{backend: b}
	pre := prefix(b.HostID(), pair.DeviceID, sid)
	for _, c := range []struct {
		topic string
		write bool
		allow bool
	}{{pre + "/up/proxy-config", true, true}, {pre + "/down/#", false, true}, {pre + "/down/snapshot", false, true}, {"#", false, false}, {"$SYS/#", false, false}, {pre + "/up/#", false, false}, {pre + "/down/+", false, false}, {pre + "/down/snapshot", true, false}, {prefix(b.HostID(), wire.NewID(), sid) + "/down/#", false, false}, {pre + "/up/shell", true, false}} {
		if h.OnACLCheck(cl, c.topic, c.write) != c.allow {
			t.Errorf("ACL %s", c.topic)
		}
	}
	attack := &mqtt.Client{ID: cl.ID}
	p := packets.Packet{ProtocolVersion: 4, Connect: packets.ConnectParams{ClientIdentifier: cl.ID, Username: []byte(pair.DeviceID + ":" + sid), Password: []byte(pair.DeviceToken), Clean: true, Keepalive: 60}}
	if h.OnConnectAuthenticate(attack, p) {
		t.Fatal("duplicate active identity took over")
	}
	b.mu.Lock()
	if s.client != cl {
		t.Error("active session evicted")
	}
	b.mu.Unlock()
	packet := packets.Packet{FixedHeader: packets.FixedHeader{Type: packets.Publish, Retain: true, Qos: 1}, TopicName: pre + "/up/proxy-config", Payload: []byte(`{}`)}
	if _, e = h.OnPacketRead(cl, packet); e == nil {
		t.Fatal("retained accepted")
	}
}

type discardStream struct{}

func (discardStream) Read([]byte) (int, error)    { return 0, io.EOF }
func (discardStream) Write(b []byte) (int, error) { return len(b), nil }
func (discardStream) Close() error                { return nil }

func TestRejectedConfigAppearsInDeviceStatus(t *testing.T) {
	b := testBackend(t)
	cfg := disabledConfig()
	pair := Pairing{DeviceID: cfg.DeviceID, DeviceName: "board", PairingID: wire.NewID(), DeviceToken: wire.Nonce(), SerialKey: wire.Nonce(), ConsentAt: time.Now().Unix(), WarningVersion: 1, LANConsent: true}
	if err := b.store.put(pair); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &session{id: cfg.SessionID, pair: pair, ctx: ctx, cancel: cancel, transport: "serial", state: "syncing_config", stream: discardStream{}, serialKeys: &SerialKeys{Frame: make([]byte, 32)}, in: make(chan incoming, 1), rate: tokenBucket{tokens: 100, rate: 100, burst: 100, last: time.Now()}, expires: time.Now().Add(time.Minute)}
	done := make(chan struct{})
	go func() { s.run(b); close(done) }()
	invalid := cfg
	invalid.Revision = 0
	raw, err := wire.Marshal(invalid)
	if err != nil {
		t.Fatal(err)
	}
	s.in <- incoming{topic: "proxy-config", data: raw, seq: 1}
	deadline := time.Now().Add(time.Second)
	for {
		b.mu.Lock()
		got := b.configErrors[pair.DeviceID]
		b.mu.Unlock()
		if got == "invalid_config" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("missing config rejection")
		}
		time.Sleep(time.Millisecond)
	}
	statuses, _ := b.Devices()
	if len(statuses) != 1 || statuses[0].ConfigError != "invalid_config" {
		t.Fatalf("statuses: %+v", statuses)
	}
	cancel()
	<-done
}
func TestConfigTransactionsNeverPartiallyApply(t *testing.T) {
	b := testBackend(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cfg := disabledConfig()
	s := &session{id: cfg.SessionID, pair: Pairing{DeviceID: cfg.DeviceID}, ctx: ctx, cancel: cancel, transport: "serial", state: "syncing_config", stream: discardStream{}, serialKeys: &SerialKeys{Frame: make([]byte, 32)}, transactions: map[string]transaction{}, rate: tokenBucket{tokens: 100, rate: 100, burst: 100, last: time.Now()}}
	defer func() {
		if s.proxy != nil {
			s.proxy.stop()
		}
	}()
	send := func(c wire.Config, seq uint32) error {
		data, _ := wire.Marshal(c)
		return s.handle(b, incoming{topic: "proxy-config", data: data, seq: seq})
	}
	if e := send(cfg, 1); e != nil {
		t.Fatal(e)
	}
	old := s.proxy
	if e := send(cfg, 2); e != nil || s.proxy != old {
		t.Fatal("idempotent configuration restarted worker", e)
	}
	changed := cfg
	changed.Revision = 2
	if e := send(changed, 3); e == nil || e.Error() != "invalid_message" {
		t.Fatal("request reuse accepted", e)
	}
	changed = cfg
	changed.RequestID = wire.NewID()
	changed.Time.Offset = 0
	if e := send(changed, 4); e == nil || e.Error() != "config_conflict" {
		t.Fatal("same-revision mutation accepted", e)
	}
	if s.config.Time.Offset != 480 {
		t.Fatal("failed config partially applied")
	}
	changed.Revision = 2
	if e := send(changed, 5); e != nil {
		t.Fatal(e)
	}
	select {
	case <-old.done:
	default:
		t.Fatal("old generation still running")
	}
	if s.config.Time.Offset != 0 || s.proxy == old {
		t.Fatal("new generation not applied")
	}
}
func TestRebindPreservesStableIdentity(t *testing.T) {
	b := testBackend(t)
	id := b.HostID()
	old := b.MQTTAddress()
	b.networkChangeMu.Lock()
	e := b.rebind("127.0.0.1", true)
	b.networkChangeMu.Unlock()
	if e != nil {
		t.Fatal(e)
	}
	if b.HostID() != id || b.bindIP() != "127.0.0.1" || b.MQTTAddress() == old {
		t.Fatal("identity changed")
	}
	current := b.MQTTAddress()
	if e := b.UpdateLANInterface("192.0.2.1"); e == nil {
		t.Fatal("unassigned interface accepted")
	}
	if b.MQTTAddress() != current || b.HostID() != id {
		t.Fatal("failed listener replacement did not roll back")
	}
}

func TestSerialDiscoveryHidesNonUSBVirtualPorts(t *testing.T) {
	b, e := New(Options{StoreDirectory: filepath.Join(t.TempDir(), "vault"), HostName: "frontend-host", BindIP: "127.0.0.1", MQTTAddress: "127.0.0.1:0", HTTPAddress: "127.0.0.1:0", EnumerateSerial: func() ([]DiscoveredDevice, error) {
		return []DiscoveredDevice{
			{Name: "USB JTAG serial", ConnectionType: "serial", Port: "/dev/cu.usbmodem1101", VID: "303A", PID: "1001", State: "unverified"},
			{Name: "/dev/cu.debug-console", ConnectionType: "serial", Port: "/dev/cu.debug-console", State: "unverified"},
			{Name: "/dev/cu.Bluetooth-Incoming-Port", ConnectionType: "serial", Port: "/dev/cu.Bluetooth-Incoming-Port", State: "unverified"},
		}, nil
	}})
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	ports, e := b.ListSerialPorts()
	if e != nil {
		t.Fatal(e)
	}
	if len(ports) != 1 || ports[0].Port != "/dev/cu.usbmodem1101" {
		t.Fatalf("unexpected serial candidates: %+v", ports)
	}
}

func TestLikelyMonitorSerialRecognizesWindowsPorts(t *testing.T) {
	cases := []struct {
		name string
		port DiscoveredDevice
		want bool
	}{
		{"esp32 usb jtag", DiscoveredDevice{Port: "COM5", VID: "303A"}, true},
		{"esp32 usb jtag prefixed vid", DiscoveredDevice{Port: "COM10", VID: "0x303A"}, true},
		{"cp210x", DiscoveredDevice{Port: "COM12", VID: "10C4"}, true},
		{"ch340", DiscoveredDevice{Port: "COM7", VID: "1A86"}, true},
		{"ftdi", DiscoveredDevice{Port: "COM11", VID: "0403"}, true},
		{"unknown usb serial", DiscoveredDevice{Port: "COM6"}, false},
		{"bluetooth virtual port", DiscoveredDevice{Port: "COM4", VID: ""}, false},
		{"linux usb serial", DiscoveredDevice{Port: "/dev/ttyUSB0"}, true},
		{"mac usb modem", DiscoveredDevice{Port: "/dev/cu.usbmodem1101"}, true},
		{"mac bluetooth port", DiscoveredDevice{Port: "/dev/cu.Bluetooth-Incoming-Port"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := likelyMonitorSerial(c.port); got != c.want {
				t.Fatalf("likelyMonitorSerial(%+v) = %v, want %v", c.port, got, c.want)
			}
		})
	}
}

func TestPairedSerialPortKeepsIdentityFromStore(t *testing.T) {
	b, e := New(Options{StoreDirectory: filepath.Join(t.TempDir(), "vault"), HostName: "frontend-host", BindIP: "127.0.0.1", MQTTAddress: "127.0.0.1:0", HTTPAddress: "127.0.0.1:0", EnumerateSerial: func() ([]DiscoveredDevice, error) {
		return []DiscoveredDevice{{Name: "USB JTAG serial", ConnectionType: "serial", Port: "/dev/cu.usbmodem1101", VID: "303A", PID: "1001", State: "unverified"}}, nil
	}})
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	pair := Pairing{DeviceID: wire.NewID(), DeviceName: "USB Monitor", PairingID: wire.NewID(), DeviceToken: wire.Nonce(), SerialKey: wire.Nonce(), ConsentAt: time.Now().Unix(), WarningVersion: 1, SerialPort: "/dev/cu.usbmodem1101"}
	if e = b.store.put(pair); e != nil {
		t.Fatal(e)
	}
	ports, e := b.ListSerialPorts()
	if e != nil {
		t.Fatal(e)
	}
	if len(ports) != 1 || !ports[0].Paired || ports[0].DeviceID != pair.DeviceID || ports[0].Name != pair.DeviceName {
		t.Fatalf("paired serial identity not restored: %+v", ports)
	}
}

func TestUnifiedDiscoveryReportsNamesAndConnectionTypes(t *testing.T) {
	b, e := New(Options{StoreDirectory: filepath.Join(t.TempDir(), "vault"), HostName: "frontend-host", BindIP: "127.0.0.1", MQTTAddress: "127.0.0.1:0", HTTPAddress: "127.0.0.1:0", DiscoveryTarget: "127.0.0.1:18445", EnumerateSerial: func() ([]DiscoveredDevice, error) {
		return []DiscoveredDevice{{Name: "USB JTAG serial", ConnectionType: "serial", Port: "/dev/cu.test", VID: "303A", PID: "1001", State: "unverified"}}, nil
	}})
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	id := wire.NewID()
	b.mu.Lock()
	b.candidates[id] = candidate{Message: wire.Discovery{DeviceID: &id, DeviceName: wire.Ptr("书房 Monitor")}, Addr: &net.UDPAddr{IP: net.ParseIP("192.168.1.100"), Port: 18444}, Seen: time.Now()}
	b.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	result := b.DiscoverDevices(ctx)
	if len(result.Devices) != 2 || result.SerialError != "" {
		t.Fatalf("%+v", result)
	}
	byType := map[string]DiscoveredDevice{}
	for _, d := range result.Devices {
		byType[d.ConnectionType] = d
	}
	if byType["lan"].Name != "书房 Monitor" || byType["serial"].Name != "USB JTAG serial" || byType["serial"].Port != "/dev/cu.test" || byType["serial"].IdentityVerified {
		t.Fatalf("%+v", result.Devices)
	}
}
