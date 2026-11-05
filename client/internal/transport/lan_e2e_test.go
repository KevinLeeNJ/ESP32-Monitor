package transport_test

import (
	"context"
	"encoding/json"
	"esp32-monitor/client/internal/transport"
	"esp32-monitor/client/internal/transport/simesp32"
	"esp32-monitor/client/internal/transport/wire"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func waitFor(t *testing.T, timeout time.Duration, f func() bool) {
	t.Helper()
	end := time.Now().Add(timeout)
	for time.Now().Before(end) {
		if f() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition timed out")
}

func requireLANIntegration(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("LAN integration test")
	}
}

func TestDiscoverDevicesWithSimulator(t *testing.T) {
	requireLANIntegration(t)
	m, err := simesp32.New("test monitor", "127.0.0.1:0")
	if err != nil {
		t.Skipf("discovery port unavailable: %v", err)
	}
	m.Start()
	defer m.Close()
	b, err := transport.New(transport.Options{StoreDirectory: filepath.Join(t.TempDir(), "vault"), HostName: "test-host", BindIP: "127.0.0.1", MQTTAddress: "127.0.0.1:0", HTTPAddress: "127.0.0.1:0", DiscoveryTarget: m.Address()})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	result := b.DiscoverDevices(ctx)
	for _, device := range result.Devices {
		if device.DeviceID == m.DeviceID && device.ConnectionType == "lan" && device.Address == "127.0.0.1" {
			return
		}
	}
	t.Fatalf("simulated monitor not discovered: %+v", result)
}

func pairSimulatedMonitor(t *testing.T, b *transport.Backend, m *simesp32.Monitor) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if e := b.Discover(ctx); e != nil {
		t.Fatal(e)
	}
	if e := b.BeginPairing(m.DeviceID, true); e != nil {
		t.Fatal(e)
	}
	waitFor(t, 3*time.Second, func() bool { return b.PairingStatus().PairingID != "" })
	if e := b.ConfirmPairing(b.PairingStatus().PairingID, true); e != nil {
		t.Fatal(e)
	}
	waitFor(t, 10*time.Second, func() bool {
		d, _ := b.Devices()
		return len(d) == 1 && d[0].Paired && d[0].ConnectionState == "connected"
	})
}

