package adapters

import (
	"context"
	"encoding/json"
	"esp32-monitor/client/internal/nowplaying"
	"esp32-monitor/client/internal/providers"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func ptr[T any](v T) *T { return &v }
func testAdapter(t *testing.T) *streamAdapter {
	t.Helper()
	a, err := New(Options{Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	return a.(*streamAdapter)
}
func TestSnapshotContract(t *testing.T) {
	a := testAdapter(t)
	a.update(Message{Current: &nowplaying.Current{Title: ptr("song"), State: nowplaying.Playing, Position: ptr(0.0)}})
	s := a.Snapshot()
	if s.Health.State != providers.Healthy || s.Current.Source != nil || s.Current.Position == nil || s.Current.PositionObservedAt == nil {
		t.Fatalf("%+v", s)
	}
	*s.Current.Title = "mutated"
	*s.Current.Position = 999
	*s.Health.LastSuccessAt = time.Time{}
	next := a.Snapshot()
	if *next.Current.Title != "song" || *next.Current.Position != 0 || next.Health.LastSuccessAt.IsZero() {
		t.Fatal("snapshot aliases internal state")
	}
	a.update(Message{})
	if s = a.Snapshot(); s.Current != nil || s.Health.State != providers.Healthy || s.Health.LastSuccessAt == nil {
		t.Fatal("empty must be healthy")
	}
	last := *s.Health.LastSuccessAt
	a.update(Message{ErrorCode: "music_permission_denied"})
	if s = a.Snapshot(); s.Current != nil || s.Health.State != providers.PermissionDenied || !s.Health.LastSuccessAt.Equal(last) {
		t.Fatal("failure must preserve last success and clear content")
	}
	b, _ := json.Marshal(s)
	if !strings.Contains(string(b), `"current":null`) {
		t.Fatal(string(b))
	}
}
func TestSanitizeAndFreshness(t *testing.T) {
	a := testAdapter(t)
	a.update(Message{Current: &nowplaying.Current{Title: ptr(strings.Repeat("中", 100) + "\x00"), Artist: ptr("\x00\nA\u202eB"), State: nowplaying.NotPlaying, Duration: ptr(5.0), Position: ptr(9.0)}})
	s := a.Snapshot()
	if len(*s.Current.Title) > 160 || !utf8.ValidString(*s.Current.Title) || *s.Current.Artist != "AB" || s.Current.State != nowplaying.NotPlaying || *s.Current.Position != 5 {
		t.Fatalf("%+v", s.Current)
	}
	a.mu.Lock()
	a.lastRead = time.Now().Add(-StaleAfter)
	a.mu.Unlock()
	if s = a.Snapshot(); s.Current != nil || s.Health.ErrorCode != "music_stale" {
		t.Fatal("stale content retained")
	}
	a.update(Message{Current: &nowplaying.Current{State: nowplaying.NotPlaying, Position: ptr(-1.0)}})
	if s = a.Snapshot(); s.Current.Position != nil || s.Current.PositionObservedAt != nil {
		t.Fatal("negative position accepted")
	}
	a.update(Message{ErrorCode: "sensitive platform exception"})
	if a.Snapshot().Health.ErrorCode != "music_read_failed" {
		t.Fatal("raw error leaked")
	}
}
func TestReadyAndDisabled(t *testing.T) {
	a, _ := New(Options{})
	if err := a.WaitReady(context.Background()); err != nil || a.Snapshot().Health.State != providers.Disabled {
		t.Fatal(err)
	}
	a.Run(context.Background())
	b := testAdapter(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if b.WaitReady(ctx) == nil {
		t.Fatal("ready before first result")
	}
	b.update(Message{ErrorCode: "music_bridge_missing"})
	if b.WaitReady(context.Background()) != nil {
		t.Fatal("errors must wake readiness")
	}
	if _, err := New(Options{Adapter: "nonexistent"}); err == nil {
		t.Fatal("unknown adapter accepted")
	}
}
func helper(a *streamAdapter, mode string) {
	a.command = func(ctx context.Context) (*exec.Cmd, error) {
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestBridgeProcess$")
		cmd.Env = append(os.Environ(), "MONITOR_MEDIA_HELPER="+mode)
		return cmd, nil
	}
}
func TestStreamLifecycle(t *testing.T) {
	a := testAdapter(t)
	helper(a, "valid")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { a.Run(ctx); close(done) }()
	ready, c := context.WithTimeout(ctx, 3*time.Second)
	defer c()
	if err := a.WaitReady(ready); err != nil {
		t.Fatal(err)
	}
	if s := a.Snapshot(); s.Current == nil || *s.Current.Title != "real helper" {
		t.Fatalf("%+v", s)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("bridge did not terminate")
	}
	if a.Snapshot().Current != nil {
		t.Fatal("cancel retained content")
	}
}
func TestMalformedBridge(t *testing.T) {
	a := testAdapter(t)
	helper(a, "invalid")
	ctx, c := context.WithTimeout(context.Background(), 3*time.Second)
	defer c()
	a.readStream(ctx)
	if a.Snapshot().Current != nil || a.Snapshot().Health.State == providers.Healthy {
		t.Fatal("invalid response treated as healthy")
	}
}
func TestBridgeProcess(t *testing.T) {
	switch os.Getenv("MONITOR_MEDIA_HELPER") {
	case "valid":
		fmt.Println(`{"current":{"title":"real helper","state":"playing"}}`)
	case "invalid":
		fmt.Println(`{}`)
	case "error":
		fmt.Println(`{"current":null,"error_code":"music_symbols_missing"}`)
		os.Exit(1)
	case "eof":
		fmt.Println(`{"current":{"title":"gone","state":"playing"}}`)
		os.Exit(0)
	default:
		return
	}
	time.Sleep(time.Minute)
	os.Exit(0)
}

func TestBridgeExitClearsAndPreservesCause(t *testing.T) {
	for _, tc := range []struct{ mode, code string }{{"error", "music_symbols_missing"}, {"eof", "music_bridge_exited"}} {
		t.Run(tc.mode, func(t *testing.T) {
			a := testAdapter(t)
			helper(a, tc.mode)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			a.readStream(ctx)
			s := a.Snapshot()
			if s.Current != nil || s.Health.ErrorCode != tc.code {
				t.Fatalf("%+v", s)
			}
		})
	}
}

func TestConcurrentSnapshots(t *testing.T) {
	a := testAdapter(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 500; i++ {
			a.update(Message{Current: &nowplaying.Current{Title: ptr("song"), State: nowplaying.Playing}})
		}
		cancel()
	}()
	for ctx.Err() == nil {
		s := a.Snapshot()
		if s.Current != nil {
			*s.Current.Title = "local"
		}
	}
	<-done
	if *a.Snapshot().Current.Title != "song" {
		t.Fatal("snapshot mutation affected writer")
	}
}

func TestBinaryPlaybackStates(t *testing.T) {
	a := testAdapter(t)
	for _, state := range []nowplaying.PlaybackState{nowplaying.Playing, nowplaying.NotPlaying} {
		a.update(Message{Current: &nowplaying.Current{Title: ptr("same track"), State: state}})
		s := a.Snapshot()
		if s.Health.State != providers.Healthy || s.Current == nil || s.Current.State != state {
			t.Fatalf("%+v", s)
		}
	}
	for _, state := range []nowplaying.PlaybackState{"", "paused", "stopped", "unknown"} {
		a.update(Message{Current: &nowplaying.Current{State: state}})
		s := a.Snapshot()
		if s.Current != nil || s.Health.ErrorCode != "music_protocol_error" {
			t.Fatalf("invalid state accepted: %+v", s)
		}
	}
}
