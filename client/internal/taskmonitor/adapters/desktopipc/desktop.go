// Internal IPC protocol reference: JustinTervala/monitor (MIT).
// Attribution and license: client/third_party/monitor/LICENSE.
package desktopipc

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"esp32-monitor/client/internal/taskmonitor"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"time"

	"esp32-monitor/client/internal/providers"
)

const desktopStreamVersion = 11
const maxDesktopFrame = 16 << 20

var desktopID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,127}$`)

type desktopState struct {
	owner      string
	revision   int64
	projection map[string]any
	task       taskmonitor.Task
	observed   time.Time
	code       string
}

// Observer is an opt-in, passive follower of internal desktop IPC v11.
// It never starts/resumes turns, handles approvals, or claims task ownership.
// Without an explicit thread selection it observes only the latest local thread.
type Observer struct {
	mu          sync.Mutex
	writeMu     sync.Mutex
	states      map[string]*desktopState
	threadIDs   []string
	selectedID  string
	resyncTimes map[string]time.Time
	home        string
	path        string
	auto        bool
	now         func() time.Time
	ready       chan struct{}
	readyOnce   sync.Once
	running     bool
	connected   bool
	code        string
	success     *time.Time
}

func New(home string, ids []string, now func() time.Time) *Observer {
	if now == nil {
		now = time.Now
	}
	if home == "" {
		home = os.Getenv("CODEX_HOME")
		if home == "" {
			userHome, _ := os.UserHomeDir()
			home = filepath.Join(userHome, ".codex")
		}
	}
	path, code := desktopEndpoint(home)
	d := &Observer{states: map[string]*desktopState{}, resyncTimes: map[string]time.Time{}, home: home, path: path, auto: len(ids) == 0, now: now, ready: make(chan struct{})}
	d.code = code
	if len(ids) > 128 {
		d.code = "desktop_task_limit_exceeded"
	}
	for _, id := range ids {
		if !desktopID.MatchString(id) {
			d.code = "invalid_codex_thread_id"
			continue
		}
		d.states[id] = &desktopState{task: taskmonitor.Task{ID: id, State: taskmonitor.Unknown}}
		if d.selectedID == "" {
			d.selectedID = id
		}
	}
	for id := range d.states {
		d.threadIDs = append(d.threadIDs, id)
	}
	sort.Strings(d.threadIDs)
	return d
}
func (d *Observer) WaitReady(ctx context.Context) {
	select {
	case <-d.ready:
	case <-ctx.Done():
	}
}
func (d *Observer) signalReady() { d.readyOnce.Do(func() { close(d.ready) }) }
func (d *Observer) Run(ctx context.Context) {
	d.mu.Lock()
	if d.running {
		d.mu.Unlock()
		return
	}
	d.running = true
	bad := d.code != ""
	d.mu.Unlock()
	defer func() { d.mu.Lock(); d.running = false; d.mu.Unlock() }()
	if bad {
		d.signalReady()
		return
	}
	failures := 0
	for ctx.Err() == nil {
		var conn net.Conn
		var err error
		if d.auto {
			var id string
			if id, err = discoverThread(d.home); err == nil {
				d.selectThread(id)
				conn, err = connectDesktop(ctx, d.path)
			}
		} else {
			conn, err = connectDesktop(ctx, d.path)
		}
		if err == nil {
			err = d.serve(ctx, conn)
			conn.Close()
		}
		d.mu.Lock()
		wasConnected := d.connected
		d.connected = false
		d.code = safeDesktopError(err)
		for id := range d.states {
			d.states[id] = &desktopState{task: taskmonitor.Task{ID: id, State: taskmonitor.Unknown}, code: d.code}
		}
		d.mu.Unlock()
		d.signalReady()
		if ctx.Err() != nil {
			return
		}
		if wasConnected {
			failures = 0
		}
		delay := time.Second * time.Duration(1<<min(failures, 4))
		if d.auto && err != nil && (err.Error() == "desktop_task_not_found" || err.Error() == "desktop_session_index_unavailable") {
			delay = 2 * time.Second
		} else {
			failures++
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
func safeDesktopError(err error) string {
	if err == nil {
		return "desktop_disconnected"
	}
	switch err.Error() {
	case "desktop_platform_unsupported", "desktop_socket_unavailable", "desktop_socket_permission_denied", "desktop_registration_rejected", "desktop_frame_invalid", "desktop_stream_version_unsupported", "desktop_task_not_found", "desktop_session_index_unavailable":
		return err.Error()
	}
	return "desktop_disconnected"
}
func readDesktopFrame(reader io.Reader) ([]byte, error) {
	var header [4]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return nil, err
	}
	size := binary.LittleEndian.Uint32(header[:])
	if size == 0 || size > maxDesktopFrame {
		return nil, errors.New("desktop_frame_invalid")
	}
	body := make([]byte, int(size))
	if _, err := io.ReadFull(reader, body); err != nil {
		return nil, err
	}
	return body, nil
}
func (d *Observer) send(conn net.Conn, value any) error {
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	buffer := make([]byte, 4+len(body))
	binary.LittleEndian.PutUint32(buffer, uint32(len(body)))
	copy(buffer[4:], body)
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	_ = conn.SetWriteDeadline(time.Now().Add(time.Second))
	for len(buffer) > 0 {
		n, err := conn.Write(buffer)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		buffer = buffer[n:]
	}
	return nil
}
func (d *Observer) follow(conn net.Conn, id string, on bool) error {
	return d.send(conn, map[string]any{"type": "broadcast", "method": "thread-stream-following-changed", "version": 1, "params": map[string]any{"conversationId": id, "hostId": "local", "following": on}})
}
func (d *Observer) ids() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string{}, d.threadIDs...)
}
func (d *Observer) currentID() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.selectedID
}
func (d *Observer) selectThread(id string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.selectedID == id {
		if _, ok := d.states[id]; ok {
			return false
		}
	}
	d.states = map[string]*desktopState{id: {task: taskmonitor.Task{ID: id, State: taskmonitor.Unknown}}}
	d.threadIDs = []string{id}
	d.selectedID = id
	d.resyncTimes = map[string]time.Time{}
	d.code = ""
	d.success = nil
	return true
}
func (d *Observer) serve(ctx context.Context, conn net.Conn) error {
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	requestID := hex.EncodeToString(nonce)
	if err := d.send(conn, map[string]any{"type": "request", "requestId": requestID, "method": "initialize", "version": 0, "params": map[string]any{"clientType": "esp32-monitor-observer"}}); err != nil {
		return err
	}
	_ = conn.SetReadDeadline(time.Now().Add(8 * time.Second))
	registered := false
	refreshDone := make(chan struct{})
	refreshCtx, cancel := context.WithCancel(ctx)
	defer func() { cancel(); <-refreshDone }()
	startRefresh := make(chan struct{})
	go func() {
		defer close(refreshDone)
		select {
		case <-refreshCtx.Done():
			return
		case <-startRefresh:
		}
		followTicker := time.NewTicker(10 * time.Second)
		defer followTicker.Stop()
		var discoverC <-chan time.Time
		if d.auto {
			discoverTicker := time.NewTicker(2 * time.Second)
			defer discoverTicker.Stop()
			discoverC = discoverTicker.C
		}
		for {
			select {
			case <-refreshCtx.Done():
				return
			case <-followTicker.C:
				for _, id := range d.ids() {
					if d.follow(conn, id, false) != nil || d.follow(conn, id, true) != nil {
						conn.Close()
						return
					}
				}
			case <-discoverC:
				id, err := discoverThread(d.home)
				if err == nil && id != d.currentID() {
					conn.Close()
					return
				}
			}
		}
	}()
	for {
		bytes, err := readDesktopFrame(conn)
		if err != nil {
			return err
		}
		var message struct {
			Type       string          `json:"type"`
			RequestID  string          `json:"requestId"`
			Method     string          `json:"method"`
			Version    int             `json:"version"`
			ResultType string          `json:"resultType"`
			Owner      string          `json:"sourceClientId"`
			Params     json.RawMessage `json:"params"`
		}
		if json.Unmarshal(bytes, &message) != nil {
			return errors.New("desktop_frame_invalid")
		}
		if message.Type == "client-discovery-request" {
			if err := d.send(conn, map[string]any{"type": "client-discovery-response", "requestId": message.RequestID, "response": map[string]bool{"canHandle": false}}); err != nil {
				return err
			}
			continue
		}
		if message.Type == "response" && message.RequestID == requestID {
			if registered || message.ResultType != "success" {
				return errors.New("desktop_registration_rejected")
			}
			registered = true
			d.mu.Lock()
			d.connected = true
			d.code = ""
			d.mu.Unlock()
			for _, id := range d.ids() {
				if err := d.follow(conn, id, true); err != nil {
					return err
				}
			}
			close(startRefresh)
		}
		if registered && message.Type == "broadcast" {
			resync := d.accept(message.Method, message.Version, message.Owner, message.Params)
			if resync != "" {
				if err := d.follow(conn, resync, false); err != nil {
					return err
				}
				if err := d.follow(conn, resync, true); err != nil {
					return err
				}
			}
		}
		if registered {
			_ = conn.SetReadDeadline(time.Now().Add(25 * time.Second))
		}
	}
}
func (d *Observer) accept(method string, version int, owner string, raw json.RawMessage) string {
	var p struct {
		Host     string `json:"hostId"`
		ID       string `json:"conversationId"`
		ThreadID string `json:"threadId"`
		ClientID string `json:"clientId"`
		Status   string `json:"status"`
		Change   struct {
			Type     string          `json:"type"`
			Revision *int64          `json:"revision"`
			Base     *int64          `json:"baseRevision"`
			State    json.RawMessage `json:"conversationState"`
			Patches  json.RawMessage `json:"patches"`
		} `json:"change"`
	}
	if json.Unmarshal(raw, &p) != nil {
		return ""
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if method == "client-status-changed" && p.Status == "disconnected" {
		for _, s := range d.states {
			if s.owner == p.ClientID {
				s.observed = time.Time{}
				s.projection = nil
				s.task.State = taskmonitor.Unknown
				s.code = "desktop_owner_disconnected"
			}
		}
		return ""
	}
	if method == "ipc-connection-reset" {
		for _, s := range d.states {
			s.observed = time.Time{}
			s.projection = nil
			s.task.State = taskmonitor.Unknown
			s.code = "desktop_state_reset"
		}
		return ""
	}
	if p.ID == "" {
		p.ID = p.ThreadID
	}
	s, ok := d.states[p.ID]
	if !ok || p.Host != "local" {
		return ""
	}
	if method == "thread-stream-following-status-requested" && version == 1 {
		return p.ID
	}
	if method != "thread-stream-state-changed" {
		return ""
	}
	if version != desktopStreamVersion {
		s.observed = time.Time{}
		s.task.State = taskmonitor.Unknown
		s.projection = nil
		s.code = "desktop_stream_version_unsupported"
		d.signalReady()
		return ""
	}
	invalidate := func(code string) string {
		s.observed = time.Time{}
		s.task.State = taskmonitor.Unknown
		s.projection = nil
		s.code = code
		if last := d.resyncTimes[p.ID]; !last.IsZero() && d.now().Sub(last) < 2*time.Second {
			return ""
		}
		d.resyncTimes[p.ID] = d.now()
		return p.ID
	}
	if owner == "" || p.Change.Revision == nil || *p.Change.Revision < 0 {
		return invalidate("desktop_state_invalid")
	}
	if s.owner == owner && s.projection != nil && *p.Change.Revision < s.revision {
		return ""
	}
	now := d.now()
	switch p.Change.Type {
	case "snapshot":
		var state map[string]any
		if json.Unmarshal(p.Change.State, &state) != nil || state["id"] != p.ID {
			return invalidate("desktop_identity_mismatch")
		}
		if s.owner != owner {
			s.task = taskmonitor.Task{ID: p.ID, State: taskmonitor.Unknown}
		}
		s.projection = projectDesktop(state, nil).(map[string]any)
	case "patches":
		if s.owner == owner && s.projection != nil && *p.Change.Revision <= s.revision {
			return ""
		}
		if s.projection == nil || s.owner != owner || p.Change.Base == nil || *p.Change.Base != s.revision {
			return invalidate("desktop_revision_gap")
		}
		next, err := patchDesktop(s.projection, p.Change.Patches, p.ID)
		if err != nil {
			return invalidate("desktop_patch_invalid")
		}
		s.projection = next
	default:
		return invalidate("desktop_state_invalid")
	}
	previous := s.task
	if !terminal(previous.State) {
		previous.ObservedAt = now
	}
	s.task = desktopTask(p.ID, s.projection, previous, now)
	runtime, _ := s.projection["threadRuntimeStatus"].(map[string]any)
	if !terminal(s.task.State) || runtime["type"] == "systemError" {
		s.task.ObservedAt = now
	}
	s.owner = owner
	s.revision = *p.Change.Revision
	s.observed = now
	s.code = ""
	if s.task.State == taskmonitor.Unknown {
		s.code = "desktop_runtime_unknown"
	}
	if s.task.State != taskmonitor.Unknown {
		v := now
		d.success = &v
	}
	all := true
	for _, state := range d.states {
		if state.observed.IsZero() && state.code == "" {
			all = false
		}
	}
	if all {
		d.signalReady()
	}
	return ""
}
func (d *Observer) Snapshot() taskmonitor.Tasks {
	d.mu.Lock()
	defer d.mu.Unlock()
	now := d.now()
	scope := "desktop_selected_threads"
	if d.auto {
		scope = "desktop_latest_thread"
	}
	out := taskmonitor.Tasks{Adapter: d.ID(), Scope: scope, State: taskmonitor.Unknown, Health: providers.Health{State: providers.Starting, ObservedAt: now, SourceVersion: "codex-desktop-ipc-v11"}, Items: []taskmonitor.Task{}}
	if d.success != nil {
		v := *d.success
		out.Health.LastSuccessAt = &v
	}
	code := d.code
	for _, id := range d.threadIDs {
		s := d.states[id]
		task := s.task
		if code == "" && s.code != "" {
			code = s.code
		}
		if !d.connected || s.observed.IsZero() || now.Sub(s.observed) > 15*time.Second {
			task.State = taskmonitor.Unknown
			if code == "" {
				code = s.code
				if code == "" {
					code = "desktop_state_stale"
				}
			}
		}
		if terminal(task.State) && now.Sub(task.ObservedAt) >= time.Minute {
			task.State = taskmonitor.Idle
		}
		out.Items = append(out.Items, task)
		if id == d.selectedID {
			v := task
			out.SelectedTask = &v
		}
	}
	if !d.connected && code == "" {
		code = "desktop_connecting"
	}
	if code != "" {
		out.Health.State = providers.Degraded
		out.Health.ErrorCode = code
		if code == "desktop_stream_version_unsupported" || code == "desktop_platform_unsupported" {
			out.Health.State = providers.Unsupported
		}
		if code == "desktop_socket_permission_denied" {
			out.Health.State = providers.PermissionDenied
		}
	} else {
		out.Health.State = providers.Healthy
	}
	active := 0
	out.State = taskmonitor.Idle
	priority := map[taskmonitor.TaskState]int{taskmonitor.Idle: 0, taskmonitor.Completed: 1, taskmonitor.Cancelled: 2, taskmonitor.Unknown: 3, taskmonitor.Running: 4, taskmonitor.Failed: 5, taskmonitor.WaitingInput: 6, taskmonitor.WaitingApproval: 7}
	unknown := false
	for _, task := range out.Items {
		if task.State == taskmonitor.Unknown {
			unknown = true
		}
		if task.State == taskmonitor.Running || task.State == taskmonitor.WaitingInput || task.State == taskmonitor.WaitingApproval {
			active++
		}
		if priority[task.State] > priority[out.State] {
			out.State = task.State
		}
	}
	if !unknown && len(out.Items) > 0 {
		out.ActiveCount = &active
	} else {
		if out.State == taskmonitor.Idle {
			out.State = taskmonitor.Unknown
		}
	}
	return out
}

// ID identifies the source implementation independently of its protocol version.
func (*Observer) ID() string { return "desktop-ipc" }

var _ taskmonitor.Adapter = (*Observer)(nil)

func terminal(s taskmonitor.TaskState) bool {
	return s == taskmonitor.Completed || s == taskmonitor.Failed || s == taskmonitor.Cancelled
}