func TestLANPairingNeedsOnlyDeviceConfirmation(t *testing.T) {
	requireLANIntegration(t)
	m, err := simesp32.New("device-confirm monitor", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	m.Start()
	defer m.Close()
	dir := filepath.Join(t.TempDir(), "vault")
	b, err := transport.New(transport.Options{StoreDirectory: dir, HostName: "test-host", BindIP: "127.0.0.1", MQTTAddress: "127.0.0.1:0", HTTPAddress: "127.0.0.1:0", DiscoveryTarget: m.Address()})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if e := b.Discover(ctx); e != nil {
		t.Fatal(e)
	}
	if e := b.BeginPairing(m.DeviceID, true); e != nil {
		t.Fatal(e)
	}
	// The GUI no longer sends ConfirmPairing after the code window; the
	// Monitor's physical confirmation must be sufficient.
	waitFor(t, 10*time.Second, func() bool {
		d, _ := b.Devices()
		return len(d) == 1 && d[0].Paired && d[0].ConnectionState == "connected"
	})
	// The LAN address is captured at pairing time so a later cold start can
	// probe unicast instead of a broadcast the sleeping monitor may miss.
	raw, e := os.ReadFile(filepath.Join(dir, "companion.json"))
	if e != nil {
		t.Fatal(e)
	}
	var stored struct {
		Pairs map[string]struct {
			LastAddress string `json:"last_address"`
		} `json:"pairs"`
	}
	if e = json.Unmarshal(raw, &stored); e != nil || stored.Pairs[m.DeviceID].LastAddress != "127.0.0.1" {
		t.Fatalf("pairing did not record the LAN address: %s", raw)
	}
}

func TestLANKeepsHeartbeatsWithoutSnapshotRefresh(t *testing.T) {
	requireLANIntegration(t)
	m, err := simesp32.New("snapshot monitor", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	m.Start()
	defer m.Close()
	b, err := transport.New(transport.Options{StoreDirectory: filepath.Join(t.TempDir(), "vault"), HostName: "test-host", BindIP: "127.0.0.1", MQTTAddress: "127.0.0.1:0", HTTPAddress: "127.0.0.1:0", DiscoveryTarget: m.Address()})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	pairSimulatedMonitor(t, b, m)
	var first wire.Snapshot
	select {
	case first = <-m.Snapshots:
	case <-time.After(3 * time.Second):
		t.Fatal("missing initial snapshot")
	}
	for len(m.Heartbeats) > 0 {
		<-m.Heartbeats
	}
	// Semantic pushes alone carry new data; freshness between them must ride on
	// heartbeat leases instead of a periodic full snapshot.
	select {
	case next := <-m.Snapshots:
		t.Fatalf("unexpected unchanged snapshot seq=%d after seq=%d", next.Seq, first.Seq)
	case <-time.After(13 * time.Second):
	}
	timer := time.NewTimer(35 * time.Second)
	defer timer.Stop()
	select {
	case next := <-m.Snapshots:
		t.Fatalf("unexpected repeated snapshot seq=%d while content was unchanged", next.Seq)
	case h := <-m.Heartbeats:
		if h.LastSnapshotSeq == nil || *h.LastSnapshotSeq != first.Seq {
			t.Fatalf("heartbeat does not reference applied snapshot: %+v", h)
		}
	case <-timer.C:
		t.Fatal("heartbeat did not continue without snapshot refresh")
	}
	select {
	case next := <-m.Snapshots:
		t.Fatalf("unexpected repeated snapshot seq=%d after heartbeat", next.Seq)
	default:
	}
}

type fixtureDoer struct{ requests atomic.Int32 }

func (f *fixtureDoer) Do(r *http.Request) (*http.Response, error) {
	f.requests.Add(1)
	code := 200
	body := `{"device_count":0,"devices":[]}`
	if r.URL.Host == "weather.example" {
		code = 401
		body = `{}`
	}
	return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
}

// Windows exposes tentative link-local addresses that cannot be bound before
// the routable interface address, so try candidates rather than taking the
// first one.
func lanBindCandidates() []string {
	var preferred, fallback []string
	ifaces, _ := net.Interfaces()
	for _, i := range ifaces {
		if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagLoopback != 0 {
			continue
		}
		as, _ := i.Addrs()
		for _, a := range as {
			n, ok := a.(*net.IPNet)
			if !ok || n.IP.To4() == nil || n.IP.IsLoopback() {
				continue
			}
			if n.IP.IsLinkLocalUnicast() {
				fallback = append(fallback, n.IP.String())
			} else {
				preferred = append(preferred, n.IP.String())
			}
		}
	}
	return append(preferred, fallback...)
}

// A simulator bound to the same LAN interface must receive its pairing begin
// through that interface. A LAN-bound socket cannot reach loopback on Windows,
// so the simulator cannot live on 127.0.0.1 there.
func TestLANPairingWithLANBind(t *testing.T) {
	requireLANIntegration(t)
	var m *simesp32.Monitor
	var b *transport.Backend
	var attempts []string
	for _, bind := range lanBindCandidates() {
		attempts = append(attempts, bind)
		sim, e := simesp32.New("LAN monitor", net.JoinHostPort(bind, "0"))
		if e != nil {
			continue
		}
		backend, e := transport.New(transport.Options{StoreDirectory: filepath.Join(t.TempDir(), "vault"), HostName: "lan-host", BindIP: bind, MQTTAddress: net.JoinHostPort(bind, "0"), HTTPAddress: net.JoinHostPort(bind, "0"), DiscoveryTarget: sim.Address()})
		if e != nil {
			sim.Close()
			continue
		}
		m, b = sim, backend
		break
	}
	if b == nil {
		t.Skipf("no bindable non-loopback IPv4 address (tried %v)", attempts)
	}
	m.Start()
	defer m.Close()
	defer b.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if e := b.Discover(ctx); e != nil {
		t.Fatal(e)
	}
	if e := b.BeginPairing(m.DeviceID, true); e != nil {
		t.Fatal(e)
	}
	waitFor(t, 3*time.Second, func() bool { return b.PairingStatus().Code != "" })
}

func TestLANEndToEndSimulatedESP32(t *testing.T) {
	requireLANIntegration(t)
	m, e := simesp32.New("客厅 Monitor", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	m.Config.Weather = wire.WeatherConfig{Enabled: true, LocationMode: "coordinates", Latitude: wire.Ptr(31.2), Longitude: wire.Ptr(121.5), Host: wire.Ptr("weather.example"), Key: wire.Ptr("qweather-secret-not-on-disk")}
	m.Config.Devices = wire.DevicesConfig{Enabled: true, URL: wire.Ptr("https://devices.example"), Token: wire.Ptr("device-read-secret-not-on-disk")}
	m.Start()
	defer m.Close()
	dir := filepath.Join(t.TempDir(), "vault")
	upstream := &fixtureDoer{}
	b, e := transport.New(transport.Options{HTTPClient: upstream, StoreDirectory: dir, HostName: "test-agent", BindIP: "127.0.0.1", MQTTAddress: "127.0.0.1:0", HTTPAddress: "127.0.0.1:0", DiscoveryTarget: m.Address()})
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if e = b.BeginPairing(m.DeviceID, false); e == nil || e.Error() != "lan_consent_required" {
		t.Fatal("missing consent allowed")
	}
	if e = b.Discover(ctx); e != nil {
		t.Fatal(e)
	}
	devices, _ := b.Devices()
	if len(devices) != 1 || devices[0].DeviceID != m.DeviceID || devices[0].Name != m.Name || devices[0].Paired {
		t.Fatalf("discovery: %+v", devices)
	}
	if e = b.BeginPairing(m.DeviceID, true); e != nil {
		t.Fatal(e)
	}
	waitFor(t, 3*time.Second, func() bool { return b.PairingStatus().PairingID != "" })
	status := b.PairingStatus()
	if len(status.Code) != 6 {
		t.Fatal("missing comparison code")
	}
	if e = b.ConfirmPairing(status.PairingID, true); e != nil {
		t.Fatal(e)
	}
	select {
	case snap := <-m.Snapshots:
		if snap.DeviceID != m.DeviceID || snap.Sections.Proxy.Data == nil || snap.Sections.GitHub.State != "disabled" {
			t.Fatalf("invalid complete snapshot %+v", snap)
		}
		var proxy struct {
			Time    wire.Section `json:"time"`
			Weather wire.Section `json:"weather"`
			Devices wire.Section `json:"devices"`
		}
		data, _ := json.Marshal(snap.Sections.Proxy.Data)
		json.Unmarshal(data, &proxy)
		if proxy.Time.State != "healthy" || proxy.Weather.State == "disabled" || proxy.Devices.State == "disabled" {
			t.Fatal("proxy statuses incorrect")
		}
	case e := <-m.Errors:
		t.Fatal(e)
	case <-time.After(15 * time.Second):
		t.Fatal("no snapshot")
	}
	waitFor(t, 4*time.Second, func() bool { d, _ := b.Devices(); return len(d) > 0 && d[0].ConnectionState == "connected" })
	data, e := os.ReadFile(filepath.Join(dir, "companion.json"))
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(data), "qweather-secret-not-on-disk") || strings.Contains(string(data), "device-read-secret-not-on-disk") || strings.Contains(string(data), "qweather") || strings.Contains(string(data), "devices_token") || strings.Contains(string(data), "latitude") {
		t.Fatal("proxy config persisted")
	}
	if upstream.requests.Load() != 3 {
		t.Fatalf("expected exactly first weather/AQI/Device fetches, got %d", upstream.requests.Load())
	}
	d, _ := b.Devices()
	active := d[0]
	pre := "emon/v1/" + b.HostID() + "/" + m.DeviceID + "/" + active.SessionID
	if impostor, err := simesp32.DialMQTT(b.MQTTAddress(), m.DeviceID, active.SessionID, "wrong-token", pre); err == nil {
		impostor.Close()
		t.Fatal("impostor accepted")
	}
	d, _ = b.Devices()
	if d[0].SessionID != active.SessionID || d[0].ConnectionState != "connected" {
		t.Fatal("impostor evicted active monitor")
	}
	if e = b.SetDeviceEnabled(m.DeviceID, false); e != nil {
		t.Fatal(e)
	}
	if d, _ = b.Devices(); len(d) != 1 || d[0].Enabled {
		t.Fatalf("disabled state not reported: %+v", d)
	}
	if e = b.SetDeviceEnabled(m.DeviceID, true); e != nil {
		t.Fatal(e)
	}
	if d, _ = b.Devices(); len(d) != 1 || !d[0].Enabled {
		t.Fatalf("enabled state not reported: %+v", d)
	}
	if e = b.Revoke(m.DeviceID); e != nil {
		t.Fatal(e)
	}
	waitFor(t, time.Second, func() bool { d, _ := b.Devices(); return len(d) == 0 })
	if e = b.Close(); e != nil {
		t.Fatal(e)
	}
	s, e := transport.OpenStore(dir)
	if e != nil {
		t.Fatal(e)
	}
	s.Close()
}

func TestLANRevokeSendsGoodbye(t *testing.T) {
	requireLANIntegration(t)
	m, e := simesp32.New("revoke target", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	m.Start()
	defer m.Close()
	dir := filepath.Join(t.TempDir(), "vault")
	b, e := transport.New(transport.Options{StoreDirectory: dir, HostName: "revoke-agent", BindIP: "127.0.0.1", MQTTAddress: "127.0.0.1:0", HTTPAddress: "127.0.0.1:0", DiscoveryTarget: m.Address()})
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	pairSimulatedMonitor(t, b, m)
	if e = b.Revoke(m.DeviceID); e != nil {
		t.Fatal(e)
	}
	select {
	case g := <-m.Goodbyes:
		if g.Reason != "host_revoked" {
			t.Fatalf("unexpected device goodbye reason %q", g.Reason)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("device did not receive host_revoked goodbye")
	}
	waitFor(t, time.Second, func() bool {
		d, _ := b.Devices()
		for _, x := range d {
			if x.DeviceID == m.DeviceID && x.Paired {
				return false
			}
		}
		return true
	})
	data, e := os.ReadFile(filepath.Join(dir, "companion.json"))
	if e != nil {
		t.Fatal(e)
	}
	var stored struct {
		Pairs map[string]json.RawMessage `json:"pairs"`
	}
	if e = json.Unmarshal(data, &stored); e != nil || len(stored.Pairs) != 0 {
		t.Fatalf("credential survived revoke: %s", data)
	}
}

func TestLANResyncRespondsWithSnapshot(t *testing.T) {
	requireLANIntegration(t)
	m, e := simesp32.New("resync monitor", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	m.Start()
	defer m.Close()
	b, e := transport.New(transport.Options{StoreDirectory: filepath.Join(t.TempDir(), "vault"), HostName: "resync-agent", BindIP: "127.0.0.1", MQTTAddress: "127.0.0.1:0", HTTPAddress: "127.0.0.1:0", DiscoveryTarget: m.Address()})
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	pairSimulatedMonitor(t, b, m)
	for len(m.Snapshots) > 0 {
		<-m.Snapshots
	}
	if e = m.SendResync("missing_snapshot", 999); e != nil {
		t.Fatal(e)
	}
	select {
	case snap := <-m.Snapshots:
		if snap.DeviceID != m.DeviceID || snap.Sections.Proxy.Data == nil {
			t.Fatalf("invalid resync snapshot: %+v", snap)
		}
	case e := <-m.Errors:
		t.Fatal(e)
	case <-time.After(5 * time.Second):
		t.Fatal("no snapshot after resync")
	}
}

func TestLANPairingRevokedByDevice(t *testing.T) {
	requireLANIntegration(t)
	m, e := simesp32.New("revoked monitor", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	m.Start()
	defer m.Close()
	dir := filepath.Join(t.TempDir(), "vault")
	b, e := transport.New(transport.Options{StoreDirectory: dir, HostName: "revoke-agent", BindIP: "127.0.0.1", MQTTAddress: "127.0.0.1:0", HTTPAddress: "127.0.0.1:0", DiscoveryTarget: m.Address()})
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	pairSimulatedMonitor(t, b, m)
	if e = m.SendGoodbye("pairing_revoked"); e != nil {
		t.Fatal(e)
	}
	waitFor(t, 3*time.Second, func() bool {
		d, _ := b.Devices()
		for _, x := range d {
			if x.DeviceID == m.DeviceID && x.Paired {
				return false
			}
		}
		return true
	})
	data, e := os.ReadFile(filepath.Join(dir, "companion.json"))
	if e != nil {
		t.Fatal(e)
	}
	var stored struct {
		Pairs map[string]json.RawMessage `json:"pairs"`
	}
	if e = json.Unmarshal(data, &stored); e != nil || len(stored.Pairs) != 0 {
		t.Fatalf("credential survived device revoke: %s", data)
	}
	for len(m.Snapshots) > 0 {
		<-m.Snapshots
	}
	time.Sleep(1500 * time.Millisecond)
	select {
	case snap := <-m.Snapshots:
		t.Fatalf("reconnected after device revoke: %+v", snap)
	default:
	}
}
