package transport

import (
	"errors"
	"esp32-monitor/client/internal/transport/wire"
	"github.com/mochi-mqtt/server/v2/listeners"
	"net"
	"time"
)

func (b *Backend) bindIP() string { return b.networkIP.Load().(string) }
func (b *Backend) httpAddress() string {
	_, port, _ := net.SplitHostPort(b.opts.HTTPAddress)
	return net.JoinHostPort(b.bindIP(), port)
}
func interfaceForIP(ip string) string {
	interfaces, _ := net.Interfaces()
	for _, i := range interfaces {
		as, _ := i.Addrs()
		for _, a := range as {
			if n, ok := a.(*net.IPNet); ok && n.IP.Equal(net.ParseIP(ip)) {
				return i.Name
			}
		}
	}
	return ""
}

// UpdateLANInterface preserves host/pairing identities, invalidates old LAN
// sessions and starts fresh authenticated invitations from the new source IP.
func (b *Backend) UpdateLANInterface(ip string) error {
	if net.ParseIP(ip).To4() == nil || net.ParseIP(ip).IsUnspecified() || net.ParseIP(ip).IsMulticast() {
		return wire.ErrMessage
	}
	b.networkChangeMu.Lock()
	defer b.networkChangeMu.Unlock()
	if b.ctx.Err() != nil {
		return errors.New("companion_closed")
	}
	if ip == b.bindIP() {
		return nil
	}
	return b.rebind(ip, true)
}
func (b *Backend) rebind(ip string, selected bool) error {
	// Stage the replacement listener before retiring a healthy old interface.
	_, port, _ := net.SplitHostPort(b.opts.MQTTAddress)
	listener := listeners.NewTCP(listeners.Config{ID: "lan-" + wire.NewID(), Address: net.JoinHostPort(ip, port)})
	if e := b.broker.AddListener(listener); e != nil {
		return errors.New("mqtt_listen_failed")
	}
	b.discoveryMu.Lock()
	defer b.discoveryMu.Unlock()
	b.pairing.close()
	b.mu.Lock()
	old := b.listener
	for id, s := range b.sessions {
		if s.transport == "lan" {
			s.cancel()
			if s.client != nil {
				s.client.Stop(errors.New("network_changed"))
			}
			delete(b.sessions, id)
			b.recovery[id] = &recoveryState{next: time.Now().Add(10 * time.Second)}
		}
	}
	b.listener = listener
	b.networkIP.Store(ip)
	if selected {
		b.interfaceName = interfaceForIP(ip)
	}
	b.mu.Unlock()
	b.broker.Listeners.Close(old.ID(), func(id string) {
		for _, c := range b.broker.Clients.GetByListener(id) {
			c.Stop(errors.New("network_changed"))
		}
	})
	b.broker.Listeners.Delete(old.ID())
	b.broker.Listeners.Serve(listener.ID(), b.broker.EstablishConnection)
	return nil
}
func (b *Backend) watchInterface() {
	defer b.wg.Done()
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-b.ctx.Done():
			return
		case <-t.C:
		}
		b.mu.Lock()
		name := b.interfaceName
		b.mu.Unlock()
		if name == "" {
			continue
		}
		iface, e := net.InterfaceByName(name)
		if e != nil {
			continue
		}
		as, e := iface.Addrs()
		if e != nil {
			continue
		}
		currentPresent := false
		next := ""
		for _, a := range as {
			if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil {
				if n.IP.Equal(net.ParseIP(b.bindIP())) {
					currentPresent = true
				}
				if !n.IP.IsLinkLocalUnicast() {
					next = n.IP.String()
				}
			}
		}
		if currentPresent || next == "" {
			continue
		}
		b.networkChangeMu.Lock()
		if b.ctx.Err() == nil {
			_ = b.rebind(next, false)
		}
		b.networkChangeMu.Unlock()
	}
}
