package storage

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"esp32-monitor/server/internal/metrics"
)

const now int64 = 1792454400

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), filepath.Join(t.TempDir(), "monitor.db"), []string{"a", "b"}, now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
func sample(ts int64, v float64) metrics.Sample {
	return metrics.Sample{Measurement: "mem", TagsJSON: `{}`, Field: "used_percent", SampledAt: ts, Value: v}
}
func count(t *testing.T, s *Store, table string) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow("SELECT count(*) FROM " + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
func TestIdempotencyOrderAndIdentity(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	for _, batch := range [][]metrics.Sample{{sample(now, 42)}, {sample(now, 999)}, {sample(now-10, 30)}, {sample(now+1, 44)}} {
		if err := s.Ingest(ctx, "a", batch, now+20); err != nil {
			t.Fatal(err)
		}
	}
	state, err := s.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(state["a"].Samples) != 1 || state["a"].Samples[0].Value != 44 || *state["a"].LastSeen != now+20 || state["b"].LastSeen != nil {
		t.Fatal(state)
	}
	if count(t, s, "metric_history") != 3 || count(t, s, "metric_latest") != 1 {
		t.Fatal("bad dedupe")
	}
	var v float64
	var received int64
	if err = s.db.QueryRow("SELECT value,received_at FROM metric_history WHERE sampled_at=?", now).Scan(&v, &received); err != nil || v != 42 || received != now+20 {
		t.Fatal(v, received, err)
	}
	if err = s.Ingest(ctx, "unknown", []metrics.Sample{sample(now, 1)}, now); err == nil {
		t.Fatal("unknown device accepted")
	}
	if err = s.Ingest(ctx, "a", nil, now); !errors.Is(err, ErrEmptyBatch) {
		t.Fatal(err)
	}
}
func TestTransactionFailureRollsBackEverything(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	if err := s.Ingest(ctx, "a", []metrics.Sample{sample(now, 42)}, now); err != nil {
		t.Fatal(err)
	}
	_, err := s.db.Exec(`CREATE TRIGGER fail_history BEFORE INSERT ON metric_history WHEN NEW.field='fail' BEGIN SELECT RAISE(ABORT,'injected failure'); END`)
	if err != nil {
		t.Fatal(err)
	}
	fail := sample(now+1, 9)
	fail.Field = "fail"
	if err = s.Ingest(ctx, "a", []metrics.Sample{sample(now+1, 99), fail}, now+20); err == nil {
		t.Fatal("expected failure")
	}
	state, err := s.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if *state["a"].LastSeen != now || state["a"].Samples[0].Value != 42 || count(t, s, "metric_history") != 1 {
		t.Fatal("partial commit", state)
	}
}
func TestCardinalityAndRollback(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	samples := make([]metrics.Sample, MaxSeries)
	for i := range samples {
		samples[i] = sample(now, 1)
		samples[i].TagsJSON = fmt.Sprintf(`{"path":"/%d"}`, i)
	}
	if err := s.Ingest(ctx, "a", samples, now); err != nil {
		t.Fatal(err)
	}
	next := sample(now+1, 2)
	next.TagsJSON = `{"path":"/new"}`
	if err := s.Ingest(ctx, "a", []metrics.Sample{next}, now+40); !errors.Is(err, ErrSeriesLimit) {
		t.Fatal(err)
	}
	state, err := s.Snapshot(ctx)
	if err != nil || len(state["a"].Samples) != 256 || *state["a"].LastSeen != now {
		t.Fatal("cardinality rollback", err)
	}
	// The same series can add another numeric field without using a new series slot.
	next = samples[0]
	next.Field = "other"
	if err = s.Ingest(ctx, "a", []metrics.Sample{next}, now+1); err != nil {
		t.Fatal(err)
	}
}
func TestCleanupBoundedAndKeepsLatest(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	samples := make([]metrics.Sample, CleanupBatch+1)
	for i := range samples {
		samples[i] = sample(now-86401-int64(i), 1)
	}
	if err := s.Ingest(ctx, "a", samples, now); err != nil {
		t.Fatal(err)
	}
	if err := s.Ingest(ctx, "a", []metrics.Sample{sample(now-86400, 2)}, now); err != nil {
		t.Fatal(err)
	}
	n, err := s.Cleanup(ctx, now-86400)
	if err != nil || n != CleanupBatch {
		t.Fatal(n, err)
	}
	n, err = s.Cleanup(ctx, now-86400)
	if err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if count(t, s, "metric_history") != 1 || count(t, s, "metric_latest") != 1 {
		t.Fatal("cleanup removed latest or cutoff")
	}
}
func TestReopenAndPragmas(t *testing.T) {
	path := filepath.Join(t.TempDir(), "monitor.db")
	ctx := context.Background()
	s, err := Open(ctx, path, []string{"a"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Ingest(ctx, "a", []metrics.Sample{sample(now, 42)}, now); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(ctx, path, []string{"a", "b"}, now+1)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	state, err := s.Snapshot(ctx)
	if err != nil || *state["a"].LastSeen != now || state["b"].LastSeen != nil {
		t.Fatal(err, state)
	}
	var journal string
	var fk, busy, version int
	for query, dest := range map[string]any{"PRAGMA journal_mode": &journal, "PRAGMA foreign_keys": &fk, "PRAGMA busy_timeout": &busy, "PRAGMA user_version": &version} {
		if err = s.db.QueryRow(query).Scan(dest); err != nil {
			t.Fatal(err)
		}
	}
	if journal != "wal" || fk != 1 || busy != 5000 || version != 1 {
		t.Fatal(journal, fk, busy, version)
	}
	if err = s.Ready(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("PRAGMA query_only=ON"); err != nil {
		t.Fatal(err)
	}
	if s.Ready(ctx) == nil {
		t.Fatal("readonly DB ready")
	}
}
func TestConcurrentIngestAndCancelledContext(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := s.Ingest(ctx, "a", []metrics.Sample{sample(now+int64(i), float64(i))}, now); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	state, err := s.Snapshot(ctx)
	if err != nil || state["a"].Samples[0].Value != 19 || count(t, s, "metric_history") != 20 {
		t.Fatal(state, err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if s.Ingest(cancelled, "a", []metrics.Sample{sample(now+30, 30)}, now) == nil {
		t.Fatal("cancelled write accepted")
	}
	s.Close()
	if _, err = s.Snapshot(ctx); err == nil {
		t.Fatal("closed store returned successful snapshot")
	}
	if s.Ready(ctx) == nil {
		t.Fatal("closed store ready")
	}
}
