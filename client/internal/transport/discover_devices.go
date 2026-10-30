package transport

import (
	"context"
	"errors"
	"esp32-monitor/client/internal/logging"
	"go.bug.st/serial"
	"go.bug.st/serial/enumerator"
	"sync"
	"time"
)

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
	go func() { defer wg.Done(); lanErr = b.Discover(ctx, "") }()
	go func() { defer wg.Done(); ports, serialErr = b.ListSerialPorts() }()
	wg.Wait()
	statuses, overflow := b.Devices()
	result.Overflow = overflow
	result.Devices = make([]DiscoveredDevice, 0, len(statuses)+len(ports))
	for _, s := range statuses {
		result.Devices = append(result.Devices, DiscoveredDevice{DeviceID: s.DeviceID, Name: s.Name, ConnectionType: "lan", Address: s.Address, IdentityVerified: s.IdentityVerified && s.ConfirmedOnline, Paired: s.Paired, Online: s.ConfirmedOnline, State: s.ConnectionState})
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
func (b *Backend) ListSerialPorts() ([]DiscoveredDevice, error) {
	enumerate := b.opts.EnumerateSerial
	if enumerate == nil {
		enumerate = SerialPorts
	}
	ports, e := enumerate()
	if e != nil {
		return nil, e
	}
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
	return ports, nil
}
func OpenSerialPort(name string) (serial.Port, error) {
	p, e := serial.Open(name, &serial.Mode{BaudRate: 115200, DataBits: 8, Parity: serial.NoParity, StopBits: serial.OneStopBit, InitialStatusBits: &serial.ModemOutputBits{DTR: false, RTS: false}})
	if e != nil {
		return nil, errors.New("serial_unavailable_or_busy")
	}
	if e = p.SetReadTimeout(100 * time.Millisecond); e != nil {
		p.Close()
		return nil, e
	}
	return p, nil
}
