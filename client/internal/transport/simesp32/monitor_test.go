package simesp32

import (
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"esp32-monitor/client/internal/transport/wire"
)

func udpSocket(t *testing.T) *net.UDPConn {
	t.Helper()
	conn, e := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func exchange(t *testing.T, conn *net.UDPConn, target *net.UDPAddr, msg wire.Discovery) wire.Discovery {
	t.Helper()
	data, e := wire.Marshal(msg)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = conn.WriteToUDP(data, target); e != nil {
		t.Fatal(e)
	}
	if e = conn.SetReadDeadline(time.Now().Add(3 * time.Second)); e != nil {
		t.Fatal(e)
	}
	buf := make([]byte, 1025)
	for {
		n, _, e := conn.ReadFromUDP(buf)
		if e != nil {
			t.Fatal(e)
		}
		var r wire.Discovery
		if wire.Decode(buf[:n], &r, 1024) == nil && r.Type == "monitor_announce" {
			return r
		}
	}
}

// The host client probes the subnet broadcast and loopback with one socket, so
// the simulator receives the same discover twice and answers twice; the offer
// references whichever announce reached the client first. Both announces must
// stay valid until consumed.
func TestPairingOfferPerDiscoverer(t *testing.T) {
	begin := make(chan wire.PairRequest, 1)
	pairing := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(io.LimitReader(r.Body, 4097))
		var req wire.PairRequest
		if wire.Decode(data, &req, 4096) != nil {
			http.Error(w, "bad", 400)
			return
		}
		select {
		case begin <- req:
		default:
		}
		response := wire.PairResponse{ProtocolVersion: 1, PairingID: wire.NewID(), State: "pending", HostID: "host", HostName: "host", HostNonce: wire.Nonce(), PairingTicket: wire.Nonce()}
		out, _ := wire.Marshal(response)
		w.Header().Set("Content-Type", wire.ContentType)
		_, _ = w.Write(out)
	})}
	l, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer pairing.Close()
	go pairing.Serve(l)
	port := l.Addr().(*net.TCPAddr).Port

	m, e := New("测试屏", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	m.Start()
	defer m.Close()
	target, e := net.ResolveUDPAddr("udp4", m.Address())
	if e != nil {
		t.Fatal(e)
	}

	a := udpSocket(t)
	request := wire.Discovery{ProtocolVersion: 1, Type: "monitor_discover", RequestID: wire.NewID(), HostID: "host-a", Nonce: wire.Nonce()}
	announceA := exchange(t, a, target, request)
	announceB := exchange(t, a, target, request)

	offer := announceA
	offer.Type = "monitor_connect_offer"
	offer.HostPort = &port
	data, e := wire.Marshal(offer)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = a.WriteToUDP(data, target); e != nil {
		t.Fatal(e)
	}

	select {
	case req := <-begin:
		if req.Step != "begin" || req.DeviceID != m.DeviceID || req.DeviceName != m.Name {
			t.Fatalf("unexpected pairing request %+v", req)
		}
	case err := <-m.Errors:
		t.Fatal(err)
	case <-time.After(3 * time.Second):
		t.Fatal("offer dropped: pairing begin never arrived")
	}
	_ = announceB
}
