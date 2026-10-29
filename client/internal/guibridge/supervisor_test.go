package guibridge

import (
	"slices"
	"testing"
)

func TestCodexEnabledStartsTaskObserver(t *testing.T) {
	s := &Supervisor{socket: "test.sock"}
	settings := Settings{}
	settings.Codex.Enabled = true
	args := s.agentArgsFor(settings)
	if !slices.Contains(args, "--codex") || !slices.Contains(args, "--codex-task-adapter") || !slices.Contains(args, "auto") {
		t.Fatalf("missing task observer: %v", args)
	}
	settings.Codex.Enabled = false
	if slices.Contains(s.agentArgsFor(settings), "--codex-task-adapter") {
		t.Fatal("observer enabled while Codex disabled")
	}
}

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
