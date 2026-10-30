package transport

import (
	"crypto/subtle"
	"esp32-monitor/client/internal/transport/wire"
	mqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/packets"
	"strings"
	"time"
)

type brokerHook struct {
	mqtt.HookBase
	backend *Backend
}

func (h *brokerHook) ID() string { return "companion-v1" }
func (h *brokerHook) Provides(e byte) bool {
	switch e {
	case mqtt.OnConnectAuthenticate, mqtt.OnACLCheck, mqtt.OnPacketRead, mqtt.OnPublish, mqtt.OnSubscribed, mqtt.OnDisconnect, mqtt.OnPacketProcessed, mqtt.OnQosPublish:
		return true
	}
	return false
}
func (h *brokerHook) OnConnectAuthenticate(cl *mqtt.Client, p packets.Packet) bool {
	b := h.backend
	b.mu.Lock()
	defer b.mu.Unlock()
	parts := strings.Split(string(p.Connect.Username), ":")
	if len(parts) != 2 {
		return false
	}
	s := b.sessions[parts[0]]
	if s == nil || s.transport != "lan" || s.id != parts[1] || s.client != nil || time.Now().After(s.expires) || p.ProtocolVersion != 4 || !p.Connect.Clean || p.Connect.Keepalive != 60 || cl.ID != "emon-"+s.pair.DeviceID || subtle.ConstantTimeCompare(p.Connect.Password, []byte(s.pair.DeviceToken)) != 1 {
		return false
	}
	pre := prefix(b.store.HostID(), s.pair.DeviceID, s.id)
	var will wire.Presence
	if !p.Connect.WillFlag || p.Connect.WillTopic != pre+"/up/presence" || p.Connect.WillQos != 1 || p.Connect.WillRetain || wire.Decode(p.Connect.WillPayload, &will, 1024) != nil || will.ProtocolVersion != 1 || will.HostID != b.store.HostID() || will.DeviceID != s.pair.DeviceID || will.SessionID != s.id || will.State != "offline" || will.Reason != "transport_lost" {
		return false
	}
	s.client = cl
	s.expires = time.Now().Add(15 * time.Second)
	b.wg.Add(1)
	go func() { defer b.wg.Done(); s.run(b) }()
	return true
}
func (h *brokerHook) find(cl *mqtt.Client) *session {
	h.backend.mu.Lock()
	defer h.backend.mu.Unlock()
	for _, s := range h.backend.sessions {
		if s.client == cl {
			return s
		}
	}
	return nil
}

var upTopics = map[string]byte{"open": 1, "proxy-config": 1, "ack": 0, "resync": 1, "goodbye": 1, "error": 1, "presence": 1}
var downTopics = map[string]bool{"session": true, "config-ack": true, "snapshot": true, "heartbeat": true, "goodbye": true, "error": true}

func (h *brokerHook) OnACLCheck(cl *mqtt.Client, topic string, write bool) bool {
	if cl.Net.Inline {
		return true
	}
	s := h.find(cl)
	if s == nil || len(topic) > 256 {
		return false
	}
	pre := prefix(h.backend.store.HostID(), s.pair.DeviceID, s.id)
	if write {
		_, ok := upTopics[strings.TrimPrefix(topic, pre+"/up/")]
		return strings.HasPrefix(topic, pre+"/up/") && ok
	}
	if topic == pre+"/down/#" {
		return true
	}
	return strings.HasPrefix(topic, pre+"/down/") && downTopics[strings.TrimPrefix(topic, pre+"/down/")]
}
func (h *brokerHook) OnPacketRead(cl *mqtt.Client, p packets.Packet) (packets.Packet, error) {
	if cl.Net.Inline || p.FixedHeader.Type == packets.Connect {
		return p, nil
	}
	if p.FixedHeader.Type == packets.Publish && (p.FixedHeader.Retain || p.FixedHeader.Qos > 1 || len(p.Payload) > 16384 || !h.OnACLCheck(cl, p.TopicName, true)) {
		return p, packets.ErrRejectPacket
	}
	return p, nil
}
func (h *brokerHook) OnSubscribed(cl *mqtt.Client, p packets.Packet, codes []byte) {
	s := h.find(cl)
	if s == nil {
		return
	}
	for i, c := range codes {
		if c > 1 {
			cl.Stop(packets.ErrNotAuthorized)
			return
		}
		if i < len(p.Filters) {
			filter := p.Filters[i].Filter
			h.backend.mu.Lock()
			if s.subscriptions == nil {
				s.subscriptions = map[string]bool{}
			}
			s.subscriptions[filter] = true
			h.backend.mu.Unlock()
		}
	}
}
func (h *brokerHook) OnPublish(cl *mqtt.Client, p packets.Packet) (packets.Packet, error) {
	if cl.Net.Inline {
		return p, nil
	}
	s := h.find(cl)
	if s == nil {
		return p, packets.ErrRejectPacket
	}
	topic := strings.TrimPrefix(p.TopicName, prefix(h.backend.store.HostID(), s.pair.DeviceID, s.id)+"/up/")
	q, ok := upTopics[topic]
	if !ok || q != p.FixedHeader.Qos || wire.Validate(p.Payload, 16384) != nil {
		return p, packets.ErrRejectPacket
	}
	return p, nil
}

