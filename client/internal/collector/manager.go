package collector

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"esp32-monitor/client/internal/collector/uploadv2"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

var errClosed = errors.New("collector_closed")

type Options struct {
	CoreFactory    func() Collector
	NativeFactory  func() Collector
	StateDirectory string
	UploadURL      string
	UploadToken    func(context.Context) (string, error)
	HTTPClient     *http.Client
	ApplyTimeout   time.Duration
}
type Manager struct {
	op         sync.Mutex // serializes scans, configuration changes and shutdown
	mu         sync.Mutex // snapshots/queue never wait for collection
	delivery   sync.Mutex // drain an in-flight upload before committing a new config
	opts       Options
	lock       *os.File
	closed     bool
	caps       Capabilities
	snapshot   Snapshot
	current    *generation
	api        *API
	ctx        context.Context
	cancel     context.CancelFunc
	workerDone chan struct{}
	wake       chan struct{}
	queue      []queued
	fixed      Batch
	v2         *v2State
}
type generation struct {
	config       Config
	revision     uint64
	staged       Batch
	collect      Config
	profile      *uploadv2.Profile
	slow         map[string]int64
	core, native Collector
	cancel       context.CancelFunc
	done         chan struct{}
}

func New(opts Options) (*Manager, error) {
	if opts.StateDirectory == "" {
		return nil, errors.New("collector_state_directory_required")
	}
	if opts.ApplyTimeout == 0 {
		opts.ApplyTimeout = 20 * time.Second
	}
	if err := os.MkdirAll(opts.StateDirectory, 0700); err != nil {
		return nil, err
	}
	st, err := os.Lstat(opts.StateDirectory)
	if err != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 || (runtime.GOOS != "windows" && st.Mode().Perm() != 0700) {
		return nil, errors.New("collector_directory_must_be_private")
	}
	lockPath := filepath.Join(opts.StateDirectory, "collector.lock")
	if st, e := os.Lstat(lockPath); e == nil && !st.Mode().IsRegular() {
		return nil, errors.New("invalid_collector_lock")
	}
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if lockFile(lock) != nil {
		lock.Close()
		return nil, errors.New("collector_already_owned")
	}
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{opts: opts, lock: lock, ctx: ctx, cancel: cancel, workerDone: make(chan struct{}), wake: make(chan struct{}, 1), snapshot: Snapshot{SchemaVersion: SchemaVersion, State: "disabled", Config: DefaultConfig(), Preview: Batch{Metrics: []Metric{}}}}
	if err := m.initUploader(); err != nil {
		cancel()
		unlockFile(lock)
		lock.Close()
		return nil, err
	}
	// One immutable inventory for this Manager lifetime. Scans and candidates
	// get fresh CPU/rate baselines, never fresh fixed metadata probes.
	startup, startupCancel := context.WithTimeout(ctx, opts.ApplyTimeout)
	defer startupCancel()
	for _, factory := range []*func() Collector{&m.opts.CoreFactory, &m.opts.NativeFactory} {
		if *factory == nil {
			var base Collector = &CoreCollector{}
			if factory == &m.opts.NativeFactory {
				base = newNativeCollector()
			}
			if fixed, ok := base.(fixedCollector); ok {
				m.fixed.Metrics = append(m.fixed.Metrics, fixed.Fixed(startup).Metrics...)
				*factory = fixed.Fork
			} else {
				*factory = func() Collector { return base }
			}
		} else if fixed, ok := (*factory)().(fixedCollector); ok {
			m.fixed.Metrics = append(m.fixed.Metrics, fixed.Fixed(startup).Metrics...)
			*factory = fixed.Fork
		}
	}
	if m.opts.UploadURL != "" {
		if err := m.initV2(); err != nil {
			cancel()
			unlockFile(lock)
			lock.Close()
			return nil, err
		}
	}
	go m.uploadLoop()
	return m, nil
}
func (m *Manager) Snapshot() Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := detached(m.snapshot)
	s.QueueDepth = len(m.queue)

	return s
}
func (m *Manager) Capabilities() Capabilities {
	m.mu.Lock()
	defer m.mu.Unlock()
	return detached(m.caps)
}

