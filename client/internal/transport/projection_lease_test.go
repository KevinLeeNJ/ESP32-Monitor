package transport

import (
	"esp32-monitor/client/internal/transport/wire"
	"testing"
	"time"
)

func TestSourceLeasesCarryOnlyObservedHealthySources(t *testing.T) {
	now := time.Now().Unix()
	observed := func() wire.Section {
		s := health("healthy", 15000, 15000)
		age := int64(0)
		s.ObservedAt = &now
		s.LastSuccessAt = &now
		s.AgeMS = &age
		return s
	}
	tasks := health("healthy", 15000, 15000)
	sections := wire.Sections{
		GitHub: observed(),
		Codex: wire.Section{Health: wire.Health{State: "healthy"},
			Data: map[string]any{"quota": observed(), "tasks": tasks}},
		Music: observed(),
		Proxy: wire.Section{Health: wire.Health{State: "healthy"},
			Data: wire.ProxyData{Time: observed(), Weather: observed(), AQI: observed(), Devices: observed()}},
	}

	want := map[string]bool{
		"github": true, "codex.quota": true, "music": true,
		"proxy.weather": true, "proxy.aqi": true, "proxy.devices": true,
	}
	for _, lease := range leases(sections, 42) {
		if lease.SnapshotSeq != 42 || lease.LastSuccessAt != now || lease.AgeMS != 0 {
			t.Fatalf("lease does not carry the observation: %+v", lease)
		}
		if !want[lease.Path] {
			t.Fatalf("unexpected lease path %q", lease.Path)
		}
		delete(want, lease.Path)
	}
	if len(want) != 0 {
		t.Fatalf("missing leases: %v", want)
	}
}
