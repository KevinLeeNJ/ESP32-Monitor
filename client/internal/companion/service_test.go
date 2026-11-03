package companion

import (
	"context"
	"testing"
	"time"

	"esp32-monitor/client/internal/nowplaying"
	"esp32-monitor/client/internal/providers"
	"esp32-monitor/client/internal/providers/codex"
	"esp32-monitor/client/internal/providers/github"
)

type fakeCodex struct {
	snapshot  codex.Snapshot
	refreshed bool
}

func (f *fakeCodex) Refresh(context.Context) codex.Snapshot { f.refreshed = true; return f.snapshot }
func (f *fakeCodex) Snapshot() codex.Snapshot               { return f.snapshot }
func (f *fakeCodex) RunTasks(context.Context)               {}
func (f *fakeCodex) WaitTasks(context.Context)              {}

type fakeMusic struct{ snapshot nowplaying.Snapshot }

func (f *fakeMusic) ID() string                      { return "fake" }
func (f *fakeMusic) Run(context.Context)             {}
func (f *fakeMusic) WaitReady(context.Context) error { return nil }
func (f *fakeMusic) Snapshot() nowplaying.Snapshot   { return f.snapshot }

func TestRefreshCombinesEnabledProviders(t *testing.T) {
	githubProvider, err := github.New(github.Options{Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	codexSnapshot := codex.Snapshot{Quota: codex.Quota{Health: providers.Health{State: providers.Healthy}}}
	musicSnapshot := nowplaying.Snapshot{Adapter: "fake", Health: providers.Health{State: providers.Healthy}}
	fake := &fakeCodex{snapshot: codexSnapshot}
	service := Service{GitHub: githubProvider, Codex: fake, Music: &fakeMusic{snapshot: musicSnapshot}}

	before := time.Now().UTC()
	snapshot := service.Refresh(context.Background())
	if !fake.refreshed {
		t.Fatal("Codex was not refreshed")
	}
	if snapshot.SchemaVersion != 1 || snapshot.ObservedAt.Before(before) {
		t.Fatalf("unexpected snapshot metadata: %+v", snapshot)
	}
	if snapshot.GitHub.Health.State != providers.Disabled {
		t.Fatalf("disabled GitHub state: %+v", snapshot.GitHub.Health)
	}
	if snapshot.Codex.Quota.Health.State != providers.Healthy || snapshot.Music.Adapter != "fake" {
		t.Fatalf("provider snapshots not combined: %+v", snapshot)
	}
}

func TestSnapshotWithoutProvidersIsSafe(t *testing.T) {
	snapshot := (&Service{}).Snapshot()
	if snapshot.SchemaVersion != 1 || snapshot.ObservedAt.IsZero() {
		t.Fatalf("unexpected empty snapshot: %+v", snapshot)
	}
	if snapshot.GitHub.Health.State != "" || snapshot.Codex.Quota.Health.State != "" || snapshot.Music.Health.State != "" {
		t.Fatalf("nil providers produced state: %+v", snapshot)
	}
}
