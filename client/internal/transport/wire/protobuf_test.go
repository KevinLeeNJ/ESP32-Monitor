package wire

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"math"
	"reflect"
	"testing"

	"esp32-monitor/client/internal/transport/wire/pb"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

// These bytes were produced by Python protobuf 6.33.6 / protoc 31.1 from the
// checked-in schema, independently of the Go adapter. They pin field numbers,
// signed integers, optional present zero/false and the outer oneof.
func TestPythonInteroperabilityVectors(t *testing.T) {
	cases := []struct {
		hex   string
		value any
		out   any
	}{
		{"08029a01170802120773657373696f6e182a22086163636570746564", Ack{1, "session", 42, "accepted"}, &Ack{}},
		{"0802c2013108021204686f73741a06646576696365220773657373696f6e282a3080e0f4ad0d4800520c08f6818ed8ae6810bf071800", Heartbeat{ProtocolVersion: 1, HostID: "host", DeviceID: "device", SessionID: "session", Seq: 42, SentAt: 1792972800, Revision: Ptr(uint32(0)), Time: &HeartbeatTime{1792972800123, -480, false}}, &Heartbeat{}},
	}
	for _, c := range cases {
		want, _ := hex.DecodeString(c.hex)
		got, err := Marshal(c.value)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("encode: %x %v", got, err)
		}
		if err = Decode(want, c.out, MaxMessage); err != nil {
			t.Fatalf("decode: %+v %v", c.out, err)
		}
		round, err := Marshal(c.out)
		if err != nil || !bytes.Equal(round, want) {
			t.Fatalf("roundtrip: %x %v", round, err)
		}
	}
}

func TestProtobufStrictValidationAndAtomicDecode(t *testing.T) {
	ack := Ack{1, "session", 42, "accepted"}
	good, err := Marshal(ack)
	if err != nil {
		t.Fatal(err)
	}
	malformed := [][]byte{nil, []byte(`{"protocol_version":1}`), good[:len(good)-1], append(append([]byte{}, good...), 8, 2), {8, 1, 154, 1, 0}, {8, 2, 154, 1, 0}}
	env := &pb.Envelope{}
	if err = proto.Unmarshal(good, env); err != nil {
		t.Fatal(err)
	}
	// Duplicate inner scalar; wrong wire type; conflicting envelope oneof.
	for _, suffix := range [][]byte{{24, 1}, {26, 1, 0}} {
		inner, _ := proto.Marshal(env.GetAck())
		inner = append(inner, suffix...)
		b := protowire.AppendBytes(protowire.AppendTag([]byte{8, 2}, 19, protowire.BytesType), inner)
		malformed = append(malformed, b)
	}
	// Overflowing uint32 must not truncate into a valid sequence.
	overflow := protowire.AppendVarint([]byte{8, 2, 18, 7, 's', 'e', 's', 's', 'i', 'o', 'n', 24}, uint64(math.MaxUint32)+42)
	overflow = append(overflow, 34, 8, 'a', 'c', 'c', 'e', 'p', 't', 'e', 'd')
	malformed = append(malformed, protowire.AppendBytes(protowire.AppendTag([]byte{8, 2}, 19, protowire.BytesType), overflow))
	other, _ := Marshal(Goodbye{1, "session", "done"})
	malformed = append(malformed, append(append([]byte{}, good...), other[2:]...))
	for i, b := range malformed {
		previous := Ack{1, "unchanged", 7, "previous"}
		out := previous
		if Validate(b, MaxMessage) == nil || Decode(b, &out, MaxMessage) == nil {
			t.Errorf("accepted malformed %d", i)
		}
		if out != previous {
			t.Fatal("partial update")
		}
	}
	var wrong Heartbeat
	if Decode(good, &wrong, MaxMessage) == nil {
		t.Fatal("accepted wrong message type")
	}
	if Validate(good, len(good)) != nil || Validate(good, len(good)-1) == nil {
		t.Fatal("binary limit")
	}
	unknown := protowire.AppendVarint(protowire.AppendTag(append([]byte{}, good...), 100, protowire.VarintType), 123)
	var out Ack
	if Decode(unknown, &out, MaxMessage) != nil || out != ack {
		t.Fatal("unknown field compatibility")
	}
	a, _ := Canonical(unknown)
	b, _ := Canonical(good)
	if !bytes.Equal(a, b) {
		t.Fatal("canonical unknown field")
	}
	for _, v := range []float64{math.NaN(), math.Inf(1)} {
		cfg := Config{Weather: WeatherConfig{Latitude: &v}}
		if _, err = Marshal(cfg); err == nil {
			t.Fatal("nonfinite accepted")
		}
	}
}

