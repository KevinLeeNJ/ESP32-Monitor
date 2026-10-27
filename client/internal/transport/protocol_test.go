package transport

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"esp32-monitor/client/internal/transport/wire"
	"google.golang.org/protobuf/encoding/protowire"
	"hash/crc32"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func disabledConfig() wire.Config {
	return wire.Config{ProtocolVersion: 1, RequestID: wire.NewID(), DeviceID: wire.NewID(), SessionID: wire.NewID(), Epoch: wire.NewID(), Revision: 1, Weather: wire.WeatherConfig{LocationMode: "ip"}, Time: wire.TimeConfig{Enabled: true, Offset: 480}}
}
func TestConfigValidation(t *testing.T) {
	base := disabledConfig()
	if e := validateConfig(base, nil); e != nil {
		t.Fatal(e)
	}
	tests := []func(*wire.Config){func(c *wire.Config) { c.Revision = 0 }, func(c *wire.Config) { c.Weather.Key = wire.Ptr("secret") }, func(c *wire.Config) { c.Time.Offset = 841 }, func(c *wire.Config) {
		c.Weather.Enabled = true
		c.Weather.Host = wire.Ptr("https://weather.example")
		c.Weather.Key = wire.Ptr("key")
	}, func(c *wire.Config) {
		c.Devices = wire.DevicesConfig{Enabled: true, URL: wire.Ptr("https://127.0.0.1"), Token: wire.Ptr("token")}
	}, func(c *wire.Config) {
		c.Devices = wire.DevicesConfig{Enabled: true, URL: wire.Ptr("http://server.example"), Token: wire.Ptr("token")}
	}, func(c *wire.Config) {
		c.Devices = wire.DevicesConfig{Enabled: true, URL: wire.Ptr("https://server.example/?key=x"), Token: wire.Ptr("token")}
	}}
	for i, change := range tests {
		c := base
		change(&c)
		if validateConfig(c, nil) == nil {
			t.Errorf("accepted %d", i)
		}
	}
}
func TestStorePersistenceAndScope(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "vault")
	s, e := OpenStore(dir)
	if e != nil {
		t.Fatal(e)
	}
	id := s.HostID()
	pair := Pairing{DeviceID: wire.NewID(), DeviceName: "test", PairingID: wire.NewID(), DeviceToken: wire.Nonce(), SerialKey: wire.Nonce(), ConsentAt: time.Now().Unix(), WarningVersion: 1, LANConsent: true}
	if e = s.put(pair); e != nil {
		t.Fatal(e)
	}
	s.Close()
	s, e = OpenStore(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if s.HostID() != id {
		t.Fatal("host changed")
	}
	p, ok := s.pair(pair.DeviceID)
	if !ok || p.SerialKey != pair.SerialKey {
		t.Fatal("pair not persisted")
	}
	info, _ := os.Stat(filepath.Join(dir, "companion.json"))
	if !privateStorePath(filepath.Join(dir, "companion.json"), info) {
		t.Fatal("vault permissions")
	}
	if e = s.remove(pair.DeviceID); e != nil {
		t.Fatal(e)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "companion.json"))
	if bytes.Contains(data, []byte(pair.DeviceToken)) {
		t.Fatal("revoked secret remains")
	}
}
func TestSerialCRCFragmentsNoiseRecoveryAndTamper(t *testing.T) {
	if crc32.ChecksumIEEE([]byte("123456789")) != 0xcbf43926 {
		t.Fatal("CRC vector")
	}
	data, _ := wire.Marshal(wire.Goodbye{ProtocolVersion: 1, SessionID: "EMON", Reason: "test"})
	key := bytes.Repeat([]byte{7}, 32)
	good, e := EncodeSerial(SerialHeartbeat, 42, data, key, 2)
	if e != nil {
		t.Fatal(e)
	}
	decoder := SerialDecoder{}
	now := time.Now()
	var frames []SerialFrame
	for _, b := range append([]byte("ROM boot logs\n"), good...) {
		frames = append(frames, decoder.Feed([]byte{b}, now)...)
	}
	if len(frames) != 1 {
		t.Fatalf("%d frames", len(frames))
	}
	if p, e := VerifySerial(frames[0], key, 2); e != nil || !bytes.Equal(p, data) {
		t.Fatal(e)
	}
	if _, e = VerifySerial(frames[0], key, 1); e == nil {
		t.Fatal("direction reflection")
	}
	frames[0].Payload[0] ^= 1
	if _, e = VerifySerial(frames[0], key, 2); e == nil {
		t.Fatal("tampering accepted")
	}
	corrupt := append([]byte(nil), good...)
	corrupt[12] ^= 1
	decoder = SerialDecoder{}
	frames = decoder.Feed(append(corrupt, good...), now)
	if len(frames) != 1 {
		t.Fatal("bad CRC swallowed following good frame")
	}
	decoder = SerialDecoder{}
	partial := append([]byte(nil), good[:12]...)
	partial[6] = 0xf0
	partial[7] = 0x3f
	frames = decoder.Feed(append(partial, good...), now)
	if len(frames) != 0 {
		t.Fatal("incomplete frame accepted")
	}
	frames = decoder.Tick(now.Add(2100 * time.Millisecond))
	if len(frames) != 1 {
		t.Fatal("half-frame deadline lost good suffix")
	}
}
func TestSerialKeysAndEncryptedConfig(t *testing.T) {
	host, device, pair, session := wire.NewID(), wire.NewID(), wire.NewID(), wire.NewID()
	hn, dn, key := wire.Nonce(), wire.Nonce(), wire.Nonce()
	k, e := DeriveSerialKeys(host, device, pair, session, hn, dn, key)
	if e != nil {
		t.Fatal(e)
	}
	if k.DeviceProof == k.HostProof || bytes.Equal(k.Frame, k.Config) {
		t.Fatal("keys not separated")
	}
	other, _ := DeriveSerialKeys(host, device, pair, wire.NewID(), hn, dn, key)
	if bytes.Equal(k.Frame, other.Frame) {
		t.Fatal("cross session replay")
	}
	c := disabledConfig()
	c.SessionID = session
	plain, _ := wire.Marshal(c)
	enc, e := EncryptSerialConfig(k.Config, 5, session, c.RequestID, plain)
	if e != nil {
		t.Fatal(e)
	}
	wrapped, _ := wire.Marshal(enc)
	decoded, e := DecryptSerialConfig(k.Config, 5, session, wrapped)
	if e != nil || !bytes.Equal(decoded, plain) {
		t.Fatal(e)
	}
	if _, e = DecryptSerialConfig(k.Config, 6, session, wrapped); e == nil {
		t.Fatal("nonce mismatch")
	}
	enc.Tag = bytes.Repeat([]byte{0}, 16)
	wrapped, _ = wire.Marshal(enc)
	if _, e = DecryptSerialConfig(k.Config, 5, session, wrapped); e == nil {
		t.Fatal("tampered tag")
	}
}