func (m *Manager) Apply(ctx context.Context, c Config) (Snapshot, error) {
	m.op.Lock()
	defer m.op.Unlock()
	return m.apply(ctx, c)
}
func (m *Manager) apply(ctx context.Context, c Config) (Snapshot, error) {
	if m.closed {
		return m.Snapshot(), errClosed
	}
	c = detached(c)
	var prepareErr error
	c, prepareErr = m.prepareV2(c)
	if prepareErr != nil {
		return m.failure(prepareErr)
	}
	if c.IntervalSeconds < 5 || c.IntervalSeconds > 60 || len(c.Selections) > 256 {
		return m.failure(errors.New("invalid_collector_config"))
	}
	// Empty selections stop collection without probing dependencies.
	if len(c.Selections) == 0 && (c.Upload == nil || !c.Upload.Enabled) {
		return m.disable(ctx, c)
	}
	// Re-probe on every configuration change, including vanished objects or dependencies.
	caps, err := m.discover(ctx)
	if err != nil {
		return m.failure(err)
	}
	if c.Upload != nil && c.Upload.Enabled && caps.V2 != nil {
		bundleAvailableSelections(c.Upload, caps.V2)
		if err = validateDeviceDependencies(c.Upload); err != nil {
			return m.failure(err)
		}
		if err = selectAllAvailableCores(c.Upload, caps.V2); err != nil {
			return m.failure(err)
		}
		c, err = m.prepareV2(c)
		if err != nil {
			return m.failure(err)
		}
	}
	if err = validate(c, caps); err != nil {
		return m.failure(err)
	}
	old := m.Snapshot()
	revision := old.Revision + 1
	if ctx.Err() != nil {
		return m.failure(ctx.Err())
	}
	// Prepare and persist while the previous generation continues collecting.
	candidate, err := m.start(ctx, c, revision)
	if err == nil {
		err = m.persist(c, revision)
	}
	if err != nil {
		return m.failure(err)
	}
	m.stopCurrent()
	m.commit(candidate)

	return m.Snapshot(), nil
}
func (m *Manager) failure(err error) (Snapshot, error) {
	m.mu.Lock()
	m.snapshot.ErrorCode = err.Error()
	if m.current == nil {
		m.snapshot.State = "error"
	}
	m.mu.Unlock()
	return m.Snapshot(), err
}

func (m *Manager) disable(ctx context.Context, c Config) (Snapshot, error) {
	if ctx.Err() != nil {
		return m.failure(ctx.Err())
	}
	revision := m.Snapshot().Revision + 1
	if err := m.persist(c, revision); err != nil {
		return m.failure(err)
	}
	m.stopCurrent()
	m.delivery.Lock()
	m.mu.Lock()
	m.snapshot.DroppedMetrics += uint64(queueMetrics(m.queue))
	m.queue = nil
	m.snapshot.Config = c
	m.snapshot.Revision = revision
	m.snapshot.State = "disabled"
	m.snapshot.ErrorCode = ""
	m.snapshot.UploadError = ""
	m.snapshot.PID = 0
	m.snapshot.Preview = Batch{Metrics: []Metric{}}
	m.snapshot.LastReceivedAt = nil
	m.snapshot.LastUploadAt = nil
	m.snapshot.V2Sample = nil
	if m.v2 != nil {
		m.enqueueV2ProfileLocked(c, revision)
	}
	m.mu.Unlock()
	m.delivery.Unlock()
	return m.Snapshot(), nil
}
func (m *Manager) commit(g *generation) {
	m.delivery.Lock()
	defer m.delivery.Unlock()
	m.mu.Lock()
	m.current = g
	m.snapshot.Config = g.config
	m.snapshot.Revision = g.revision
	m.snapshot.State = "healthy"
	m.snapshot.ErrorCode = ""
	m.snapshot.UploadError = ""
	m.snapshot.PID = os.Getpid()
	m.snapshot.Preview = Batch{Metrics: []Metric{}}
	m.snapshot.LastReceivedAt = nil
	m.snapshot.LastUploadAt = nil
	m.snapshot.V2Sample = nil
	// Pending samples belong to the previous config and must not escape after a switch.
	m.snapshot.DroppedMetrics += uint64(queueMetrics(m.queue))
	m.queue = nil
	if m.v2 != nil && (g.config.Upload == nil || !g.config.Upload.Enabled) {
		m.enqueueV2ProfileLocked(g.config, g.revision)
	}
	m.mu.Unlock()
	m.accept(g, g.staged)
	ctx, cancel := context.WithCancel(m.ctx)
	g.cancel = cancel
	g.done = make(chan struct{})
	go m.collectLoop(ctx, g)
}
func randomToken() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}

// collectConfig keeps every cheap local source running so the GUI can show live
// values for available metrics the user has not selected.
func (m *Manager) collectConfig(c Config) Config {
	return v2PreviewConfig(c)
}

