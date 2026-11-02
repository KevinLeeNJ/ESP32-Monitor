package transport

import (
	"context"
	"net"
	"testing"
	"time"

	"esp32-monitor/client/internal/transport/wire"
)

func TestPairingLostHintKeepsCredential(t *testing.T) {
	b := testBackend(t)
	pair := Pairing{DeviceID: wire.NewID(), DeviceName: "board", PairingID: wire.NewID(), DeviceToken: wire.Nonce(), SerialKey: wire.Nonce(), ConsentAt: time.Now().Unix(), WarningVersion: 1, LANConsent: true}
	if e := b.store.put(pair); e != nil {
		t.Fatal(e)
	}
	now := time.Now()
	b.pairingLost[pair.DeviceID] = pairingLostState{last: now, count: 1}
	devices, _ := b.Devices()
	if len(devices) != 1 || devices[0].PairingLost {
		t.Fatal("a single unauthenticated observation must not surface pairing loss")
	}
	b.notePairingLost(pair.DeviceID)
	devices, _ = b.Devices()
	if len(devices) != 1 || !devices[0].PairingLost || devices[0].ConnectionState != "pairing_lost" || !devices[0].Paired || !devices[0].ConfirmedOnline {
		t.Fatalf("pairing loss not surfaced safely: %+v", devices)
	}
	if _, ok := b.store.pair(pair.DeviceID); !ok {
		t.Fatal("discovery must never delete stored credentials")
	}
	b.clearPairingLost(pair.DeviceID)
	devices, _ = b.Devices()
	if devices[0].PairingLost || devices[0].ConnectionState != "disconnected" {
		t.Fatalf("pairing loss not cleared: %+v", devices)
	}
	if !pairingLostActive(pairingLostState{last: now, count: 2}, now) {
		t.Fatal("fresh repeated observation should be active")
	}
	if pairingLostActive(pairingLostState{last: now, count: 2}, now.Add(pairingLostWindow+time.Second)) {
		t.Fatal("stale observation must expire")
	}
	if pairingLostActive(pairingLostState{last: now, count: 1}, now) {
		t.Fatal("single observation must not be active")
	}
}

func TestDuplicateUnpairedAnnouncementCountsOncePerDiscovery(t *testing.T) {
	b := testBackend(t)
	pair := Pairing{DeviceID: wire.NewID(), PairingID: wire.NewID(), SerialKey: wire.Nonce()}
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	done := make(chan error, 1)
	go func() {
		buf := make([]byte, 1024)
		n, addr, err := conn.ReadFromUDP(buf)
		if err != nil {
			done <- err
			return
		}
		var request wire.Discovery
		if err = wire.Decode(buf[:n], &request, 1024); err != nil {
			done <- err
			return
		}
		response := wire.Discovery{ProtocolVersion: 1, Type: "monitor_announce", RequestID: request.RequestID, HostID: request.HostID, Nonce: request.Nonce, DeviceID: &pair.DeviceID, ResponseNonce: wire.Ptr(wire.Nonce()), DeviceName: wire.Ptr("board"), FirmwareVersion: wire.Ptr("1"), PairingOpen: wire.Ptr(true)}
		data, err := wire.Marshal(response)
		if err == nil {
			for i := 0; i < 3; i++ {
				_, err = conn.WriteToUDP(data, addr)
				if err != nil {
					break
				}
			}
		}
		done <- err
	}()
	if _, err = b.query(context.Background(), conn.LocalAddr().String(), &pair, 200*time.Millisecond, false); err != nil {
		t.Fatal(err)
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	if got := b.pairingLost[pair.DeviceID].count; got != 1 {
		t.Fatalf("one discovery round counted %d observations", got)
	}
}
