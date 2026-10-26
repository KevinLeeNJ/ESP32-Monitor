package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"esp32-monitor/client/internal/transport/wire"
	"net"
	"path/filepath"
	"testing"
	"time"
)

func TestSerialEndToEndSimulatedMonitor(t *testing.T) {
	b, e := New(Options{StoreDirectory: filepath.Join(t.TempDir(), "vault"), HostName: "serial-host", BindIP: "127.0.0.1", MQTTAddress: "127.0.0.1:0", HTTPAddress: "127.0.0.1:0"})
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	pair := Pairing{DeviceID: wire.NewID(), DeviceName: "USB Monitor", PairingID: wire.NewID(), DeviceToken: wire.Nonce(), SerialKey: wire.Nonce(), ConsentAt: time.Now().Unix(), WarningVersion: 1}
	if e = b.store.put(pair); e != nil {
		t.Fatal(e)
	}
	host, device := net.Pipe()
	defer device.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	hostDone := make(chan error, 1)
	go func() { hostDone <- b.AttachSerial(ctx, host, SerialSelection{DeviceID: pair.DeviceID}) }()
	reader := readSerial(ctx, device)
	defer func() { device.Close(); <-reader.done }()
	var seq uint32
	send := func(typ byte, data any, key []byte) {
		t.Helper()
		seq++
		p, _ := json.Marshal(data)
		frame, e := EncodeSerial(typ, seq, p, key, 2)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = device.Write(frame); e != nil {
			t.Fatal(e)
		}
	}
	recv := func(key []byte) (SerialFrame, []byte) {
		t.Helper()
		f, e := reader.next(ctx, 4*time.Second)
		if e != nil {
			t.Fatal(e)
		}
		p, e := VerifySerial(f, key, 1)
		if e != nil {
			t.Fatal(e)
		}
		return f, p
	}
	f, p := recv(nil)
	if f.Type != SerialHello {
		t.Fatal("missing hello")
	}
	var hello Hello
	json.Unmarshal(p, &hello)
	hello.Step = "response"
	hello.DeviceID = pair.DeviceID
	hello.DeviceName = pair.DeviceName
	hello.FirmwareVersion = "sim-serial"
	hello.DeviceNonce = wire.Nonce()
	hello.PairingID = &pair.PairingID
	hello.MaxPayloadBytes = 16368
	send(SerialHello, hello, nil)
	f, p = recv(nil)
	if f.Type != SerialAuth {
		t.Fatal("missing challenge")
	}
	var auth Auth
	json.Unmarshal(p, &auth)
	keys, e := DeriveSerialKeys(b.HostID(), pair.DeviceID, pair.PairingID, auth.SessionID, hello.HostNonce, hello.DeviceNonce, pair.SerialKey)
	if e != nil {
		t.Fatal(e)
	}
	auth.Step = "response"
	auth.Proof = &keys.DeviceProof
	send(SerialAuth, auth, nil)
	f, p = recv(nil)
	var result Auth
	json.Unmarshal(p, &result)
	if value(result.Proof) != keys.HostProof || result.Accepted == nil || !*result.Accepted {
		t.Fatal("host auth failed")
	}
	send(SerialAck, wire.Ack{ProtocolVersion: 1, SessionID: auth.SessionID, AckSeq: f.Seq, Status: "accepted"}, keys.Frame)
	cfg := disabledConfig()
	cfg.DeviceID = pair.DeviceID
	cfg.SessionID = auth.SessionID
	data, _ := json.Marshal(cfg)
	wrapped, e := EncryptSerialConfig(keys.Config, seq+1, auth.SessionID, cfg.RequestID, data)
	if e != nil {
		t.Fatal(e)
	}
	send(SerialConfig, wrapped, keys.Frame)
	f, p = recv(keys.Frame)
	if f.Type != SerialConfigAck {
		t.Fatalf("expected config ack, type=%x payload=%s", f.Type, p)
	}
	f, p = recv(keys.Frame)
	if f.Type != SerialAck {
		t.Fatal("configuration frame not ACKed")
	}
	f, p = recv(keys.Frame)
	if f.Type != SerialSnapshot {
		t.Fatalf("expected snapshot, type=%x", f.Type)
	}
	var snapshot wire.Snapshot
	if e = wire.Decode(p, &snapshot, 16336); e != nil || snapshot.Seq != f.Seq {
		t.Fatal("wire snapshot invalid", e)
	}
	send(SerialAck, wire.Ack{ProtocolVersion: 1, SessionID: auth.SessionID, AckSeq: f.Seq, Status: "accepted"}, keys.Frame)
	for i := 0; i < 2; i++ {
		f, p = recv(keys.Frame)
		if f.Type != SerialHeartbeat {
			t.Fatalf("expected heartbeat, type=%x", f.Type)
		}
		var hb wire.Heartbeat
		json.Unmarshal(p, &hb)
		if hb.Seq != f.Seq || hb.Time == nil || hb.Time.Trusted {
			t.Fatal("heartbeat time/sequence")
		}
		send(SerialHeartbeat, wire.Heartbeat{ProtocolVersion: 1, HostID: b.HostID(), DeviceID: pair.DeviceID, SessionID: auth.SessionID, Seq: seq + 1, SentAt: time.Now().Unix(), LastSnapshotSeq: &snapshot.Seq, Epoch: &cfg.Epoch, Revision: &cfg.Revision}, keys.Frame)
		send(SerialAck, wire.Ack{ProtocolVersion: 1, SessionID: auth.SessionID, AckSeq: f.Seq, Status: "accepted"}, keys.Frame)
	}
	device.Close()
	select {
	case <-hostDone:
	case <-time.After(time.Second):
		t.Fatal("serial unplug did not release session")
	}
	b.mu.Lock()
	left := b.sessions[pair.DeviceID]
	b.mu.Unlock()
	if left != nil {
		t.Fatal("old serial session retained")
	}
}

func TestSerialInvalidHandshakeReleasesFloodedReader(t *testing.T) {
	b := testBackend(t)
	host, device := net.Pipe()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	defer device.Close()
	done := make(chan error, 1)
	go func() { done <- b.AttachSerial(ctx, host, SerialSelection{}) }()
	buf := make([]byte, 2048)
	if _, e := device.Read(buf); e != nil {
		t.Fatal(e)
	}
	bad, _ := EncodeSerial(SerialHello, 1, []byte(`{"invalid":true}`), nil, 2)
	burst := bytes.Repeat(bad, 20)
	go func() { _, _ = device.Write(burst) }()
	select {
	case e := <-done:
		if e == nil {
			t.Fatal("invalid HELLO accepted")
		}
	case <-time.After(time.Second):
		t.Fatal("flooded reader not released")
	}
}
