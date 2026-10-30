package collector

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	v2 "esp32-monitor/client/internal/collector/uploadv2"
)

// catalogText bounds display text without splitting UTF-8 or forwarding control
// characters. Hardware serials, MACs and PNP identifiers are not display names.
func catalogText(value string) string {
	value = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, strings.ToValidUTF8(value, "�")))
	for len(value) > 512 {
		_, size := utf8.DecodeLastRuneInString(value)
		value = value[:len(value)-size]
	}
	return value
}

func (s *v2State) objectDirectory(b Batch) []v2.ObjectDescriptor {
	current := map[uint16]string{}
	for _, kind := range []string{"core", "gpu", "drive", "interface", "battery", "volume"} {
		for id := range s.objectsOfKind(kind) {
			current[id] = kind
		}
	}
	entries := map[uint16]v2.ObjectDescriptor{}
	// IDs also contain retired objects; only the current startup inventory is sent.
	for key, id := range s.IDs {
		kind, local, ok := strings.Cut(key, ":")
		if !ok || current[id] != kind {
			continue
		}
		name := catalogText(s.Names[key])
		if name == "" {
			switch kind {
			case "core":
				for _, c := range s.inventory.Cores {
					if c.ID == id {
						name = fmt.Sprintf("逻辑核 %d", c.Index)
					}
				}
			case "interface", "drive":
				name = catalogText(local)
			case "gpu":
				name = "GPU"
			case "battery":
				name = "系统电池"
			case "volume":
				name = "卷"
			}
		}
		entries[id] = v2.ObjectDescriptor{ID: id, Kind: kind, Name: name}
	}
	// Native fixed tags supply model/friendly-name information on both platforms.
	// Generic counter tags never overwrite richer native metadata.
	for _, m := range b.Metrics {
		for _, kind := range []string{"gpu", "drive", "interface", "battery"} {
			source := map[string]string{"gpu": "gpu", "drive": "disk", "interface": "network", "battery": "battery"}[kind]
			if m.Name != nativeSourceID(source) {
				continue
			}
			id := s.object(kind, m)
			entry, ok := entries[id]
			if !ok {
				continue
			}
			if model := catalogText(m.Tags["model"]); model != "" {
				entry.Model = ptr(model)
				if kind == "gpu" || kind == "drive" {
					entry.Name = model
				}
			}
			if name := catalogText(m.Tags["display_name"]); name != "" {
				entry.Name = name
			}
			if kind == "interface" {
				if name := catalogText(m.Tags["interface"]); name != "" && m.Tags["display_name"] == "" {
					entry.Name = name
				}
			}
			entries[id] = entry
		}
	}
	out := make([]v2.ObjectDescriptor, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
