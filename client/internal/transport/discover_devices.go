package transport

import (
	"context"
	"errors"
	"esp32-monitor/client/internal/logging"
	"go.bug.st/serial"
	"go.bug.st/serial/enumerator"
	"runtime"
	"strings"
	"sync"
	"time"
)

// likelyMonitorSerial hides virtual/Bluetooth ports that cannot be an ESP32.
// Known USB bridge vendors and USB-CDC port names are kept; an unknown port
// with no USB identity is not offered as a Monitor candidate.
func likelyMonitorSerial(p DiscoveredDevice) bool {
	vid := strings.ToLower(strings.TrimPrefix(strings.ToLower(p.VID), "0x"))
	switch vid {
	case "303a", "10c4", "1a86", "0403":
		return true
	}
	name := strings.ToLower(p.Port)
	for _, marker := range []string{"usbmodem", "usbserial", "slab_usbtouart", "wchusbserial", "ttyacm", "ttyusb"} {
		if strings.Contains(name, marker) {
			return true
		}
	}
	return false
}

// DiscoveredDevice is the frontend contract for one reachable connection route.
// DeviceID is empty for an enumerated serial port until a valid HELLO identifies
// the Monitor. A USB product label never proves protocol identity or online state.
type DiscoveredDevice struct {
	DeviceID         string `json:"device_id"`
	Name             string `json:"name"`
	ConnectionType   string `json:"connection_type"`
	Address          string `json:"address"`
	Port             string `json:"port"`
	VID              string `json:"vid"`
	PID              string `json:"pid"`
	USBSerial        string `json:"usb_serial"`
	IdentityVerified bool   `json:"identity_verified"`
	Paired           bool   `json:"paired"`
	PairingLost      bool   `json:"pairing_lost"`
	Online           bool   `json:"online"`
	State            string `json:"state"`
}
type DiscoveryResult struct {
	Devices     []DiscoveredDevice `json:"devices"`
	LANError    string             `json:"lan_error"`
	SerialError string             `json:"serial_error"`
	Overflow    bool               `json:"overflow"`
}

func SerialPorts() ([]DiscoveredDevice, error) {
	ports, e := enumerator.GetDetailedPortsList()
	if e != nil {
		return nil, errors.New("serial_enumeration_failed")
	}
	out := make([]DiscoveredDevice, 0, len(ports))
	for _, p := range ports {
		name := p.Product
		if name == "" {
			name = p.Name
		}
		out = append(out, DiscoveredDevice{Name: name, ConnectionType: "serial", Port: p.Name, VID: p.VID, PID: p.PID, USBSerial: p.SerialNumber, State: "unverified"})
	}
	return out, nil
}

// DiscoverDevices concurrently discovers LAN Monitors and enumerates serial
// candidates. No commands are sent to unknown ports. Frontends may immediately
// render serial results through ListSerialPorts while the LAN round completes.
func (b *Backend) DiscoverDevices(ctx context.Context) DiscoveryResult {
	var result DiscoveryResult
	var wg sync.WaitGroup
	var lanErr error
	var ports []DiscoveredDevice
	var serialErr error
	wg.Add(2)
	go func() { defer wg.Done(); lanErr = b.Discover(ctx) }()
	go func() {
		defer wg.Done()
		var e error
		ports, e = b.ListSerialPorts()
		if e != nil {
			serialErr = e
			return
		}
		b.probeSerialCandidates(ctx, ports)
		ports = b.enrichSerialPorts(ports)
	}()
	wg.Wait()
	statuses, overflow := b.Devices()
	result.Overflow = overflow
	result.Devices = make([]DiscoveredDevice, 0, len(statuses)+len(ports))
	for _, s := range statuses {
		result.Devices = append(result.Devices, DiscoveredDevice{DeviceID: s.DeviceID, Name: s.Name, ConnectionType: "lan", Address: s.Address, IdentityVerified: s.IdentityVerified && s.Transport != "serial" && s.ConfirmedOnline, Paired: s.Paired, PairingLost: s.PairingLost, Online: s.ConfirmedOnline, State: s.ConnectionState})
	}
	result.Devices = append(result.Devices, ports...)
	if lanErr != nil {
		result.LANError = lanErr.Error()
	}
	if serialErr != nil {
		result.SerialError = serialErr.Error()
	}
	logging.L("transport").Info("discovery", "devices", len(result.Devices), "lan_error", result.LANError, "serial_error", result.SerialError)
	return result
}
func (b *Backend) serialCandidates() ([]DiscoveredDevice, error) {
	enumerate := b.opts.EnumerateSerial
	if enumerate == nil {
		enumerate = SerialPorts
	}
	ports, e := enumerate()
	if e != nil {
		return nil, e
	}
	filtered := make([]DiscoveredDevice, 0, len(ports))
	for _, p := range ports {
		if likelyMonitorSerial(p) {
			filtered = append(filtered, p)
		}
	}
	return filtered, nil
}

