package transport

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"esp32-monitor/client/internal/transport/wire"
	"hash/crc32"
	"time"
)

const (
	SerialHello     byte = 1
	SerialAuth      byte = 2
	SerialPair      byte = 3
	SerialConfig    byte = 0x10
	SerialConfigAck byte = 0x11
	SerialSnapshot  byte = 0x20
	SerialHeartbeat byte = 0x21
	SerialAck       byte = 0x22
	SerialGoodbye   byte = 0x7e
	SerialError     byte = 0x7f
)

type SerialFrame struct {
	Type    byte
	Seq     uint32
	Payload []byte
	Header  []byte
}

func serialType(t byte) bool {
	switch t {
	case 1, 2, 3, 0x10, 0x11, 0x20, 0x21, 0x22, 0x7e, 0x7f:
		return true
	}
	return false
}

// SerialDecoder is bounded to one maximum frame. Feed accepts logs and fragments;
// invalid candidates are rescanned from magic+1, preserving a following frame.
type SerialDecoder struct {
	buf   []byte
	since time.Time
}

func (d *SerialDecoder) Feed(data []byte, now time.Time) []SerialFrame {
	out := []SerialFrame{}
	for _, v := range data {
		d.buf = append(d.buf, v)
		out = append(out, d.parse(now)...)
	}
	return out
}
func (d *SerialDecoder) Tick(now time.Time) []SerialFrame { return d.parse(now) }
func (d *SerialDecoder) parse(now time.Time) []SerialFrame {
	out := []SerialFrame{}
	for {
		idx := bytes.Index(d.buf, []byte("EMON"))
		if idx < 0 {
			if len(d.buf) > 3 {
				d.buf = d.buf[len(d.buf)-3:]
			}
			d.since = time.Time{}
			break
		}
		if idx > 0 {
			d.buf = d.buf[idx:]
			d.since = time.Time{}
		}
		if d.since.IsZero() {
			d.since = now
		}
		if now.Sub(d.since) > 2*time.Second {
			d.buf = d.buf[1:]
			d.since = time.Time{}
			continue
		}
		if len(d.buf) < 12 {
			break
		}
		n := int(binary.LittleEndian.Uint16(d.buf[6:8]))
		if d.buf[4] != wire.WireVersion || !serialType(d.buf[5]) || n < 2 || n > 16368 || binary.LittleEndian.Uint32(d.buf[8:12]) == 0 {
			d.buf = d.buf[1:]
			d.since = time.Time{}
			continue
		}
		total := 16 + n
		if len(d.buf) < total {
			break
		}
		if crc32.ChecksumIEEE(d.buf[4:12+n]) != binary.LittleEndian.Uint32(d.buf[12+n:total]) {
			d.buf = d.buf[1:]
			d.since = time.Time{}
			continue
		}
		out = append(out, SerialFrame{d.buf[5], binary.LittleEndian.Uint32(d.buf[8:12]), append([]byte(nil), d.buf[12:12+n]...), append([]byte(nil), d.buf[:12]...)})
		d.buf = d.buf[total:]
		d.since = time.Time{}
	}
	return out
}
func EncodeSerial(t byte, seq uint32, data []byte, key []byte, direction byte) ([]byte, error) {
	if !serialType(t) || seq == 0 || wire.Validate(data, 16336) != nil {
		return nil, wire.ErrMessage
	}
	n := len(data)
	if len(key) > 0 {
		n += 32
	} else if n > 2048 {
		return nil, errLarge
	}
	if n > 16368 {
		return nil, errLarge
	}
	b := make([]byte, 12, 16+n)
	copy(b, "EMON")
	b[4] = wire.WireVersion
	b[5] = t
	binary.LittleEndian.PutUint16(b[6:8], uint16(n))
	binary.LittleEndian.PutUint32(b[8:12], seq)
	b = append(b, data...)
	if len(key) > 0 {
		h := hmac.New(sha256.New, key)
		h.Write([]byte{direction})
		h.Write(b)
		b = append(b, h.Sum(nil)...)
	}
	b = binary.LittleEndian.AppendUint32(b, crc32.ChecksumIEEE(b[4:]))
	return b, nil
}
func VerifySerial(f SerialFrame, key []byte, direction byte) ([]byte, error) {
	if len(f.Header) != 12 {
		return nil, wire.ErrMessage
	}
	p := f.Payload
	if len(key) > 0 {
		if len(p) < 34 {
			return nil, wire.ErrMessage
		}
		data := p[:len(p)-32]
		h := hmac.New(sha256.New, key)
		h.Write([]byte{direction})
		h.Write(f.Header)
		h.Write(data)
		if !hmac.Equal(h.Sum(nil), p[len(p)-32:]) {
			return nil, errors.New("unauthorized")
		}
		p = data
	} else if len(p) > 2048 {
		return nil, errLarge
	}
	if wire.Validate(p, 16336) != nil {
		return nil, wire.ErrMessage
	}
	return p, nil
}

