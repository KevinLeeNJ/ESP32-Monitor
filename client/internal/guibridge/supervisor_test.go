package guibridge

import (
	"slices"
	"testing"
)

func TestAgentArgsIgnoreUIPreferences(t *testing.T) {
	s := &Supervisor{socket: "/tmp/test.sock", agentPath: "/tmp/monitor-agent"}
	base := Settings{Collector: CollectorSettings{Enabled: true}}
	uiOnly := base
	uiOnly.Launch = LaunchSettings{LaunchAtLogin: true, CloseToBackground: true, ShowInStatusBar: true}
	if !slices.Equal(s.agentArgsFor(base), s.agentArgsFor(uiOnly)) {
		t.Fatal("UI preferences must not restart the Agent")
	}
	collectorChange := uiOnly
	collectorChange.Collector.Enabled = false
	if slices.Equal(s.agentArgsFor(uiOnly), s.agentArgsFor(collectorChange)) {
		t.Fatal("collector changes must restart the Agent")
	}
}
