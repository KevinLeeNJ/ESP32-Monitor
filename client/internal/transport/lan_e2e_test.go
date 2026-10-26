package transport_test

import (
	"context"
	"encoding/json"
	"esp32-monitor/client/internal/transport"
	"esp32-monitor/client/internal/transport/simesp32"
	"esp32-monitor/client/internal/transport/wire"
	"io"
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
func TestLANEndToEndSimulatedESP32(t *testing.T) {
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
	if e = b.Discover(ctx, ""); e != nil {
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
