package transport

import (
	"testing"
	"time"
)

func TestCancelPairingRejectsWindowAndAbortsSerialWait(t *testing.T) {
	cancelled := false
	b := &Backend{pairing: &pairingServer{window: &pairWindow{
		deviceID:      "device",
		sourceIP:      "serial",
		expires:       time.Now().Add(time.Minute),
		hostConfirmed: true,
		cancel:        func() { cancelled = true },
	}}}
	if err := b.CancelPairing(); err != nil {
		t.Fatal(err)
	}
	if !cancelled {
		t.Fatal("serial pairing wait was not cancelled")
	}
	if status := b.PairingStatus(); status.State != "closed" {
		t.Fatalf("serial window not released: state=%q", status.State)
	}
	if b.pairing.window != nil {
		t.Fatal("serial pairing window must be released for retry")
	}
}
