package transport

import (
	"net"
	"path/filepath"
	"testing"
)

func TestPairingReportsOccupiedPort(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	b, err := New(Options{StoreDirectory: filepath.Join(t.TempDir(), "vault"), HostName: "test", BindIP: "127.0.0.1", MQTTAddress: "127.0.0.1:0", HTTPAddress: listener.Addr().String()})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if err := b.pairing.open("device", "127.0.0.1"); err == nil || err.Error() != "pairing_port_in_use" {
		t.Fatalf("expected pairing_port_in_use, got %v", err)
	}
}
