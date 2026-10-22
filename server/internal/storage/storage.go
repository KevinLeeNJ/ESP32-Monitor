// Package storage persists accepted batches atomically using a single SQLite connection.
package storage

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"math"
	"net/url"
	"os"
	"path/filepath"

	"esp32-monitor/server/internal/metrics"
	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schema string
var ErrSeriesLimit = errors.New("series_limit_exceeded")
var ErrEmptyBatch = errors.New("empty_batch")

const MaxSeries = 256
const CleanupBatch = 1000
const AverageWindowSeconds int64 = 5 * 60

type Store struct{ db *sql.DB }
type DeviceState struct {
	LastSeen *int64
	Samples  []metrics.Sample
}
type Snapshot map[string]DeviceState

func Open(ctx context.Context, path string, ids []string, now int64) (*Store, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(filepath.Dir(abs), 0700); err != nil {
		return nil, err
	}
	if st, e := os.Lstat(abs); e == nil && !st.Mode().IsRegular() {
		return nil, errors.New("database must be a regular local file")
	}
	f, err := os.OpenFile(abs, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	err = f.Chmod(0600)
	f.Close()
	if err != nil {
		return nil, err
	}
	u := url.URL{Scheme: "file", Path: abs}
	q := url.Values{}
	for _, p := range []string{"journal_mode(WAL)", "foreign_keys(1)", "busy_timeout(5000)"} {
		q.Add("_pragma", p)
	}
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	s := &Store{db}
	if err = s.initialize(ctx, ids, now); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}
func (s *Store) initialize(ctx context.Context, ids []string, now int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var version int
	if err = tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	switch version {
	case 0:
		if _, err = tx.ExecContext(ctx, schema); err != nil {
			return err
		}
	case 1:
	default:
		return errors.New("unsupported database schema version")
	}
	for _, id := range ids {
		if _, err = tx.ExecContext(ctx, "INSERT INTO devices(device_id,created_at) VALUES(?,?) ON CONFLICT(device_id) DO NOTHING", id, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (s *Store) Close() error { return s.db.Close() }
func (s *Store) Ingest(ctx context.Context, id string, samples []metrics.Sample, receivedAt int64) error {
	if len(samples) == 0 {
		return ErrEmptyBatch
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Reserve the write lock before reading cardinality. This is safe across processes too.
	res, err := tx.ExecContext(ctx, "UPDATE devices SET last_seen_at=? WHERE device_id=?", receivedAt, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.New("device is not registered")
	}
	rows, err := tx.QueryContext(ctx, "SELECT DISTINCT measurement,tags_json FROM metric_latest WHERE device_id=?", id)
	if err != nil {
		return err
	}
	type key struct{ m, t string }
	series := map[key]bool{}
	for rows.Next() {
		var k key
		if err = rows.Scan(&k.m, &k.t); err != nil {
			rows.Close()
			return err
		}
		series[k] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, v := range samples {
		series[key{v.Measurement, v.TagsJSON}] = true
	}
	if len(series) > MaxSeries {
		return ErrSeriesLimit
	}
	history, err := tx.PrepareContext(ctx, `INSERT INTO metric_history(device_id,measurement,tags_json,field,sampled_at,received_at,value) VALUES(?,?,?,?,?,?,?) ON CONFLICT DO NOTHING`)
	if err != nil {
		return err
	}
	defer history.Close()
	latest, err := tx.PrepareContext(ctx, `INSERT INTO metric_latest(device_id,measurement,tags_json,field,sampled_at,received_at,value) VALUES(?,?,?,?,?,?,?) ON CONFLICT(device_id,measurement,tags_json,field) DO UPDATE SET sampled_at=excluded.sampled_at,received_at=excluded.received_at,value=excluded.value WHERE excluded.sampled_at>metric_latest.sampled_at`)
	if err != nil {
		return err
	}
	defer latest.Close()
	for _, v := range samples {
		args := []any{id, v.Measurement, v.TagsJSON, v.Field, v.SampledAt, receivedAt, v.Value}
		r, e := history.ExecContext(ctx, args...)
		if e != nil {
			return e
		}
		inserted, e := r.RowsAffected()
		if e != nil {
			return e
		}
		if inserted > 0 {
			if _, e = latest.ExecContext(ctx, args...); e != nil {
				return e
			}
		}
	}
	return tx.Commit()
}

// Snapshot uses one read transaction so last_seen and latest describe the same commit.
func (s *Store) Snapshot(ctx context.Context) (Snapshot, error) {
	return s.snapshot(ctx, nil)
}

// SummarySnapshot replaces CPU/MEM values with per-series rolling averages.
// Sample timestamps still come from latest so freshness and source ambiguity
// are evaluated independently of the averaging window.
func (s *Store) SummarySnapshot(ctx context.Context, now int64) (Snapshot, error) {
	return s.snapshot(ctx, &now)
}

func (s *Store) snapshot(ctx context.Context, now *int64) (Snapshot, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	result := Snapshot{}
	rows, err := tx.QueryContext(ctx, "SELECT device_id,last_seen_at FROM devices")
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id string
		var seen sql.NullInt64
		if err = rows.Scan(&id, &seen); err != nil {
			rows.Close()
			return nil, err
		}
		d := DeviceState{}
		if seen.Valid {
			x := seen.Int64
			d.LastSeen = &x
		}
		result[id] = d
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	query := "SELECT device_id,measurement,tags_json,field,sampled_at,value FROM metric_latest"
	var args []any
	if now != nil {
		query = `SELECT l.device_id,l.measurement,l.tags_json,l.field,l.sampled_at,
		 CASE WHEN (l.measurement='cpu' AND l.field='usage_active')
		        OR (l.measurement='mem' AND l.field='used_percent')
		 THEN (SELECT AVG(h.value) FROM metric_history h
		       WHERE h.device_id=l.device_id AND h.measurement=l.measurement
		         AND h.tags_json=l.tags_json AND h.field=l.field
		         AND h.sampled_at>=? AND h.sampled_at<=?)
		 ELSE l.value END
		 FROM metric_latest l`
		args = []any{*now - AverageWindowSeconds, *now}
	}
	rows, err = tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id string
		var v metrics.Sample
		var value sql.NullFloat64
		if err = rows.Scan(&id, &v.Measurement, &v.TagsJSON, &v.Field, &v.SampledAt, &value); err != nil {
			rows.Close()
			return nil, err
		}
		v.Value = math.NaN()
		if value.Valid {
			v.Value = value.Float64
		}
		d := result[id]
		d.Samples = append(d.Samples, v)
		result[id] = d
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}

// Ready checks both reads and ability to obtain a write lock without changing data.
func (s *Store) Ready(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var n int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM devices").Scan(&n); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "UPDATE devices SET created_at=created_at WHERE 0")
	return err
}

// Cleanup deletes one bounded batch. Call repeatedly until fewer than CleanupBatch rows.
func (s *Store) Cleanup(ctx context.Context, cutoff int64) (int64, error) {
	r, err := s.db.ExecContext(ctx, `DELETE FROM metric_history WHERE rowid IN (SELECT rowid FROM metric_history WHERE sampled_at < ? LIMIT ?)`, cutoff, CleanupBatch)
	if err != nil {
		return 0, err
	}
	return r.RowsAffected()
}
