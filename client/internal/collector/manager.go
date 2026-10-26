package collector

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

var errClosed = errors.New("collector_closed")

type Options struct {
	Executable     string
	StateDirectory string
	UploadURL      string
	UploadToken    func(context.Context) (string, error)
	HTTPClient     *http.Client
	ApplyTimeout   time.Duration
}
type Manager struct {
	op         sync.Mutex // serializes scans, configuration changes and shutdown
	mu         sync.Mutex // snapshot/receiver/queue never wait for a scan or child process
	delivery   sync.Mutex // drain an in-flight upload before committing a new config
	exe        executable
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
}
type generation struct {
	config    Config
	revision  uint64
	path      string
	server    *http.Server
	process   *process
	ready     chan struct{}
	mu        sync.Mutex
	staged    Batch
	observed  map[string]bool
	committed bool
}

func New(opts Options) (*Manager, error) {
	if opts.StateDirectory == "" {
		return nil, errors.New("collector_state_directory_required")
	}
	if opts.Executable == "" {
		opts.Executable = "telegraf"
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
	m := &Manager{opts: opts, exe: executable{opts.Executable, opts.StateDirectory}, lock: lock, ctx: ctx, cancel: cancel, workerDone: make(chan struct{}), wake: make(chan struct{}, 1), snapshot: Snapshot{SchemaVersion: SchemaVersion, State: "disabled", Config: DefaultConfig(), Preview: Batch{Metrics: []Metric{}}}}
	if err := m.initUploader(); err != nil {
		cancel()
		unlockFile(lock)
		lock.Close()
		return nil, err
	}
	go m.uploadLoop()
	return m, nil
}
func (m *Manager) Snapshot() Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := detached(m.snapshot)
	s.QueueDepth = len(m.queue)
	if m.current != nil && m.current.process != nil {
		select {
		case <-m.current.process.done:
			s.State = "error"
			s.ErrorCode = "telegraf_exited"
			s.PID = 0
		default:
		}
	}
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
	if c.IntervalSeconds < 5 || c.IntervalSeconds > 60 || len(c.Selections) > 256 {
		return m.failure(errors.New("invalid_collector_config"))
	}
	// Stopping must work even if the Telegraf binary/dependencies disappeared.
	if len(c.Selections) == 0 {
		return m.disable(ctx, c)
	}
	// Re-probe on every configuration change, including vanished objects or dependencies.
	caps, err := m.discover(ctx)
	if err != nil {
		return m.failure(err)
	}
	if err = validate(c, caps); err != nil {
		return m.failure(err)
	}
	for _, s := range c.Selections {
		if s.Upload && m.opts.UploadURL == "" {
			return m.failure(errors.New("upload_not_configured"))
		}
	}
	old := m.Snapshot()
	revision := old.Revision + 1
	if ctx.Err() != nil {
		return m.failure(ctx.Err())
	}
	// Discovery has already exercised each input. The candidate must additionally
	// deliver every requested object/field through its actual HTTP output.
	m.stopCurrent()
	m.mu.Lock()
	m.snapshot.State = "applying"
	m.mu.Unlock()
	candidate, err := m.start(ctx, c, revision)
	if err == nil {
		err = m.persist(c, revision)
	}
	if err != nil {
		m.stopGeneration(candidate)
		if old.PID > 0 && len(old.Config.Selections) > 0 {
			rollbackCtx, cancel := context.WithTimeout(m.ctx, m.opts.ApplyTimeout)
			defer cancel()
			restored, restoreErr := m.start(rollbackCtx, old.Config, old.Revision)
			if restoreErr != nil {
				m.stopGeneration(restored)
				m.mu.Lock()
				m.snapshot.State = "error"
				m.snapshot.PID = 0
				m.snapshot.ErrorCode = "rollback_failed"
				m.mu.Unlock()
				return m.Snapshot(), errors.New("rollback_failed")
			}
			m.commit(restored)
		}
		return m.failure(err)
	}
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
	m.mu.Unlock()
	m.delivery.Unlock()
	return m.Snapshot(), nil
}
func (m *Manager) commit(g *generation) {
	m.delivery.Lock()
	defer m.delivery.Unlock()
	g.mu.Lock()
	defer g.mu.Unlock()
	m.mu.Lock()
	m.current = g
	m.snapshot.Config = g.config
	m.snapshot.Revision = g.revision
	m.snapshot.State = "healthy"
	m.snapshot.ErrorCode = ""
	m.snapshot.UploadError = ""
	m.snapshot.PID = g.process.cmd.Process.Pid
	m.snapshot.Preview = Batch{Metrics: []Metric{}}
	m.snapshot.LastReceivedAt = nil
	m.snapshot.LastUploadAt = nil
	// Pending samples belong to the previous config and must not escape after a switch.
	m.snapshot.DroppedMetrics += uint64(queueMetrics(m.queue))
	m.queue = nil
	m.mu.Unlock()
	g.committed = true
	m.accept(g, g.staged)
}
func randomToken() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func (m *Manager) start(ctx context.Context, c Config, revision uint64) (*generation, error) {
	token := randomToken()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, errors.New("collector_listen_failed")
	}
	g := &generation{config: c, revision: revision, ready: make(chan struct{}), observed: map[string]bool{}}
	g.server = &http.Server{ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, MaxHeaderBytes: 8192}
	g.server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/v1/collect" {
			w.WriteHeader(404)
			return
		}
		if r.Header.Get("Origin") != "" || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+token)) != 1 {
			w.WriteHeader(401)
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
		if err != nil {
			w.WriteHeader(413)
			return
		}
		b, err := parseBatch(body)
		if err != nil {
			w.WriteHeader(400)
			return
		}
		b = filter(b, c, false)
		if len(b.Metrics) == 0 {
			w.WriteHeader(400)
			return
		}
		g.mu.Lock()
		defer g.mu.Unlock()
		if g.committed {
			m.accept(g, b)
		} else {
			// Merge first-gather batches until every selected field/object is observed.
			g.staged = mergePreview(g.staged, b)
			for _, sel := range c.Selections {
				for _, sample := range b.Metrics {
					p := strings.Split(sel.ID, "/")
					if sample.Name != p[1] {
						continue
					}
					if _, ok := sample.Fields[p[2]]; !ok {
						continue
					}
					if len(sel.Objects) == 0 {
						g.observed[sel.ID] = true
					} else {
						for _, o := range sel.Objects {
							if sameTags(o, sample.Tags) {
								g.observed[sel.ID+tagKey(o)] = true
							}
						}
					}
				}
			}
			complete := true
			for _, sel := range c.Selections {
				if len(sel.Objects) == 0 {
					complete = complete && g.observed[sel.ID]
				} else {
					for _, o := range sel.Objects {
						complete = complete && g.observed[sel.ID+tagKey(o)]
					}
				}
			}
			if complete {
				select {
				case <-g.ready:
				default:
					close(g.ready)
				}
			}
		}
		w.WriteHeader(204)
	})
	go func() { _ = g.server.Serve(listener) }()
	data, err := renderConfig(c, "http://"+listener.Addr().String()+"/v1/collect", token)
	if err == nil {
		g.path, err = writeTemp(m.exe.dir, data)
	}
	if err != nil {
		g.server.Close()
		return nil, errors.New("config_write_failed")
	}
	g.process, err = m.exe.start(g.path)
	if err != nil {
		return g, err
	}
	timer := time.NewTimer(m.opts.ApplyTimeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return g, ctx.Err()
	case <-m.ctx.Done():
		return g, errClosed
	case <-g.process.done:
		return g, errors.New("telegraf_exited_before_ready")
	case <-timer.C:
		return g, errors.New("telegraf_receive_timeout")
	case <-g.ready:
	}
	select {
	case <-g.process.done:
		return g, errors.New("telegraf_exited_before_ready")
	default:
		return g, nil
	}
}
func mergePreview(old, b Batch) Batch {
	byKey := map[string]Metric{}
	for _, list := range [][]Metric{old.Metrics, b.Metrics} {
		for _, sample := range list {
			key := sample.Name + "\x00" + tagKey(sample.Tags)
			prev, ok := byKey[key]
			if ok && prev.Timestamp == sample.Timestamp {
				for k, v := range sample.Fields {
					prev.Fields[k] = v
				}
				byKey[key] = prev
			} else {
				byKey[key] = sample
			}
		}
	}
	out := Batch{Metrics: []Metric{}}
	for _, sample := range byKey {
		out.Metrics = append(out.Metrics, sample)
	}
	return out
}
func (m *Manager) accept(g *generation, b Batch) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.current != g {
		return
	}
	now := time.Now()
	m.snapshot.LastReceivedAt = &now
	m.snapshot.Preview = mergePreview(m.snapshot.Preview, b)
	upload := filter(b, g.config, true)
	if len(upload.Metrics) > 0 {
		m.enqueueLocked(upload, g.revision, now)
	}
}
func (m *Manager) stopGeneration(g *generation) {
	if g == nil {
		return
	}
	g.process.stop()
	if g.server != nil {
		_ = g.server.Close()
	}
	if g.path != "" {
		_ = os.Remove(g.path)
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

// Restore applies the last committed model using a fresh process and fresh local credential.
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