func TestTypedSnapshotRoundTripAndSize(t *testing.T) {
	id := "11111111-1111-4111-8111-111111111111"
	disabled := func() Section {
		return Section{Health: Health{State: "disabled", StaleAfterMS: 15000, ExpireAfterMS: 60000}}
	}
	s := Snapshot{ProtocolVersion: 1, HostID: id, DeviceID: id, SessionID: id, Seq: 42, SentAt: 1792972800, Epoch: id, Revision: 1, Sections: Sections{GitHub: disabled(), Codex: disabled(), Music: disabled(), Proxy: disabled()}}
	pd := ProxyData{Epoch: id, Revision: 1, Time: disabled(), Weather: disabled(), AQI: disabled(), Devices: disabled()}
	pd.Time.State = "healthy"
	pd.Time.ObservedAt = Ptr(int64(1792972800))
	pd.Time.LastSuccessAt = Ptr(int64(1792972800))
	pd.Time.AgeMS = Ptr(int64(0))
	pd.Time.Data = TimeData{1792972800123, 480, false, "system_clock"}
	s.Sections.Proxy.State = "healthy"
	s.Sections.Proxy.Data = pd
	s.Sections.GitHub.State = "healthy"
	s.Sections.GitHub.Data = map[string]any{"login": "monitor", "start_date": "2026-01-01", "days": 3, "levels": "024", "today_count": 0, "total_count": 9}
	s.Sections.Music.State = "healthy"
	s.Sections.Music.Data = map[string]any{"adapter": "mediaremote", "coverage": "system", "current": map[string]any{"state": "not_playing", "title": "天气", "artist": nil, "source": "player", "duration": 0, "position": nil, "position_observed_at": nil}}
	s.Sections.Codex.State = "healthy"
	q := disabled()
	q.State = "healthy"
	q.Data = map[string]any{"windows": []any{map[string]any{"bucket_id": "weekly", "window_minutes": 10080, "used_percent": 0, "resets_at": 1792972800}}, "truncated": false}
	tasks := disabled()
	tasks.State = "healthy"
	tasks.Data = map[string]any{"adapter": "events", "scope": "local", "state": "unknown", "active_count": nil, "selected_task": nil, "last_completed_at": nil}
	s.Sections.Codex.Data = map[string]any{"quota": q, "tasks": tasks}
	binary, err := Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var out Snapshot
	if err = Decode(binary, &out, MaxMessage); err != nil {
		t.Fatal(err)
	}
	old, _ := json.Marshal(s)
	round, _ := json.Marshal(out)
	var a, b any
	json.Unmarshal(old, &a)
	json.Unmarshal(round, &b)
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("snapshot changed\n%s\n%s", old, round)
	}
	if len(binary) >= len(old)*60/100 {
		t.Fatalf("insufficient reduction %d / %d", len(binary), len(old))
	}
	t.Logf("snapshot: JSON %d bytes -> Protobuf %d bytes (%.1f%% smaller)", len(old), len(binary), 100*(1-float64(len(binary))/float64(len(old))))
	heartbeat := Heartbeat{ProtocolVersion: 1, HostID: id, DeviceID: id, SessionID: id, Seq: 43, SentAt: 1792972800, LastSnapshotSeq: Ptr(uint32(42)), Epoch: &id, Revision: Ptr(uint32(1)), Time: &HeartbeatTime{1792972800123, 480, false}}
	h, _ := Marshal(heartbeat)
	j, _ := json.Marshal(heartbeat)
	t.Logf("heartbeat: JSON %d bytes -> Protobuf %d bytes (%.1f%% smaller)", len(j), len(h), 100*(1-float64(len(h))/float64(len(j))))
	if len(h) >= len(j) {
		t.Fatal("heartbeat did not shrink")
	}
}