func (b *Backend) ListSerialPorts() ([]DiscoveredDevice, error) {
	ports, e := b.serialCandidates()
	if e != nil {
		return nil, e
	}
	return b.enrichSerialPorts(ports), nil
}

func (b *Backend) enrichSerialPorts(ports []DiscoveredDevice) []DiscoveredDevice {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i, p := range ports {
		if known, ok := b.serialObserved[p.Port]; ok {
			ports[i].Name = known.Name
			ports[i].DeviceID = known.DeviceID
			_, ports[i].Paired = b.store.pair(known.DeviceID)
			if s := b.sessions[known.DeviceID]; s != nil && s.transport == "serial" {
				ports[i].State = s.state
				ports[i].Online = s.state == "connected"
				ports[i].IdentityVerified = true
			}
		}
	}
	// A paired serial port keeps its identity across Agent restarts even
	// before this process has probed it again.
	for i, p := range ports {
		// Only fall back to the stored port mapping while the port identity is
		// unknown. A HELLO probe always wins over a stale stored name.
		if p.Paired || p.DeviceID != "" {
			continue
		}
		for _, pair := range b.store.pairs() {
			if pair.SerialPort != p.Port {
				continue
			}
			ports[i].DeviceID = pair.DeviceID
			ports[i].Name = pair.DeviceName
			ports[i].Paired = true
			if s := b.sessions[pair.DeviceID]; s != nil && s.transport == "serial" {
				ports[i].State = s.state
				ports[i].Online = s.state == "connected"
				ports[i].IdentityVerified = true
			}
			break
		}
	}
	return ports
}

// probeSerialCandidates sends one bounded, read-only HELLO to every likely
// Monitor port so the discovery list shows the real device identity and shared
// pairing state. Ports already in use and concurrent probes are skipped.
func (b *Backend) probeSerialCandidates(ctx context.Context, ports []DiscoveredDevice) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, 2)
	for i := range ports {
		port := ports[i].Port
		if port == "" {
			continue
		}
		b.mu.Lock()
		busy := b.serialPorts[port] || b.serialProbing[port]
		b.mu.Unlock()
		if busy {
			continue
		}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			probeCtx, cancel := context.WithTimeout(ctx, serialProbeTimeout)
			defer cancel()
			d, err := b.ProbeSerial(probeCtx, port)
			if err != nil || d.DeviceID == "" {
				return
			}
			ports[i].DeviceID = d.DeviceID
			ports[i].Name = d.Name
			ports[i].Paired = d.Paired
			ports[i].IdentityVerified = true
			ports[i].State = "identified"
			if d.Paired {
				b.startPairedSerial(port, d.DeviceID)
			}
		}(i)
	}
	wg.Wait()
}
func OpenSerialPort(name string) (serial.Port, error) {
	p, e := serial.Open(name, &serial.Mode{BaudRate: 115200, DataBits: 8, Parity: serial.NoParity, StopBits: serial.OneStopBit, InitialStatusBits: &serial.ModemOutputBits{DTR: false, RTS: false}})
	if e != nil {
		var portErr *serial.PortError
		if runtime.GOOS == "linux" && errors.As(e, &portErr) && portErr.Code() == serial.PermissionDenied {
			return nil, errors.New("serial_permission_denied")
		}
		return nil, errors.New("serial_unavailable_or_busy")
	}
	if e = p.SetReadTimeout(100 * time.Millisecond); e != nil {
		p.Close()
		return nil, e
	}
	_ = p.ResetInputBuffer()
	return p, nil
}
