// Package simesp32 is a network-level Monitor simulator for backend acceptance.
// Its MQTT client encodes MQTT 3.1.1 packets directly, independently of the Broker.
package simesp32

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync"
	"time"
)

type MQTT struct {
	conn net.Conn
	r    *bufio.Reader
	mu   sync.Mutex
	id   uint16
}
type Publication struct {
	Topic   string
	Payload []byte
	Retain  bool
	QoS     byte
}

func field(b []byte, s []byte) []byte {
	b = binary.BigEndian.AppendUint16(b, uint16(len(s)))
	return append(b, s...)
}
func (m *MQTT) write(header byte, payload []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	b := []byte{header}
	n := len(payload)
	for {
		v := byte(n % 128)
		n /= 128
		if n > 0 {
			v |= 128
		}
		b = append(b, v)
		if n == 0 {
			break
		}
	}
	b = append(b, payload...)
	m.conn.SetWriteDeadline(time.Now().Add(4 * time.Second))
	for len(b) > 0 {
		n, e := m.conn.Write(b)
		if e != nil {
			return e
		}
		b = b[n:]
	}
	return nil
}
func (m *MQTT) read() (byte, []byte, error) {
	h, e := m.r.ReadByte()
	if e != nil {
		return 0, nil, e
	}
	n, mult := 0, 1
	for i := 0; i < 4; i++ {
		b, e := m.r.ReadByte()
		if e != nil {
			return 0, nil, e
		}
		n += int(b&127) * mult
		if n > 18432 {
			return 0, nil, errors.New("oversized_mqtt")
		}
		if b&128 == 0 {
			p := make([]byte, n)
			_, e = io.ReadFull(m.r, p)
			return h, p, e
		}
		mult *= 128
	}
	return 0, nil, errors.New("malformed_mqtt")
}
func DialMQTT(address, device, session, token, prefix string) (*MQTT, error) {
	conn, e := net.DialTimeout("tcp4", address, 4*time.Second)
	if e != nil {
		return nil, e
	}
	m := &MQTT{conn: conn, r: bufio.NewReader(conn)}
	b := field(nil, []byte("MQTT"))
	b = append(b, 4, 0xce, 0, 60)
	b = field(b, []byte("emon-"+device))
	b = field(b, []byte(prefix+"/up/presence"))
	will := []byte(`{"protocol_version":1,"host_id":"` + hostFromPrefix(prefix) + `","device_id":"` + device + `","session_id":"` + session + `","state":"offline","reason":"transport_lost"}`)
	b = field(b, will)
	b = field(b, []byte(device+":"+session))
	b = field(b, []byte(token))
	if e = m.write(0x10, b); e != nil {
		conn.Close()
		return nil, e
	}
	conn.SetReadDeadline(time.Now().Add(4 * time.Second))
	h, p, e := m.read()
	if e != nil || h != 0x20 || !bytes.Equal(p, []byte{0, 0}) {
		conn.Close()
		return nil, errors.New("mqtt_connect_refused")
	}
	return m, nil
}
func hostFromPrefix(s string) string {
	if len(s) < 44 {
		return ""
	}
	return s[8:44]
}
func (m *MQTT) Subscribe(topic string) error {
	m.id++
	p := binary.BigEndian.AppendUint16(nil, m.id)
	p = field(p, []byte(topic))
	p = append(p, 1)
	if e := m.write(0x82, p); e != nil {
		return e
	}
	h, b, e := m.read()
	if e != nil || h != 0x90 || len(b) < 3 || b[2] > 1 {
		return errors.New("mqtt_subscribe_refused")
	}
	return nil
}
func (m *MQTT) Publish(topic string, payload []byte, qos byte, retain bool) error {
	p := field(nil, []byte(topic))
	if qos == 1 {
		m.id++
		p = binary.BigEndian.AppendUint16(p, m.id)
	}
	p = append(p, payload...)
	h := byte(0x30) | qos<<1
	if retain {
		h |= 1
	}
	return m.write(h, p)
}
func (m *MQTT) Next() (Publication, error) {
	for {
		m.conn.SetReadDeadline(time.Now().Add(95 * time.Second))
		h, p, e := m.read()
		if e != nil {
			return Publication{}, e
		}
		if h>>4 != 3 {
			continue
		}
		if len(p) < 2 {
			return Publication{}, io.ErrUnexpectedEOF
		}
		n := int(binary.BigEndian.Uint16(p))
		if n+2 > len(p) {
			return Publication{}, io.ErrUnexpectedEOF
		}
		topic := string(p[2 : 2+n])
		p = p[2+n:]
		qos := (h >> 1) & 3
		if qos == 1 {
			if len(p) < 2 {
				return Publication{}, io.ErrUnexpectedEOF
			}
			if e = m.write(0x40, p[:2]); e != nil {
				return Publication{}, e
			}
			p = p[2:]
		}
		return Publication{topic, append([]byte(nil), p...), h&1 != 0, qos}, nil
	}
}
func (m *MQTT) Close() error { return m.conn.Close() }
