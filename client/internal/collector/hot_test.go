package collector

import (
	"context"
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	v2 "esp32-monitor/client/internal/collector/uploadv2"
)

type recordingCollector struct {
	mu   *sync.Mutex
	seen *[]string
	base Collector
}

func (r *recordingCollector) Gather(ctx context.Context, ids []string) (Batch, map[string]string) {
	r.mu.Lock()
	*r.seen = append(*r.seen, ids...)
	r.mu.Unlock()
	return r.base.Gather(ctx, ids)
}

func sameSources(a, b []string) bool {
	a = slices.Clone(a)
	b = slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(slices.Compact(a), slices.Compact(b))
}

func TestSetUploadTargetSwitchesInPlace(t *testing.T) {
	m := newV2Fixture(t, "https://old.example/v2/collector/samples", "", new(atomic.Int32), new(atomic.Bool))
	if err := m.SetUploadTarget("https://new.example/v2/collector/samples", ""); err != nil {
		t.Fatal(err)
	}
	if got := m.uploadTarget().URL; got != "https://new.example/v2/collector/samples" {
		t.Fatalf("target = %q", got)
	}
	if err := m.SetUploadTarget("https://new.example/wrong", ""); err == nil {
		t.Fatal("invalid endpoint accepted")
	}
	if got := m.uploadTarget().URL; got != "https://new.example/v2/collector/samples" {
		t.Fatalf("failed switch changed target: %q", got)
	}
}

func TestSetCollectingPausesAndResumesInPlace(t *testing.T) {
	m := newV2Fixture(t, "", "", new(atomic.Int32), new(atomic.Bool))
	caps, err := m.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	id := ""
	for _, category := range caps.Categories {
		for _, source := range category.Sources {
			if id == "" && len(source.Metrics) > 0 {
				id = source.Metrics[0].ID
			}
		}
	}
	if id == "" {
		t.Fatal("fixture exposed no metrics")
	}
	if _, err := m.Apply(context.Background(), Config{IntervalSeconds: 5, Selections: []Selection{{ID: id}}}); err != nil {
		t.Fatal(err)
	}
	revision := m.Snapshot().Revision
	m.SetCollecting(false)
	if m.CollectingEnabled() || m.Snapshot().State != "disabled" || m.current != nil {
		t.Fatalf("pause: enabled=%v state=%s current=%v", m.CollectingEnabled(), m.Snapshot().State, m.current)
	}
	if m.Snapshot().Revision != revision {
		t.Fatal("pause changed the committed configuration")
	}
	m.MarkPreview()
	if m.previewActive() {
		t.Fatal("preview started while collection was paused")
	}
	m.SetCollecting(true)
	deadline := time.Now().Add(3 * time.Second)
	for m.Snapshot().State != "healthy" {
		if time.Now().After(deadline) {
			t.Fatalf("resume did not start: %+v", m.Snapshot())
		}
		time.Sleep(10 * time.Millisecond)
	}
	if m.Snapshot().Revision <= revision {
		t.Fatal("resume did not apply the committed configuration")
	}
}

func TestCollectConfigOnlyUsesUploadSelections(t *testing.T) {
	c := v2Config(v2.UploadSelection{Metric: "gpu.usage", Objects: []uint16{1}})
	upload := selectedSources(v2CollectConfig(c))
	preview := selectedSources(v2PreviewConfig(c))
	if len(preview) <= len(upload) {
		t.Fatalf("preview sources %v do not exceed upload sources %v", preview, upload)
	}
	for _, source := range upload {
		if !slices.Contains(preview, source) {
			t.Fatalf("upload source %q missing from preview %v", source, preview)
		}
	}
}

func TestCollectLoopWithoutGUISamplesUploadSelectionsOnly(t *testing.T) {
	missing := new(atomic.Bool)
	var mu sync.Mutex
	var seen []string
	core := &recordingCollector{mu: &mu, seen: &seen, base: &v2Fixture{fixedReads: new(atomic.Int32), missing: missing}}
	native := &recordingCollector{mu: &mu, seen: &seen, base: &v2Fixture{fixedReads: new(atomic.Int32), missing: missing}}
	c := v2Config(v2.UploadSelection{Metric: "gpu.usage", Objects: []uint16{1}})
	m := &Manager{opts: Options{ApplyTimeout: time.Second}, ctx: context.Background(), previewCh: make(chan struct{}, 1), v2: &v2State{IDs: map[string]uint16{}, Next: 1}}
	g := &generation{config: c, collect: v2PreviewConfig(c), revision: 1, core: core, native: native, slow: map[string]int64{}, done: make(chan struct{})}
	m.current = g
	m.snapshot.Config = c
	m.snapshot.Revision = 1
	ctx, cancel := context.WithCancel(context.Background())
	go m.collectLoop(ctx, g)

	take := func() []string {
		deadline := time.Now().Add(2 * time.Second)
		for {
			mu.Lock()
			got := slices.Clone(seen)
			seen = nil
			mu.Unlock()
			if len(got) > 0 {
				return got
			}
			if time.Now().After(deadline) {
				t.Fatal("collectLoop did not gather")
			}
			time.Sleep(5 * time.Millisecond)
		}
	}

	m.previewCh <- struct{}{}
	if got, want := take(), selectedSources(v2CollectConfig(c)); !sameSources(got, want) {
		t.Fatalf("GUI closed gathered %v, want upload sources %v", got, want)
	}
	m.previewAt.Store(time.Now().UnixNano())
	m.previewCh <- struct{}{}
	if got, want := take(), selectedSources(v2PreviewConfig(c)); !sameSources(got, want) {
		t.Fatalf("GUI open gathered %v, want preview sources %v", got, want)
	}
	cancel()
	<-g.done
}

func TestSetCollectingReleasesNothingOnPause(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	m, err := New(Options{StateDirectory: dir, CoreFactory: func() Collector { return &v2Fixture{new(atomic.Int32), new(atomic.Bool)} }, NativeFactory: func() Collector { return &v2Fixture{new(atomic.Int32), new(atomic.Bool)} }, ApplyTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	m.SetCollecting(false)
	m.SetCollecting(false)
	if err := m.SetUploadTarget("https://example.com/v2/collector/samples", ""); err != nil {
		t.Fatalf("state lock released or target rejected while paused: %v", err)
	}
}
