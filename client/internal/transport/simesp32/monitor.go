package simesp32

import (
	"bytes"
	"context"
	"errors"
	"esp32-monitor/client/internal/transport"
	"esp32-monitor/client/internal/transport/wire"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Monitor struct {
	mu            sync.Mutex
	DeviceID      string
	Name          string
	PairingWindow bool
	Pair          *transport.Pairing
	Config        wire.Config
	Snapshots     chan wire.Snapshot
	Errors        chan error
	UDP           *net.UDPConn
	mqtt          *MQTT
	ctx           context.Context
	cancel        context.CancelFunc
	wg            sync.WaitGroup
}

func New(name, address string) (*Monitor, error) {
	a, e := net.ResolveUDPAddr("udp4", address)
	if e != nil {
		return nil, e
	}
	conn, e := net.ListenUDP("udp4", a)
	if e != nil {
		return nil, e
	}
	ctx, cancel := context.WithCancel(context.Background())
	m := &Monitor{DeviceID: wire.NewID(), Name: name, PairingWindow: true, Snapshots: make(chan wire.Snapshot, 16), Errors: make(chan error, 16), UDP: conn, ctx: ctx, cancel: cancel}
	m.Config = wire.Config{ProtocolVersion: 1, Epoch: wire.NewID(), Revision: 1, Weather: wire.WeatherConfig{LocationMode: "ip"}, Time: wire.TimeConfig{Enabled: true, Offset: 480}}
	return m, nil
}
func (m *Monitor) Address() string { return m.UDP.LocalAddr().String() }
func (m *Monitor) Start()          { m.wg.Add(1); go func() { defer m.wg.Done(); m.discover() }() }
func (m *Monitor) Close() {
	m.cancel()
	m.UDP.Close()
	m.mu.Lock()
	if m.mqtt != nil {
		m.mqtt.Close()
	}
	m.mu.Unlock()
	m.wg.Wait()
}
func (m *Monitor) report(e error) {
	if e != nil && m.ctx.Err() == nil {
		select {
		case m.Errors <- e:
		default:
		}
	}
}
func (m *Monitor) discover() {
	var contextMsg *wire.Discovery
	var source string
	var created time.Time
	buf := make([]byte, 1025)
	for {
		n, a, e := m.UDP.ReadFromUDP(buf)
		if e != nil {
			return
		}
		var d wire.Discovery
		if wire.Decode(buf[:n], &d, 1024) != nil || d.ProtocolVersion != 1 {
			continue
		}
		m.mu.Lock()
		pair := m.Pair
		pairingOpen := m.PairingWindow
		m.mu.Unlock()
		switch d.Type {
		case "monitor_discover":
			if pair == nil && !pairingOpen {
				continue
			}
			if pair != nil {
				if d.DeviceID == nil || *d.DeviceID != m.DeviceID || d.PairingID == nil || *d.PairingID != pair.PairingID || d.MAC == nil || *d.MAC != transport.DiscoveryMAC(d, pair.SerialKey) {
					continue
				}
			}
			r := d
			r.Type = "monitor_announce"
			r.DeviceID = &m.DeviceID
			r.ResponseNonce = wire.Ptr(wire.Nonce())
			r.DeviceName = &m.Name
			r.FirmwareVersion = wire.Ptr("sim-esp32-v1")
			r.PairingOpen = wire.Ptr(pair == nil && pairingOpen)
			if pair != nil {
				r.MAC = wire.Ptr(transport.DiscoveryMAC(r, pair.SerialKey))
			}
			data, _ := wire.Marshal(r)
			_, _ = m.UDP.WriteToUDP(data, a)
			contextMsg = &r
			source = a.String()
			created = time.Now()
		case "monitor_connect_offer":
			if contextMsg == nil || source != a.String() || time.Since(created) > 10*time.Second || d.RequestID != contextMsg.RequestID || d.HostID != contextMsg.HostID || d.Nonce != contextMsg.Nonce || val(d.ResponseNonce) != val(contextMsg.ResponseNonce) || val(d.DeviceID) != m.DeviceID {
				continue
			}
			contextMsg = nil
			if pair == nil {
				if d.HostPort == nil || !pairingOpen {
					continue
				}
				m.wg.Add(1)
				go func(d wire.Discovery, ip string) { defer m.wg.Done(); m.report(m.pairHTTP(d, ip)) }(d, a.IP.String())
			} else {
				if d.SessionID == nil || d.MQTTPort == nil || d.MAC == nil || *d.MAC != transport.DiscoveryMAC(d, pair.SerialKey) {
					continue
				}
				m.wg.Add(1)
				go func(d wire.Discovery, ip string) { defer m.wg.Done(); m.report(m.lan(d, ip, *pair)) }(d, a.IP.String())
			}
		}
	}
}
func val(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
func (m *Monitor) pairHTTP(d wire.Discovery, ip string) error {
	base := "http://" + net.JoinHostPort(ip, strconv.Itoa(*d.HostPort)) + "/companion/v1/pair"
	client := &http.Client{Timeout: 4 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	request := wire.PairRequest{ProtocolVersion: 1, Step: "begin", RequestID: wire.NewID(), DeviceID: m.DeviceID, DeviceName: m.Name, DeviceNonce: wire.Nonce()}
	call := func(req wire.PairRequest) (wire.PairResponse, error) {
		data, _ := wire.Marshal(req)
		r, e := http.NewRequestWithContext(m.ctx, "POST", base, bytes.NewReader(data))
		if e != nil {
			return wire.PairResponse{}, e
		}
		r.Header.Set("Content-Type", wire.ContentType)
		resp, e := client.Do(r)
		if e != nil {
			return wire.PairResponse{}, e
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 && resp.StatusCode != 201 && resp.StatusCode != 202 {
			return wire.PairResponse{}, errors.New("pair_http_failed")
		}
		var p wire.PairResponse
		data, e = io.ReadAll(io.LimitReader(resp.Body, 4097))
		if e == nil {
			e = wire.Decode(data, &p, 4096)
		}
		return p, e
	}
	r, e := call(request)
	if e != nil {
		return e
	}
	confirm := wire.PairRequest{ProtocolVersion: 1, Step: "confirm", RequestID: wire.NewID(), PairingID: r.PairingID, PairingTicket: r.PairingTicket, DeviceConfirmed: true}
	for {
		r, e = call(confirm)
		if e != nil {
			return e
		}
		if r.State == "approved" {
			if r.DeviceToken == nil || r.SerialKey == nil {
				return errors.New("pair_missing_credentials")
			}
			m.mu.Lock()
			m.Pair = &transport.Pairing{DeviceID: m.DeviceID, DeviceName: m.Name, PairingID: r.PairingID, DeviceToken: *r.DeviceToken, SerialKey: *r.SerialKey}
			m.PairingWindow = false
			m.mu.Unlock()
			return nil
		}
		if r.State == "rejected" {
			return errors.New("pair_rejected")
		}
		select {
		case <-m.ctx.Done():
			return nil
		case <-time.After(time.Second):
		}
	}
}
func (m *Monitor) lan(d wire.Discovery, ip string, pair transport.Pairing) error {
	sid := *d.SessionID
	pre := "emon/v1/" + d.HostID + "/" + m.DeviceID + "/" + sid
	client, e := DialMQTT(net.JoinHostPort(ip, strconv.Itoa(*d.MQTTPort)), m.DeviceID, sid, pair.DeviceToken, pre)
	if e != nil {
		return e
	}
	defer client.Close()
	m.mu.Lock()
	m.mqtt = client
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		if m.mqtt == client {
			m.mqtt = nil
		}
		m.mu.Unlock()
	}()
	if e = client.Subscribe(pre + "/down/#"); e != nil {
		return e
	}
	var seq, lastSeq uint32
	send := func(topic string, data any, req *string, qos byte) error {
		seq++
		body, err := wire.Marshal(data)
		if err != nil {
			return err
		}
		control := wire.Control{ProtocolVersion: 1, HostID: d.HostID, DeviceID: m.DeviceID, SessionID: sid, Seq: seq, SentAt: time.Now().Unix(), RequestID: req, Data: body}
		payload, err := wire.Marshal(control)
		if err != nil {
			return err
		}
		return client.Publish(pre+"/up/"+topic, payload, qos, false)
	}
	req := wire.NewID()
	if e = send("open", wire.Open{ProtocolVersion: 1, RequestID: req, PairingID: pair.PairingID, DeviceID: m.DeviceID, DeviceName: m.Name, FirmwareVersion: "sim-esp32-v1", Transport: "lan"}, &req, 1); e != nil {
		return e
	}
	for {
		p, e := client.Next()
		if e != nil {
			return e
		}
		if p.Retain {
			return errors.New("retained_business_message")
		}
		topic := strings.TrimPrefix(p.Topic, pre+"/down/")
		switch topic {
		case "session":
			m.mu.Lock()
			cfg := m.Config
			m.mu.Unlock()
			cfg.DeviceID = m.DeviceID
			cfg.SessionID = sid
			cfg.RequestID = wire.NewID()
			if e = send("proxy-config", cfg, &cfg.RequestID, 1); e != nil {
				return e
			}
		case "snapshot":
			var snap wire.Snapshot
			if wire.Decode(p.Payload, &snap, 16384) != nil || snap.SessionID != sid || snap.DeviceID != m.DeviceID || snap.Seq <= lastSeq {
				return wire.ErrMessage
			}
			lastSeq = snap.Seq
			select {
			case m.Snapshots <- snap:
			default:
			}
			if e = send("ack", wire.Ack{ProtocolVersion: 1, SessionID: sid, AckSeq: snap.Seq, Status: "accepted"}, nil, 0); e != nil {
				return e
			}
		case "heartbeat":
			var heartbeat wire.Heartbeat
			if wire.Decode(p.Payload, &heartbeat, 4096) != nil || heartbeat.Seq <= lastSeq {
				return wire.ErrMessage
			}
			lastSeq = heartbeat.Seq
			if e = send("ack", wire.Ack{ProtocolVersion: 1, SessionID: sid, AckSeq: heartbeat.Seq, Status: "accepted"}, nil, 0); e != nil {
				return e
			}
		case "error":
			return errors.New("companion_business_error")
		case "goodbye":
			return nil
		}
	}
}
