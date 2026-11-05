package transport

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"esp32-monitor/client/internal/transport/wire"
)

func newTestPairing() Pairing {
	return Pairing{DeviceID: wire.NewID(), DeviceName: "board", PairingID: wire.NewID(), DeviceToken: wire.Nonce(), SerialKey: wire.Nonce(), ConsentAt: time.Now().Unix(), WarningVersion: 1, LANConsent: true}
}

func TestLastAddressPersistsAcrossReopen(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "vault")
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	p := newTestPairing()
	if err = store.put(p); err != nil {
		t.Fatal(err)
	}
	if err = store.rememberAddress(p.DeviceID, "192.168.110.169"); err != nil {
		t.Fatal(err)
	}
	// An unchanged value is a no-op and must not fail.
	if err = store.rememberAddress(p.DeviceID, "192.168.110.169"); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	got, ok := store.pair(p.DeviceID)
	if !ok || got.LastAddress != "192.168.110.169" {
		t.Fatalf("last_address = %q, want 192.168.110.169", got.LastAddress)
	}
}

func TestOpenStoreRejectsInvalidLastAddress(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "vault")
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	p := newTestPairing()
	if err = store.put(p); err != nil {
		t.Fatal(err)
	}
	host := store.HostID()
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "companion.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err = json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	pairs := doc["pairs"].(map[string]any)
	entry := pairs[p.DeviceID].(map[string]any)
	entry["last_address"] = "not-an-address"
	raw, err = json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}

	if _, err = OpenStore(dir); err == nil {
		t.Fatalf("OpenStore accepted an invalid last_address for host %s", host)
	}
}
