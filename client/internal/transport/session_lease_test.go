package transport

import (
	"context"
	"esp32-monitor/client/internal/transport/wire"
	"net"
	"testing"
	"time"
)

// A heartbeat without a changed lease must still name every healthy source.
// Otherwise the monitor falls back to the old snapshot age and expires the
// source until the next observation-driven lease arrives.
func TestHeartbeatRepeatsUnchangedSourceLeases(t *testing.T) {
	b := testBackend(t)
	host, device := net.Pipe()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	keys, err := DeriveSerialKeys(b.HostID(), wire.NewID(), wire.NewID(), wire.NewID(), wire.Nonce(), wire.Nonce(), wire.Nonce())
	if err != nil {
		t.Fatal(err)
	}
	deviceID, sessionID := wire.NewID(), wire.NewID()
	s := &session{
		id:           sessionID,
		pair:         Pairing{DeviceID: deviceID},
		transport:    "serial",
		ctx:          ctx,
		stream:       host,
		serialKeys:   &keys,
		config:       &wire.Config{Epoch: wire.NewID(), Revision: 1},
		outputSeq:    4,
		lastSnapshot: 9,
		sentProof:    map[uint32]bool{},
		rate:         tokenBucket{tokens: 5, burst: 5, rate: 2, last: time.Now()},
	}
	reader := readSerial(ctx, device)
	defer func() {
		cancel()
		host.Close()
		device.Close()
		<-reader.done
	}()

	observed := time.Now().Unix()
	age := int64(1000)
	section := health("healthy", 15000, 15000)
	section.ObservedAt = &observed
	section.LastSuccessAt = &observed
	section.AgeMS = &age
	sections := wire.Sections{Music: section}

	beat := func() wire.Heartbeat {
		t.Helper()
		if e := s.heartbeat(b, sections); e != nil {
			t.Fatal(e)
		}
		f, e := reader.next(ctx, 2*time.Second)
		if e != nil {
			t.Fatal(e)
		}
		plain, e := VerifySerial(f, keys.Frame, 1)
		if e != nil {
			t.Fatal(e)
		}
		var hb wire.Heartbeat
		if e = wire.Decode(plain, &hb, 4096); e != nil {
			t.Fatal(e)
		}
		return hb
	}

	first := beat()
	if len(first.SourceUpdates) != 1 || first.SourceUpdates[0].Path != "music" ||
		first.SourceUpdates[0].SnapshotSeq != 9 || first.SourceUpdates[0].AgeMS != 1000 {
		t.Fatalf("first heartbeat missing current lease: %+v", first.SourceUpdates)
	}
	age = 3000
	second := beat()
	if len(second.SourceUpdates) != 1 || second.SourceUpdates[0].Path != "music" ||
		second.SourceUpdates[0].LastSuccessAt != observed || second.SourceUpdates[0].AgeMS != 3000 {
		t.Fatalf("unchanged lease dropped from heartbeat: %+v", second.SourceUpdates)
	}
}
