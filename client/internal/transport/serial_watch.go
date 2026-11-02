package transport

import (
	"context"
	"errors"
	"esp32-monitor/client/internal/logging"
	"time"
)

const (
	serialScanInterval = 2 * time.Second
	serialProbeTimeout = 2500 * time.Millisecond
)

// watchSerialPorts keeps paired Monitors connected over USB. A LAN-paired
// device is switched as soon as an authenticated serial link is available;
// a renamed or re-plugged port is identified again by a read-only HELLO.
func (b *Backend) watchSerialPorts() {
	defer b.wg.Done()
	ticker := time.NewTicker(serialScanInterval)
	defer ticker.Stop()
	b.scanSerialPorts(time.Now())
	for {
		select {
		case <-b.ctx.Done():
			return
		case now := <-ticker.C:
			b.scanSerialPorts(now)
		}
	}
}

func (b *Backend) scanSerialPorts(now time.Time) {
	ports, err := b.serialCandidates()
	if err != nil {
		return
	}
	pairs := b.store.pairs()
	byPort := make(map[string]Pairing, len(pairs))
	for _, pair := range pairs {
		if pair.SerialPort != "" {
			byPort[pair.SerialPort] = pair
		}
	}
	for _, port := range ports {
		if port.Port == "" {
			continue
		}
		b.mu.Lock()
		if b.ctx.Err() != nil {
			b.mu.Unlock()
			return
		}
		if b.serialPorts[port.Port] || b.serialProbing[port.Port] {
			b.mu.Unlock()
			continue
		}
		state := b.serialProbe[port.Port]
		if state == nil {
			state = &recoveryState{}
			b.serialProbe[port.Port] = state
		}
		if now.Before(state.next) {
			b.mu.Unlock()
			continue
		}
		device := ""
		if observed, ok := b.serialObserved[port.Port]; ok {
			device = observed.DeviceID
		}
		if device == "" {
			device = byPort[port.Port].DeviceID
		}
		if device != "" {
			_, paired := b.store.pair(device)
			if !paired {
				device = ""
			} else if b.disabled[device] {
				b.mu.Unlock()
				continue
			}
		}
		if device != "" {
			b.mu.Unlock()
			b.startPairedSerial(port.Port, device)
			continue
		}
		state.attempt++
		state.next = now.Add(serialProbeDelay(state.attempt))
		b.serialProbing[port.Port] = true
		b.mu.Unlock()
		b.startSerialProbe(port.Port)
	}
}

func serialProbeDelay(attempt int) time.Duration {
	delay := 5 * time.Second
	for i := 1; i < attempt && delay < time.Minute; i++ {
		delay *= 2
	}
	if delay > time.Minute {
		delay = time.Minute
	}
	return delay
}

func (b *Backend) startSerialProbe(port string) {
	started := b.track(func() {
		defer func() {
			b.mu.Lock()
			delete(b.serialProbing, port)
			b.mu.Unlock()
		}()
		b.probeSerialPort(port)
	})
	if !started {
		b.mu.Lock()
		delete(b.serialProbing, port)
		b.mu.Unlock()
	}
}

func (b *Backend) probeSerialPort(port string) {
	ctx, cancel := context.WithTimeout(b.ctx, serialProbeTimeout)
	defer cancel()
	device, err := b.ProbeSerial(ctx, port)
	b.mu.Lock()
	state := b.serialProbe[port]
	if state == nil {
		state = &recoveryState{}
		b.serialProbe[port] = state
	}
	if err == nil {
		state.attempt = 0
		state.next = time.Now().Add(time.Minute)
	}
	paired := false
	if err == nil && device.DeviceID != "" {
		if _, paired = b.store.pair(device.DeviceID); paired && !b.disabled[device.DeviceID] {
			if s := b.sessions[device.DeviceID]; s != nil && s.transport == "serial" {
				paired = false
			}
		}
	}
	b.mu.Unlock()
	if paired {
		b.startPairedSerial(port, device.DeviceID)
	}
}

// startKnownSerial reconnects a known USB port immediately after a device is
// re-enabled, before the LAN recovery loop can invite it.
func (b *Backend) startKnownSerial(device string) {
	pair, ok := b.store.pair(device)
	if !ok || pair.SerialPort == "" {
		return
	}
	ports, err := b.serialCandidates()
	if err != nil {
		return
	}
	for _, port := range ports {
		if port.Port == pair.SerialPort {
			b.startPairedSerial(port.Port, device)
			return
		}
	}
}

// resumeSerialProbe lets a just-paired device switch to USB immediately even
// if its port was identified as unpaired a moment earlier.
func (b *Backend) resumeSerialProbe(device string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for port, observed := range b.serialObserved {
		if observed.DeviceID != device {
			continue
		}
		if state := b.serialProbe[port]; state != nil {
			state.attempt = 0
			state.next = time.Time{}
		}
	}
}

// startPairedSerial claims the device for USB before opening the port, so the
// LAN recovery loop cannot invite the same Monitor in the meantime.
func (b *Backend) startPairedSerial(port, device string) bool {
	b.mu.Lock()
	if b.ctx.Err() != nil || b.disabled[device] || b.serialPending[device] || b.serialPorts[port] {
		b.mu.Unlock()
		return false
	}
	if _, ok := b.store.pair(device); !ok {
		b.mu.Unlock()
		return false
	}
	if s := b.sessions[device]; s != nil && s.transport == "serial" {
		b.mu.Unlock()
		return false
	}
	b.serialPending[device] = true
	b.mu.Unlock()
	b.autoConnectSerial(port, device)
	return true
}

func (b *Backend) autoConnectSerial(port, device string) {
	started := b.track(func() {
		defer func() {
			b.mu.Lock()
			delete(b.serialPending, device)
			b.mu.Unlock()
		}()
		logging.L("transport").Info("serial_autoconnect", "device", logging.Ref(device), "port", port)
		err := b.ConnectSerial(b.ctx, port, SerialSelection{DeviceID: device})
		if err == nil || b.ctx.Err() != nil || errors.Is(err, context.Canceled) || err.Error() == "device_disabled" {
			return
		}
		logging.L("transport").Warn("serial_autoconnect_failed", "device", logging.Ref(device), "port", port, "error", err.Error())
		b.mu.Lock()
		if err.Error() == "unauthorized" {
			// The stored port identity may have been reused by another device.
			delete(b.serialObserved, port)
		}
		state := b.serialProbe[port]
		if state == nil {
			state = &recoveryState{}
			b.serialProbe[port] = state
		}
		state.attempt++
		state.next = time.Now().Add(serialProbeDelay(state.attempt))
		b.mu.Unlock()
	})
	if !started {
		b.mu.Lock()
		delete(b.serialPending, device)
		b.mu.Unlock()
	}
}

// track starts a goroutine counted by the backend wait group. The ctx check
// under mu prevents an Add racing a completed Close/Wait.
func (b *Backend) track(fn func()) bool {
	b.mu.Lock()
	if b.ctx.Err() != nil {
		b.mu.Unlock()
		return false
	}
	b.wg.Add(1)
	b.mu.Unlock()
	go func() {
		defer b.wg.Done()
		fn()
	}()
	return true
}