// Enqueue only after the Broker has finished the inbound QoS transaction.
// This prevents application responses racing the Broker's incoming PUBACK.
func (h *brokerHook) OnPacketProcessed(cl *mqtt.Client, p packets.Packet, err error) {
	if err != nil || cl.Net.Inline {
		return
	}
	s := h.find(cl)
	if s == nil {
		return
	}
	if p.FixedHeader.Type == packets.Puback {
		h.backend.mu.Lock()
		matches := s.pendingMQTT == p.PacketID && s.pendingMQTT != 0
		if matches {
			s.pendingMQTT = 0
		}
		h.backend.mu.Unlock()
		if matches {
			select {
			case s.qosDone <- struct{}{}:
			default:
			}
		}
		return
	}
	if p.FixedHeader.Type != packets.Publish {
		return
	}
	topic := strings.TrimPrefix(p.TopicName, prefix(h.backend.HostID(), s.pair.DeviceID, s.id)+"/up/")
	q, ok := upTopics[topic]
	if !ok || q != p.FixedHeader.Qos || p.FixedHeader.Retain || wire.Validate(p.Payload, 16384) != nil || !h.OnACLCheck(cl, p.TopicName, true) {
		return
	}
	payload := append([]byte(nil), p.Payload...)
	select {
	case s.in <- incoming{topic: topic, data: payload}:
	default:
		cl.Stop(packets.ErrQuotaExceeded)
	}
}
func (h *brokerHook) OnQosPublish(cl *mqtt.Client, p packets.Packet, _ int64, _ int) {
	if p.FixedHeader.Type != packets.Publish {
		return
	}
	if s := h.find(cl); s != nil {
		h.backend.mu.Lock()
		s.pendingMQTT = p.PacketID
		h.backend.mu.Unlock()
	}
}

func (h *brokerHook) OnDisconnect(cl *mqtt.Client, _ error, _ bool) {
	if s := h.find(cl); s != nil {
		s.cancel()
	}
}
func (b *Backend) sendMQTT(s *session, topic string, data any, qos byte) error {
	data = stampWallClock(data, time.Now())
	payload, e := wire.Marshal(data)
	if e != nil {
		return wire.ErrMessage
	}
	if len(payload) > 16384 {
		return errLarge
	}
	if e = wire.Validate(payload, 16384); e != nil {
		return e
	}
	if e = b.broker.Publish(prefix(b.store.HostID(), s.pair.DeviceID, s.id)+"/down/"+topic, payload, false, qos); e != nil {
		return e
	}
	if qos == 1 {
		timer := time.NewTimer(4 * time.Second)
		defer timer.Stop()
		select {
		case <-s.ctx.Done():
			return s.ctx.Err()
		case <-s.qosDone:
		case <-timer.C:
			return errTimeout
		}
	}
	return nil
}
