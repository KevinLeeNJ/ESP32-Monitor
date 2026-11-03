package transport

import (
	"context"
	"testing"
	"time"
)

func TestSerialHandoffKeepsLANUntilConnected(t *testing.T) {
	lanCtx, lanCancel := context.WithCancel(context.Background())
	defer lanCancel()
	serialCtx, serialCancel := context.WithCancel(context.Background())
	defer serialCancel()
	lan := &session{transport: "lan", ctx: lanCtx, cancel: lanCancel}
	serial := &session{transport: "serial", ctx: serialCtx, cancel: serialCancel}
	lan.pair.DeviceID = "device"
	serial.pair.DeviceID = "device"
	b := &Backend{sessions: map[string]*session{"device": lan}, disabled: map[string]bool{}, serialSeen: map[string]time.Time{}}

	serial.setState(b, "waiting_snapshot")
	if b.sessions["device"] != lan || lanCtx.Err() != nil {
		t.Fatal("an unproven serial session interrupted LAN")
	}
	serial.setState(b, "connected")
	if b.sessions["device"] != serial || lanCtx.Err() == nil {
		t.Fatal("proven serial session did not replace LAN")
	}
}

func TestSerialHandoffRejectsDisabledDevice(t *testing.T) {
	lanCtx, lanCancel := context.WithCancel(context.Background())
	defer lanCancel()
	serialCtx, serialCancel := context.WithCancel(context.Background())
	defer serialCancel()
	lan := &session{transport: "lan", ctx: lanCtx, cancel: lanCancel}
	serial := &session{transport: "serial", ctx: serialCtx, cancel: serialCancel}
	lan.pair.DeviceID = "device"
	serial.pair.DeviceID = "device"
	b := &Backend{sessions: map[string]*session{"device": lan}, disabled: map[string]bool{"device": true}, serialSeen: map[string]time.Time{}}

	serial.setState(b, "connected")
	if b.sessions["device"] != lan || lanCtx.Err() != nil || serialCtx.Err() == nil {
		t.Fatal("disabled device accepted pending serial session")
	}
}