type SerialKeys struct {
	Frame, Config          []byte
	DeviceProof, HostProof string
}

func DeriveSerialKeys(host, device, pairing, session, hn, dn, key string) (SerialKeys, error) {
	var k SerialKeys
	h, e := wire.RawNonce(hn)
	if e != nil {
		return k, e
	}
	d, e := wire.RawNonce(dn)
	if e != nil {
		return k, e
	}
	raw, e := wire.RawNonce(key)
	if e != nil {
		return k, e
	}
	t := []byte{}
	for _, s := range []string{"ESP32MON-SERIAL-V1", host, device, pairing, session} {
		t = append(t, lp(s)...)
	}
	t = append(t, h...)
	t = append(t, d...)
	proof := func(direction string) string {
		m := hmac.New(sha256.New, raw)
		m.Write([]byte(direction))
		m.Write(t)
		return hex.EncodeToString(m.Sum(nil))
	}
	salt := sha256.Sum256(t)
	k.Frame, _ = hkdf.Key(sha256.New, raw, salt[:], "frame", 32)
	k.Config, _ = hkdf.Key(sha256.New, raw, salt[:], "proxy-config", 32)
	k.DeviceProof = proof("device")
	k.HostProof = proof("host")
	return k, nil
}

type EncryptedConfig = wire.EncryptedConfig

func configGCM(key []byte) (cipher.AEAD, error) {
	block, e := aes.NewCipher(key)
	if e != nil {
		return nil, e
	}
	return cipher.NewGCM(block)
}
func configAAD(session, req string) []byte {
	return append(append(lp("ESP32MON-CONFIG-V1"), lp(session)...), lp(req)...)
}
func EncryptSerialConfig(key []byte, seq uint32, session, request string, plain []byte) (EncryptedConfig, error) {
	out := EncryptedConfig{ProtocolVersion: 1, SessionID: session, RequestID: request, Algorithm: "aes-256-gcm"}
	if wire.Validate(plain, 4096) != nil || seq == 0 {
		return out, wire.ErrMessage
	}
	g, e := configGCM(key)
	if e != nil {
		return out, e
	}
	nonce := make([]byte, 12)
	binary.BigEndian.PutUint32(nonce[8:], seq)
	sealed := g.Seal(nil, nonce, plain, configAAD(session, request))
	out.Nonce = nonce
	out.Ciphertext = sealed[:len(sealed)-16]
	out.Tag = sealed[len(sealed)-16:]
	return out, nil
}
func DecryptSerialConfig(key []byte, seq uint32, session string, wrapped []byte) ([]byte, error) {
	var e EncryptedConfig
	if wire.Decode(wrapped, &e, 8192) != nil || e.ProtocolVersion != 1 || e.SessionID != session || !wire.UUID(e.RequestID) || e.Algorithm != "aes-256-gcm" || seq == 0 {
		return nil, wire.ErrMessage
	}
	nonce, ciphertext, tag := e.Nonce, e.Ciphertext, e.Tag
	expected := make([]byte, 12)
	binary.BigEndian.PutUint32(expected[8:], seq)
	if len(tag) != 16 || !bytes.Equal(nonce, expected) || len(ciphertext) > 4096 {
		return nil, wire.ErrMessage
	}
	g, err := configGCM(key)
	if err != nil {
		return nil, wire.ErrMessage
	}
	plain, err := g.Open(nil, nonce, append(ciphertext, tag...), configAAD(session, e.RequestID))
	if err != nil {
		return nil, errors.New("unauthorized")
	}
	var cfg wire.Config
	if wire.Decode(plain, &cfg, 4096) != nil || cfg.RequestID != e.RequestID || cfg.SessionID != session {
		return nil, wire.ErrMessage
	}
	return plain, nil
}
