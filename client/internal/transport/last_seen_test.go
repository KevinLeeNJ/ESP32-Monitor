package transport

import (
	"path/filepath"
	"testing"
	"time"

	"esp32-monitor/client/internal/transport/wire"
)

func TestLastSeenPersistsAcrossReopen(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "vault")
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	p := Pairing{DeviceID: wire.NewID(), DeviceName: "board", PairingID: wire.NewID(), DeviceToken: wire.Nonce(), SerialKey: wire.Nonce(), ConsentAt: time.Now().Unix(), WarningVersion: 1, LANConsent: true}
	if err := store.put(p); err != nil {
		t.Fatal(err)
	}
	seen := time.Now().Add(-90 * time.Minute).Truncate(time.Second)
	if err := store.markLastSeen(p.DeviceID, seen, true); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	got, ok := store.pair(p.DeviceID)
	if !ok || got.LastSeen != seen.Unix() {
		t.Fatalf("last_seen = %d, want %d", got.LastSeen, seen.Unix())
	}

	backend := &Backend{store: store, candidates: map[string]candidate{}, sessions: map[string]*session{}, disabled: map[string]bool{}, serialSeen: map[string]time.Time{}, pairingLost: map[string]pairingLostState{}, configErrors: map[string]string{}}
	devices, _ := backend.Devices()
	if len(devices) != 1 || !devices[0].LastSeen.Equal(seen) {
		t.Fatalf("devices = %+v, want last_seen %v", devices, seen)
	}
}
