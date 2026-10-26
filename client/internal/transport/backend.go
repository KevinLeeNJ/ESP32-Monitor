// Package transport owns the Companion V1 backend. Its management methods are
// local-only APIs for the application composition root, never MQTT/HTTP routes.
package transport

import (
	"context"
	"errors"
	"esp32-monitor/client/internal/companion"
	"esp32-monitor/client/internal/transport/wire"
	mqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/listeners"
	"io"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

type Options struct {
	StoreDirectory     string
	HostName           string
	BindIP             string // An explicitly selected interface IPv4; never 0.0.0.0.
	MQTTAddress        string
	HTTPAddress        string
	DiscoveryTarget    string // Optional explicit UDP target, also useful for isolated tests.
	Providers          *companion.Service
	EnumerateSerial    func() ([]DiscoveredDevice, error) // Optional platform adapter injection.
	HTTPClient         HTTPDoer                           // Optional trusted embedding/testing override.
	AllowPrivateServer func(host string) bool             // Explicit local GUI authorization.
	AllowHTTPServer    func(baseURL string) bool
}
type Backend struct {
	networkIP       atomic.Value
	networkChangeMu sync.Mutex
	interfaceName   string

	mu                sync.Mutex
	discoveryMu       sync.Mutex
	opts              Options
	store             *Store
	broker            *mqtt.Server
	listener          *listeners.TCP
	pairing           *pairingServer
	ctx               context.Context
	cancel            context.CancelFunc
	wg                sync.WaitGroup
	closeOnce         sync.Once
	candidates        map[string]candidate
	sessions          map[string]*session
	lastDiscovery     time.Time
	discoveryOverflow bool
	pairSelection     string
	recovery          map[string]*recoveryState
	disabled          map[string]bool
	serialPorts       map[string]bool
	serialObserved    map[string]DiscoveredDevice
	globalRate        tokenBucket
}
type recoveryState struct {
	next    time.Time
	attempt int
}

func New(opts Options) (*Backend, error) {
	if !wire.Text(opts.HostName, 64) || opts.HostName == "" {
		return nil, wire.ErrMessage
	}
	if opts.BindIP == "" {
		return nil, errors.New("select_lan_interface")
	}
	ip := net.ParseIP(opts.BindIP)
	if ip == nil || ip.To4() == nil || ip.IsUnspecified() || ip.IsMulticast() {
		return nil, errors.New("select_lan_interface")
	}
	if opts.MQTTAddress == "" {
		opts.MQTTAddress = net.JoinHostPort(opts.BindIP, "18883")
	}
	if opts.HTTPAddress == "" {
		opts.HTTPAddress = net.JoinHostPort(opts.BindIP, "18080")
	}
	for _, a := range []string{opts.MQTTAddress, opts.HTTPAddress} {
		h, _, e := net.SplitHostPort(a)
		if e != nil || h != opts.BindIP {
			return nil, errors.New("listener_interface_mismatch")
		}
	}
	store, e := OpenStore(opts.StoreDirectory)
	if e != nil {
		return nil, e
	}
	ctx, cancel := context.WithCancel(context.Background())
	b := &Backend{opts: opts, store: store, ctx: ctx, cancel: cancel, candidates: map[string]candidate{}, sessions: map[string]*session{}, recovery: map[string]*recoveryState{}, disabled: map[string]bool{}, serialPorts: map[string]bool{}, serialObserved: map[string]DiscoveredDevice{}}
	b.networkIP.Store(opts.BindIP)
	b.interfaceName = interfaceForIP(opts.BindIP)
	b.globalRate = tokenBucket{tokens: 20, burst: 20, rate: 10, last: time.Now()}
	b.pairing = &pairingServer{service: b, attempts: map[string][]time.Time{}}
	cap := mqtt.NewDefaultServerCapabilities()
	cap.MaximumClients = 4
	cap.MaximumPacketSize = 18432
	cap.MaximumClientWritesPending = 4
	cap.MaximumInflight = 1
	cap.ReceiveMaximum = 1
	cap.MaximumQos = 1
	cap.RetainAvailable = 0
	cap.MinimumProtocolVersion = 4
	cap.MaximumSessionExpiryInterval = 0
	b.broker = mqtt.New(&mqtt.Options{InlineClient: true, Capabilities: cap, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if e = b.broker.AddHook(&brokerHook{backend: b}, nil); e != nil {
		cancel()
		store.Close()
		return nil, e
	}
	b.listener = listeners.NewTCP(listeners.Config{ID: "companion", Address: opts.MQTTAddress})
	if e = b.broker.AddListener(b.listener); e != nil {
		cancel()
		store.Close()
		return nil, errors.New("mqtt_listen_failed")
	}
	if e = b.broker.Serve(); e != nil {
		b.broker.Close()
		cancel()
		store.Close()
		return nil, e
	}
	b.wg.Add(1)
	go b.maintain()
	b.wg.Add(1)
	go b.watchInterface()
	return b, nil
}
func (b *Backend) HostID() string      { return b.store.HostID() }
func (b *Backend) MQTTAddress() string { b.mu.Lock(); defer b.mu.Unlock(); return b.listener.Address() }
func (b *Backend) mqttPort() int {
	_, p, _ := net.SplitHostPort(b.MQTTAddress())
	n, _ := strconv.Atoi(p)
	return n
}
func (b *Backend) Close() error {
	b.closeOnce.Do(func() {
		b.cancel()
		b.networkChangeMu.Lock()

		b.pairing.close()
		b.mu.Lock()
		for _, s := range b.sessions {
			s.cancel()
			if s.client != nil {
				s.client.Stop(errors.New("host_exit"))
			}
		}
		b.mu.Unlock()
		_ = b.broker.Close()
		b.networkChangeMu.Unlock()
		b.wg.Wait()
		_ = b.store.Close()
	})
	return nil
}
func (b *Backend) Revoke(device string) error {
	if !wire.UUID(device) {
		return wire.ErrMessage
	}
	b.mu.Lock()
	if s := b.sessions[device]; s != nil {
		s.cancel()
		if s.client != nil {
			s.client.Stop(errors.New("pairing_revoked"))
		}
		delete(b.sessions, device)
	}
	delete(b.candidates, device)
	delete(b.recovery, device)
	b.mu.Unlock()
	return b.store.remove(device)
}
func (b *Backend) SetDeviceEnabled(device string, enabled bool) error {
	if _, ok := b.store.pair(device); !ok {
		return errors.New("unauthorized")
	}
	b.mu.Lock()
	b.disabled[device] = !enabled
	if !enabled {
		if s := b.sessions[device]; s != nil {
			s.cancel()
			if s.client != nil {
				s.client.Stop(errors.New("device_disabled"))
			}
			delete(b.sessions, device)
		}
	}
	b.mu.Unlock()
	return nil
}
func (b *Backend) invite(p Pairing) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.ctx.Err() != nil || !p.LANConsent || b.disabled[p.DeviceID] {
		return "", errors.New("unauthorized")
	}
	if _, ok := b.sessions[p.DeviceID]; ok {
		return "", errors.New("session_conflict")
	}
	if len(b.sessions) >= 4 {
		return "", errors.New("capacity_exceeded")
	}
	ctx, cancel := context.WithCancel(b.ctx)
	s := &session{id: wire.NewID(), pair: p, transport: "lan", state: "authenticating", expires: time.Now().Add(30 * time.Second), ctx: ctx, cancel: cancel, in: make(chan incoming, 4), qosDone: make(chan struct{}, 1), rate: tokenBucket{tokens: 5, burst: 5, rate: 2, last: time.Now()}}
	b.sessions[p.DeviceID] = s
	return s.id, nil
}
func (b *Backend) maintain() {
	defer b.wg.Done()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-b.ctx.Done():
			return
		case <-ticker.C:
		}
		b.pairing.expire()
		now := time.Now()
		b.mu.Lock()
		for id, s := range b.sessions {
			if s.client == nil && s.transport == "lan" && now.After(s.expires) {
				s.cancel()
				delete(b.sessions, id)
			}
		}
		var jobs []Pairing
		for _, p := range b.store.pairs() {
			if !p.LANConsent || b.disabled[p.DeviceID] || b.sessions[p.DeviceID] != nil {
				continue
			}
			r := b.recovery[p.DeviceID]
			if r == nil {
				r = &recoveryState{}
				b.recovery[p.DeviceID] = r
			}
			if now.Before(r.next) {
				continue
			}
			seconds := min(60, 1<<min(r.attempt, 6))
			r.attempt++ // cryptographic jitter, no high-frequency probing
			jitter := float64(time.Now().UnixNano()%4001)/10000 + 0.8
			r.next = now.Add(time.Duration(float64(seconds) * jitter * float64(time.Second)))
			jobs = append(jobs, p)
		}
		b.mu.Unlock()
		for _, p := range jobs {
			if b.ctx.Err() != nil {
				return
			}
			b.recoverDevice(b.ctx, p)
		}
	}
}
func (b *Backend) release(s *session) {
	s.cancel()
	b.mu.Lock()
	if b.sessions[s.pair.DeviceID] == s {
		delete(b.sessions, s.pair.DeviceID)
		b.recovery[s.pair.DeviceID] = &recoveryState{next: time.Now().Add(10 * time.Second)}
	}
	b.mu.Unlock()
	if s.proxy != nil {
		s.proxy.stop()
		s.proxy = nil
	}
	s.config = nil
	s.transactions = nil
	s.lastMusic = nil
	s.serialKeys = nil
	if s.client != nil {
		s.client.Stop(errors.New("session_expired"))
	}
	if s.stream != nil {
		s.stream.Close()
	}
}

type tokenBucket struct {
	tokens, burst, rate float64
	last                time.Time
}

func (t *tokenBucket) take(now time.Time) bool {
	t.tokens = min(t.burst, t.tokens+now.Sub(t.last).Seconds()*t.rate)
	t.last = now
	if t.tokens < 1 {
		return false
	}
	t.tokens--
	return true
}

func (b *Backend) ConfirmLANConsent(device string, trusted bool) error {
	if !trusted {
		return errors.New("lan_consent_required")
	}
	return b.store.consent(device)
}