type doerFunc func(*http.Request) (*http.Response, error)

func (f doerFunc) Do(r *http.Request) (*http.Response, error) { return f(r) }
func TestProxyMemoryOnlyIsolationAndFailureAge(t *testing.T) {
	calls := make(chan string, 10)
	client := doerFunc(func(r *http.Request) (*http.Response, error) {
		calls <- r.Header.Get("Authorization")
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"device_count":0,"devices":[]}`))}, nil
	})
	c := disabledConfig()
	c.Devices = wire.DevicesConfig{Enabled: true, URL: wire.Ptr("https://server.example"), Token: wire.Ptr("private-token-one")}
	p := newProxy(context.Background(), c, client)
	defer p.stop()
	select {
	case token := <-calls:
		if token != "Bearer private-token-one" {
			t.Fatal(token)
		}
	case <-time.After(time.Second):
		t.Fatal("no initial fetch")
	}
	for i := 0; i < 100; i++ {
		if p.snapshot(false).Devices.State == "healthy" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	for i := 0; i < 5; i++ {
		p.snapshot(false)
	}
	select {
	case <-calls:
		t.Fatal("snapshot triggered upstream poll")
	default:
	}
	p.mu.Lock()
	before := p.data.Devices.LastSuccessAt
	p.update(&p.data.Devices, nil, io.EOF)
	after := p.data.Devices.LastSuccessAt
	p.mu.Unlock()
	if before == nil || after == nil || *before != *after {
		t.Fatal("failure renewed data age")
	}
	if d := p.snapshot(false); d.Devices.State != "degraded" || d.Time.Data.(wire.TimeData).Trusted {
		t.Fatal("failure/trust misreported")
	}
	other := newProxy(context.Background(), disabledConfig(), client)
	defer other.stop()
	if other.snapshot(false).Devices.Data != nil {
		t.Fatal("cross device cache")
	}
}

// Fixed vectors independently generated with Python hashlib/hmac/struct, rather
// than deriving expected values with the implementation under test.
func TestIndependentAuthenticationVectors(t *testing.T) {
	host := "11111111-1111-4111-8111-111111111111"
	device := "22222222-2222-4222-8222-222222222222"
	pair := "33333333-3333-4333-8333-333333333333"
	session := "44444444-4444-4444-8444-444444444444"
	nonce := func(b byte) string { return base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{b}, 32)) }
	key := nonce(3)
	keys, e := DeriveSerialKeys(host, device, pair, session, nonce(1), nonce(2), key)
	if e != nil {
		t.Fatal(e)
	}
	if hex.EncodeToString(keys.Frame) != "5585de967095e0ac73af32f48bd236c7910a054eb17911ae159ba79fa2de7e05" || hex.EncodeToString(keys.Config) != "2cd902bee8d60808ff7396a204e60d0c7ff39a6bdf9553dd90e2c30cf0583035" || keys.DeviceProof != "e20742d25a921bcca4d1888d3d4164420bf29964008218aaf39a76db125db6aa" || keys.HostProof != "5ac16db3d5eb1f11b4cbab2d0b7fa4fb10e96f5139041e7063bb55f0b3440d08" {
		t.Fatal("serial interoperability vector mismatch")
	}
	d := wire.Discovery{ProtocolVersion: 1, Type: "monitor_discover", RequestID: session, HostID: host, DeviceID: &device, PairingID: &pair, Nonce: nonce(1)}
	if DiscoveryMAC(d, key) != "267bd0e53aa9f7a6c0e357caaeaa1bd8b82528a80627c0d286f1f9457d6e4bc5" {
		t.Fatal("discovery interoperability vector mismatch")
	}
	d.Type = "monitor_connect_offer"
	if DiscoveryMAC(d, key) == "267bd0e53aa9f7a6c0e357caaeaa1bd8b82528a80627c0d286f1f9457d6e4bc5" {
		t.Fatal("message-type reflection")
	}
}

func TestSerialProtobufVersionAndBinaryLimits(t *testing.T) {
	data, err := wire.Marshal(wire.Goodbye{ProtocolVersion: 1, SessionID: "session", Reason: "done"})
	if err != nil {
		t.Fatal(err)
	}
	key := bytes.Repeat([]byte{7}, 32)
	good, err := EncodeSerial(SerialGoodbye, 1, data, key, 2)
	if err != nil {
		t.Fatal(err)
	}
	old := append([]byte{}, good...)
	old[4] = 1
	binary.LittleEndian.PutUint32(old[len(old)-4:], crc32.ChecksumIEEE(old[4:len(old)-4]))
	decoder := SerialDecoder{}
	frames := decoder.Feed(append(old, good...), time.Now())
	if len(frames) != 1 || frames[0].Header[4] != wire.WireVersion {
		t.Fatal("old serial version accepted or new frame lost")
	}
	if _, err = EncodeSerial(SerialGoodbye, 1, []byte(`{"protocol_version":1}`), key, 2); err == nil {
		t.Fatal("legacy JSON accepted")
	}
	// Unknown length-delimited fields can legally extend a future message to the
	// binary boundary, independently of text/UTF-8 size. Embedded NUL is valid.
	base := protowire.AppendTag(append([]byte{}, data...), 100, protowire.BytesType)
	maximum := protowire.AppendBytes(base, make([]byte, 16336-len(base)-2))
	if len(maximum) != 16336 {
		t.Fatal("incorrect fixture boundary")
	}
	frame, err := EncodeSerial(SerialGoodbye, 2, maximum, key, 2)
	if err != nil || len(frame) != 16384 {
		t.Fatalf("max frame %d %v", len(frame), err)
	}
	tooLarge := protowire.AppendBytes(base, make([]byte, 16337-len(base)-2))
	if _, err = EncodeSerial(SerialGoodbye, 2, tooLarge, key, 2); err == nil {
		t.Fatal("oversized authenticated frame")
	}
	if _, err = EncodeSerial(SerialGoodbye, 2, maximum, nil, 2); err == nil {
		t.Fatal("oversized unauthenticated handshake")
	}
}

func TestPairingHTTPProtobufCapabilitiesAndLegacyRejection(t *testing.T) {
	b := testBackend(t)
	b.pairing.mu.Lock()
	b.pairing.window = &pairWindow{sourceIP: "127.0.0.1", expires: time.Now().Add(time.Minute)}
	b.pairing.mu.Unlock()
	request := httptest.NewRequest(http.MethodGet, "/companion/v1/capabilities", nil)
	request.RemoteAddr = "127.0.0.1:1234"
	response := httptest.NewRecorder()
	b.pairing.ServeHTTP(response, request)
	var caps wire.Capabilities
	if response.Code != 200 || response.Header().Get("Content-Type") != wire.ContentType || wire.Decode(response.Body.Bytes(), &caps, 4096) != nil || caps.ProtocolVersion != 1 || caps.MQTT.Port == 0 {
		t.Fatal("capabilities not protobuf")
	}
	for _, contentType := range []string{"application/json", wire.ContentType} {
		request = httptest.NewRequest(http.MethodPost, "/companion/v1/pair", strings.NewReader(`{"protocol_version":1}`))
		request.RemoteAddr = "127.0.0.1:1234"
		request.Header.Set("Content-Type", contentType)
		response = httptest.NewRecorder()
		b.pairing.ServeHTTP(response, request)
		var failure wire.Error
		want := 400
		if contentType == "application/json" {
			want = 415
		}
		if response.Code != want || wire.Decode(response.Body.Bytes(), &failure, 4096) != nil || failure.Error.Code != "invalid_message" {
			t.Fatal("legacy JSON not rejected with protobuf error")
		}
	}
}
