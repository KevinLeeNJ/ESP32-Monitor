package transport

import (
	"bytes"
	"testing"
	"time"
)

func TestSerialTimeFrames(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	for _, tc := range []struct {
		typ       byte
		payload   string
		direction byte
	}{
		{SerialTimeRequest, "?\n", 2},
		{SerialTimeResponse, "42 1792972800123\n", 1},
	} {
		raw, err := EncodeSerial(tc.typ, 42, []byte(tc.payload), key, tc.direction)
		if err != nil {
			t.Fatal(err)
		}
		var decoder SerialDecoder
		var frames []SerialFrame
		for _, b := range raw {
			frames = append(frames, decoder.Feed([]byte{b}, time.Now())...)
		}
		if len(frames) != 1 {
			t.Fatal("fragmented time frame missing")
		}
		payload, err := VerifySerial(frames[0], key, tc.direction)
		if err != nil || string(payload) != tc.payload {
			t.Fatal("time payload changed", err)
		}
		if _, err := VerifySerial(frames[0], nil, tc.direction); err == nil {
			t.Fatal("unauthenticated time accepted")
		}
		frames[0].Payload[0] ^= 1
		if _, err := VerifySerial(frames[0], key, tc.direction); err == nil {
			t.Fatal("tampered time accepted")
		}
	}
	for _, payload := range []string{"", "0 1792972800123\n", "4294967296 1792972800123\n", "42 0000000000000\n", "42 4102444800000\n", "42 1792972800123", "42 1792972800123\nextra", "+1 1792972800123\n"} {
		if _, err := EncodeSerial(SerialTimeResponse, 1, []byte(payload), key, 1); err == nil {
			t.Fatalf("invalid time accepted: %q", payload)
		}
	}
	if _, err := EncodeSerial(SerialTimeRequest, 1, []byte("?\n"), nil, 2); err == nil {
		t.Fatal("unsigned request accepted")
	}
}
