package collector

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"

	"esp32-monitor/client/internal/platform"
)

type persisted struct {
	SchemaVersion int    `json:"schema_version"`
	Revision      uint64 `json:"revision"`
	Config        Config `json:"config"`
}

func (m *Manager) persist(c Config, rev uint64) error {
	b, _ := json.Marshal(persisted{SchemaVersion, rev, c})
	if err := platform.WriteFileAtomic(m.opts.StateDirectory, ".collector-*.json", "collector.json", b, 0600); err != nil {
		return errors.New("config_persist_failed")
	}
	return nil
}

// Restore applies the last committed selection using fresh in-process collectors.
func (m *Manager) Restore(ctx context.Context) error {
	m.op.Lock()
	defer m.op.Unlock()
	if m.closed {
		return errClosed
	}
	if m.Snapshot().Revision != 0 {
		return errors.New("collector_already_restored")
	}
	path := filepath.Join(m.opts.StateDirectory, "collector.json")
	st, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		// Registration can start an Agent before its first sampling config exists.
		// Synchronize the startup directory without enabling sample collection.
		if m.v2 != nil {
			p := m.profileV2(m.snapshot.Config)
			if err := validateV2Profile(p); err != nil {
				return err
			}
			m.mu.Lock()
			m.enqueueV2ProfileLocked(m.snapshot.Config, m.snapshot.Revision)
			m.mu.Unlock()
		}
		return nil
	}
	if err != nil || !st.Mode().IsRegular() || st.Size() > maxBody || (runtime.GOOS != "windows" && st.Mode().Perm() != 0600) {
		return errors.New("invalid_persisted_collector_config")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var p persisted
	if json.Unmarshal(b, &p) != nil || p.SchemaVersion != SchemaVersion || p.Revision == 0 {
		return errors.New("invalid_persisted_collector_config")
	}
	m.mu.Lock()
	m.snapshot.Revision = p.Revision - 1
	m.mu.Unlock()
	_, err = m.apply(ctx, p.Config)
	if err != nil {
		// Keep the committed model visible for repair even when a selected
		// object/dependency vanished while the Agent was stopped.
		m.mu.Lock()
		m.snapshot.Revision = p.Revision
		m.snapshot.Config = p.Config
		m.snapshot.State = "error"
		m.snapshot.ErrorCode = err.Error()
		m.mu.Unlock()
	}
	return err
}
