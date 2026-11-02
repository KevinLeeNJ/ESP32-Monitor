package transport

import (
	"esp32-monitor/client/internal/transport/wire"
	"path/filepath"
	"testing"
	"time"
)

func TestAuthenticatedDiscoveryRevocationRemovesCredential(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "vault"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	pair := Pairing{
		DeviceID: wire.NewID(), DeviceName: "Monitor", PairingID: wire.NewID(),
		DeviceToken: wire.Nonce(), SerialKey: wire.Nonce(),
		ConsentAt: time.Now().Unix(), WarningVersion: 1, LANConsent: true,
	}
	if err := store.put(pair); err != nil {
		t.Fatal(err)
	}
	b := &Backend{store: store, sessions: map[string]*session{}, candidates: map[string]candidate{},
		recovery: map[string]*recoveryState{}, pairingLost: map[string]pairingLostState{}}
	open := true
	announce := func() wire.Discovery {
		return wire.Discovery{
			ProtocolVersion: 1, Type: "monitor_announce", RequestID: wire.NewID(), HostID: store.HostID(),
			DeviceID: &pair.DeviceID, PairingID: &pair.PairingID,
			Nonce: wire.Nonce(), ResponseNonce: wire.Ptr(wire.Nonce()),
			DeviceName: wire.Ptr("ESP32 Monitor"), FirmwareVersion: wire.Ptr("1.0.0"),
			PairingOpen: &open,
		}
	}

	bad := announce()
	bad.MAC = wire.Ptr(wire.Nonce()) // wrong length and value
	if b.discoveryRevokes(bad, &pair) {
		t.Fatal("forged revocation accepted")
	}
	closed := announce()
	closed.PairingOpen = wire.Ptr(false)
	closed.MAC = wire.Ptr(DiscoveryMAC(closed, pair.SerialKey))
	if b.discoveryRevokes(closed, &pair) {
		t.Fatal("paired announcement treated as revocation")
	}
	other := announce()
	other.PairingID = wire.Ptr(wire.NewID())
	other.MAC = wire.Ptr(DiscoveryMAC(other, pair.SerialKey))
	if b.discoveryRevokes(other, &pair) {
		t.Fatal("revocation for another pairing accepted")
	}
	if _, ok := store.pair(pair.DeviceID); !ok {
		t.Fatal("credential removed by unauthenticated announcement")
	}

	valid := announce()
	valid.MAC = wire.Ptr(DiscoveryMAC(valid, pair.SerialKey))
	if !b.discoveryRevokes(valid, &pair) {
		t.Fatal("authenticated revocation rejected")
	}
	if _, ok := store.pair(pair.DeviceID); ok {
		t.Fatal("revoked credential retained")
	}
}
