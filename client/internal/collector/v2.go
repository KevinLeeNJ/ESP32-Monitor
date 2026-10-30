package collector

import (
	"encoding/json"
	"errors"
	v2 "esp32-monitor/client/internal/collector/uploadv2"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type V2Metric struct {
	Metric    string   `json:"metric"`
	Objects   []uint16 `json:"objects,omitempty"`
	Fixed     bool     `json:"fixed"`
	Available bool     `json:"available"`
	Required  bool     `json:"required"`
	Reason    string   `json:"reason,omitempty"`
}
type V2Capabilities struct {
	Inventory v2.Inventory `json:"inventory"`
	Metrics   []V2Metric   `json:"metrics"`
	Objects   []V2Object   `json:"objects"`
}
type V2Object struct {
	ID   uint16 `json:"id"`
	Kind string `json:"kind"`
	Name string `json:"name"`
}
type v2Binding struct{ source, field, kind string }

// Native-backed bindings resolve to this platform's source IDs. A missing role
// (e.g. macOS memory breakdown on Windows) binds to "" and stays unavailable.
var v2Bindings = buildV2Bindings()

func buildV2Bindings() map[string]v2Binding {
	return map[string]v2Binding{
		"cpu.usage": {"cpu", "usage_active", ""}, "memory.usage": {"mem", "used_percent", ""}, "system.uptime": {"system", "uptime", ""},
		"cpu.load1": {nativeLoadSource(), "load1", ""}, "cpu.load5": {nativeLoadSource(), "load5", ""}, "cpu.load15": {nativeLoadSource(), "load15", ""},
		"cpu.temperature": {nativeSourceID("cpu"), "temperature_celsius", ""}, "core.usage": {"cpu", "usage_active", "core"},
		"memory.used": {"mem", "used", ""}, "memory.app": {nativeSourceID("memory"), "app_bytes", ""}, "memory.wired": {nativeSourceID("memory"), "wired_bytes", ""}, "memory.compressed": {nativeSourceID("memory"), "compressed_bytes", ""},
		"memory.swap_total": {"swap", "total", ""}, "memory.swap_used": {"swap", "used", ""}, "memory.pressure": {nativeSourceID("memory"), "pressure_level", ""},
		"gpu.usage": {nativeSourceID("gpu"), "usage_percent", "gpu"}, "gpu.temperature": {nativeSourceID("gpu"), "temperature_celsius", "gpu"},
		"drive.read_rate": {"diskio", "read_bytes_per_second", "drive"}, "drive.write_rate": {"diskio", "write_bytes_per_second", "drive"}, "drive.used": {nativeSourceID("disk_usage"), "used", "drive"},
		"drive.smart_health":    {nativeSourceID("disk"), "smart_healthy", "drive"},
		"interface.upload_rate": {"net", "upload_bytes_per_second", "interface"}, "interface.download_rate": {"net", "download_bytes_per_second", "interface"},
		"interface.up": {nativeSourceID("network"), "connected", "interface"}, "interface.ipv4": {nativeSourceID("network"), "connected", "interface"}, "network.public_ipv4": {nativeSourceID("public_ip"), "present", ""},
		"battery.level": {nativeSourceID("battery"), "charge_percent", "battery"}, "battery.charging": {nativeSourceID("battery"), "charging", "battery"},
	}
}

var v2Static = map[string]string{
	"cpu.model": "", "cpu.physical_cores": "", "cpu.logical_cores": "", "cpu.performance_cores": "", "cpu.efficiency_cores": "", "memory.total": "", "system.os": "", "system.product": "",
	"drive.model": "drive", "drive.total": "drive", "interface.medium": "interface",
}

// The persisted envelope contains IDs/names plus a confirmed Profile. Its
// limit is separate from the 256 KiB HTTP body and sample-queue budget.
const maxV2StateBody = 4 * maxBody

type v2State struct {
	IDs map[string]uint16 `json:"ids"`
	// Names holds GUI display names (GPU/disk model) keyed like IDs. Object
	// keys stay stable device identifiers.
	Names     map[string]string `json:"names,omitempty"`
	AuthHash  string            `json:"auth_hash,omitempty"`
	Next      uint32            `json:"next"`
	Endpoint  string            `json:"endpoint"`
	Profile   *v2.Profile       `json:"profile,omitempty"`
	Revision  uint64            `json:"revision"`
	inventory v2.Inventory
	// confirmed wire state is used only under delivery; IDs/inventory freeze at startup.
}

func ptr[T any](v T) *T { return &v }
func (m *Manager) initV2() error {
	s := &v2State{IDs: map[string]uint16{}, Next: 1, Endpoint: m.opts.UploadURL}
	path := filepath.Join(m.opts.StateDirectory, "collector-v2.json")
	if _, err := os.Lstat(path); err == nil {
		data, e := readPrivateFile(path, maxV2StateBody)
		if e != nil {
			return errors.New("invalid_v2_state")
		}
		if json.Unmarshal(data, s) != nil || s.IDs == nil || s.Next == 0 || s.Next > 65536 {
			return errors.New("invalid_v2_state")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if s.Endpoint != m.opts.UploadURL {
		s.Endpoint = m.opts.UploadURL
		s.Profile = nil
		s.Revision = 0
		s.AuthHash = ""
	}
	used := map[uint16]bool{}
	for _, id := range s.IDs {
		if id == 0 || used[id] || uint32(id) >= s.Next {
			return errors.New("invalid_v2_object_ids")
		}
		used[id] = true
	}
	m.v2 = s
	if err := s.buildInventory(m.fixed); err != nil {
		return err
	}
	return m.persistV2()
}
func (m *Manager) persistV2() error {
	b, err := json.Marshal(m.v2)
	if err == nil && len(b) > maxV2StateBody {
		return errors.New("v2_state_too_large")
	}
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(m.opts.StateDirectory, ".collector-v2-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	if e := f.Close(); err == nil {
		err = e
	}
	if err == nil {
		err = os.Rename(f.Name(), filepath.Join(m.opts.StateDirectory, "collector-v2.json"))
	}
	return err
}
func objectKey(kind string, m Metric) string {
	var key string
	switch kind {
	case "core":
		key = m.Tags["cpu"]
		if key == "cpu-total" {
			return ""
		}
	case "gpu":
		key = m.Tags["device"]
	case "drive":
		key = m.Tags["device"]
		if key == "" {
			key = m.Tags["name"]
		}
	case "volume":
		key = m.Tags["device"] + ":" + m.Tags["path"]
	case "interface":
		key = m.Tags["interface"]
	case "battery":
		key = "0"
	}
	if key == "" {
		return ""
	}
	return kind + ":" + key
}
func (s *v2State) object(kind string, m Metric) uint16 { return s.IDs[objectKey(kind, m)] }
func objectDisplayName(kind string, m Metric) string {
	switch kind {
	case "gpu", "drive":
		return strings.TrimSpace(m.Tags["model"])
	}
	return ""
}
func (s *v2State) allocate(kind string, m Metric) (uint16, error) {
	key := objectKey(kind, m)
	if key == "" {
		return 0, errors.New("invalid_v2_object")
	}
	if name := objectDisplayName(kind, m); name != "" {
		if s.Names == nil {
			s.Names = map[string]string{}
		}
		s.Names[key] = name
	}
	if id := s.IDs[key]; id != 0 {
		return id, nil
	}
	if s.Next > 65535 {
		return 0, errors.New("v2_object_ids_exhausted")
	}
	id := uint16(s.Next)
	s.Next++
	s.IDs[key] = id
	return id, nil
}
func number(m Metric, key string) *uint64 {
	if raw, ok := m.Exact[key]; ok {
		n, e := strconv.ParseUint(raw, 10, 64)
		if e == nil {
			return &n
		}
		return nil
	}
	v, ok := m.Fields[key]
	if !ok || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 9007199254740991 || v != math.Trunc(v) {
		return nil
	}
	return ptr(uint64(v))
}
func count(m Metric, key string) *uint16 {
	if v := number(m, key); v != nil && *v <= 65535 {
		return ptr(uint16(*v))
	}
	return nil
}
func text(tags map[string]string, key string) *string {
	if v := tags[key]; v != "" {
		return ptr(v)
	}
	return nil
}
func (m *Manager) profileURL() string {
	u, _ := url.Parse(m.opts.UploadURL)
	u.Path = "/v2/collector/profile"
	return u.String()
}