func (m *Manager) start(ctx context.Context, c Config, revision uint64) (*generation, error) {
	ctx, cancel := context.WithTimeout(ctx, m.opts.ApplyTimeout)
	defer cancel()
	g := &generation{config: c, collect: m.collectConfig(c), revision: revision, core: m.opts.CoreFactory(), native: m.opts.NativeFactory(), slow: map[string]int64{}}
	if core, ok := g.core.(*CoreCollector); ok && c.Upload != nil && c.Upload.Enabled {
		core.maxRateGap = time.Duration(c.IntervalSeconds) * 2 * time.Second
	}
	batch, _ := gather(ctx, g.core, g.native, selectedSources(g.collect))
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err := validate(c, capabilitiesFrom(batch, nil)); err != nil {
		return nil, err
	}
	if c.Upload != nil && c.Upload.Enabled {
		if err := m.validateV2Selection(c, batch); err != nil {
			return nil, err
		}
		profile := m.profileV2(c)
		if err := validateV2Profile(profile); err != nil {
			return nil, err
		}
		g.profile = &profile
		if _, err := m.sampleV2(c, batch); err != nil {
			return nil, err
		}
	}
	g.staged = batch
	return g, nil
}
func (m *Manager) collectLoop(ctx context.Context, g *generation) {
	defer close(g.done)
	ticker := time.NewTicker(time.Duration(g.config.IntervalSeconds) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		sampleCtx, cancel := context.WithTimeout(ctx, m.opts.ApplyTimeout)
		batch, reasons := gather(sampleCtx, g.core, g.native, selectedSources(g.collect))
		cancel()
		if ctx.Err() != nil {
			return
		}
		// Preview-only sources may legitimately be unavailable; the collector
		// state tracks the upload selections, not every previewed source.
		upload := map[string]bool{}
		for _, id := range selectedSources(v2CollectConfig(g.config)) {
			upload[id] = true
		}
		for id := range reasons {
			if !upload[id] {
				delete(reasons, id)
			}
		}
		m.mu.Lock()
		if m.current == g {
			m.snapshot.State = "healthy"
			m.snapshot.ErrorCode = ""
			if len(reasons) > 0 || validate(g.config, capabilitiesFrom(batch, reasons)) != nil {
				m.snapshot.State = "error"
				m.snapshot.ErrorCode = "collector_gather_failed"
			}
		}
		m.mu.Unlock()
		m.accept(g, batch)
	}
}
func (m *Manager) accept(g *generation, b Batch) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.current != g {
		return
	}
	now := time.Now()
	if len(b.Metrics) > 0 {
		m.snapshot.LastReceivedAt = &now
	}
	m.snapshot.Preview = filter(b, g.config)
	if g.config.Upload != nil && g.config.Upload.Enabled {
		sample, err := m.sampleV2(g.config, b)
		if err != nil {
			m.snapshot.State = "error"
			m.snapshot.ErrorCode = err.Error()
			return
		}
		m.enqueueV2Locked(g, sample, now)
	}
	// The GUI shows every gathered metric, not only the upload choices;
	// uploaded samples stay restricted to the user's selections.
	if sample, err := m.previewSampleV2(b); err == nil {
		m.snapshot.V2Sample = ptr(detached(sample))
	}
}
func (m *Manager) stopGeneration(g *generation) {
	if g == nil {
		return
	}
	if g.cancel != nil {
		g.cancel()
		<-g.done
	}
}
func (m *Manager) stopCurrent() {
	m.mu.Lock()
	g := m.current
	m.current = nil
	m.snapshot.PID = 0
	m.mu.Unlock()
	m.stopGeneration(g)
}

type persisted struct {
	SchemaVersion int    `json:"schema_version"`
	Revision      uint64 `json:"revision"`
	Config        Config `json:"config"`
}

func (m *Manager) persist(c Config, rev uint64) error {
	b, _ := json.Marshal(persisted{SchemaVersion, rev, c})
	f, err := os.CreateTemp(m.opts.StateDirectory, ".collector-*.json")
	if err != nil {
		return errors.New("config_persist_failed")
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	if err == nil {
		err = f.Close()
	}
	if err == nil {
		err = os.Rename(f.Name(), filepath.Join(m.opts.StateDirectory, "collector.json"))
	}
	if err != nil {
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
func (m *Manager) Close() error {
	m.op.Lock()
	defer m.op.Unlock()
	if m.closed {
		return nil
	}
	m.closed = true
	m.cancel()
	if m.api != nil {
		_ = m.api.Close()
	}
	m.stopCurrent()
	<-m.workerDone
	unlockFile(m.lock)
	return m.lock.Close()
}
