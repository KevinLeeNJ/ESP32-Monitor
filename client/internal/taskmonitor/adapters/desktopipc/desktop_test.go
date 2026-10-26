package desktopipc

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"esp32-monitor/client/internal/taskmonitor"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"esp32-monitor/client/internal/providers"
)

func desktopMessage(id, kind, owner string, revision int64, state any, patches any, base int64) json.RawMessage {
	v := map[string]any{"hostId": "local", "conversationId": id, "change": map[string]any{"type": kind, "revision": revision, "baseRevision": base, "conversationState": state, "patches": patches}}
	data, _ := json.Marshal(v)
	return data
}
func desktopFixture(t *testing.T) (*Observer, *time.Time) {
	t.Helper()
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	d := New(t.TempDir(), []string{"a"}, func() time.Time { return now })
	d.connected = true
	return d, &now
}
func rawState(id, runtime, status, turn string) map[string]any {
	return map[string]any{"id": id, "threadRuntimeStatus": map[string]any{"type": runtime, "activeFlags": []any{}}, "turns": []any{map[string]any{"turnId": turn, "status": status, "turnStartedAtMs": float64(1000), "items": []any{map[string]any{"text": "private-prompt"}}}}, "requests": []any{}, "cwd": "private-directory", "title": "private-title"}
}
func snapshot(d *Observer, state any, revision int64) {
	d.accept("thread-stream-state-changed", 11, "owner", desktopMessage("a", "snapshot", "owner", revision, state, nil, 0))
}
func TestDesktopProjectionAndTaskTransitions(t *testing.T) {
	d, now := desktopFixture(t)
	snapshot(d, rawState("a", "active", "inProgress", "t1"), 1)
	s := d.Snapshot()
	if s.State != taskmonitor.Running || s.Health.State != providers.Healthy || *s.ActiveCount != 1 {
		t.Fatal(s)
	}
	bytes, _ := json.Marshal(d.states["a"].projection)
	if strings.Contains(string(bytes), "private") {
		t.Fatal("private state retained")
	}
	patches := []any{map[string]any{"op": "replace", "path": []any{"threadRuntimeStatus", "activeFlags"}, "value": []any{"waitingOnApproval"}}}
	d.accept("thread-stream-state-changed", 11, "owner", desktopMessage("a", "patches", "owner", 2, nil, patches, 1))
	if d.Snapshot().State != taskmonitor.WaitingApproval {
		t.Fatal("approval state not read")
	}
	state := rawState("a", "active", "inProgress", "t1")
	state["requests"] = []any{map[string]any{"method": "item/tool/requestUserInput", "params": map[string]any{"question": "private-question"}}}
	snapshot(d, state, 3)
	if d.Snapshot().State != taskmonitor.WaitingInput {
		t.Fatal("input state not read")
	}
	*now = now.Add(time.Second)
	snapshot(d, rawState("a", "idle", "completed", "t1"), 4)
	if d.Snapshot().State != taskmonitor.Completed {
		t.Fatal("real completion not observed")
	}
	*now = now.Add(40 * time.Second)
	snapshot(d, rawState("a", "idle", "completed", "t1"), 4)
	*now = now.Add(21 * time.Second)
	snapshot(d, rawState("a", "idle", "completed", "t1"), 4)
	if d.Snapshot().State != taskmonitor.Idle {
		t.Fatal("repeated snapshots extended completion retention")
	}
}
func TestDesktopHistoricalTerminalDoesNotBecomeNewCompletion(t *testing.T) {
	for _, status := range []string{"completed", "failed", "interrupted"} {
		d, _ := desktopFixture(t)
		snapshot(d, rawState("a", "idle", status, "historic"), 1)
		if d.Snapshot().State != taskmonitor.Idle {
			t.Fatal("historical terminal replayed as new completion")
		}
	}
	for _, tc := range []struct {
		status string
		want   taskmonitor.TaskState
	}{{"failed", taskmonitor.Failed}, {"interrupted", taskmonitor.Cancelled}} {
		d, _ := desktopFixture(t)
		snapshot(d, rawState("a", "active", "inProgress", "t"), 1)
		snapshot(d, rawState("a", "idle", tc.status, "t"), 2)
		if d.Snapshot().State != tc.want {
			t.Fatal("terminal transition lost", tc.status)
		}
	}
}
func TestDesktopRevisionsOwnerVersionAndDisconnect(t *testing.T) {
	d, now := desktopFixture(t)
	snapshot(d, rawState("a", "active", "inProgress", "t"), 10)
	snapshot(d, rawState("a", "idle", "completed", "t"), 9)
	if d.Snapshot().State != taskmonitor.Running {
		t.Fatal("old snapshot accepted")
	}
	patches := []any{map[string]any{"op": "replace", "path": []any{"threadRuntimeStatus", "type"}, "value": "idle"}}
	resync := d.accept("thread-stream-state-changed", 11, "owner", desktopMessage("a", "patches", "owner", 12, nil, patches, 11))
	if resync != "a" || d.Snapshot().State != taskmonitor.Unknown || d.Snapshot().ActiveCount != nil {
		t.Fatal("revision gap did not invalidate")
	}
	snapshot(d, rawState("a", "active", "inProgress", "t"), 20)
	d.accept("thread-stream-state-changed", 11, "different-owner", desktopMessage("a", "patches", "", 21, nil, patches, 20))
	if d.Snapshot().State != taskmonitor.Unknown {
		t.Fatal("owner changed without snapshot")
	}
	snapshot(d, rawState("a", "active", "inProgress", "t"), 30)
	*now = now.Add(16 * time.Second)
	if d.Snapshot().State != taskmonitor.Unknown {
		t.Fatal("stale runtime still running")
	}
	snapshot(d, rawState("a", "active", "inProgress", "t"), 30)
	d.accept("client-status-changed", 1, "", json.RawMessage(`{"clientId":"owner","status":"disconnected"}`))
	if d.Snapshot().State != taskmonitor.Unknown {
		t.Fatal("owner disconnect retained state")
	}
	snapshot(d, rawState("a", "active", "inProgress", "t"), 31)
	d.accept("thread-stream-state-changed", 12, "owner", desktopMessage("a", "snapshot", "", 32, rawState("a", "active", "inProgress", "t"), nil, 0))
	if s := d.Snapshot(); s.State != taskmonitor.Unknown || s.Health.State != providers.Unsupported {
		t.Fatal("unknown protocol accepted")
	}
}
func TestDesktopPatchesAreAtomicBoundedAndPrivate(t *testing.T) {
	state := projectDesktop(rawState("a", "active", "inProgress", "t"), nil).(map[string]any)
	raw := json.RawMessage(`[{"op":"replace","path":["turns",0,"items",0,"text"],"value":"secret"},{"op":"add","path":["requests",0],"value":{"method":"item/commandExecution/requestApproval","params":{"secret":"credentials"}}}]`)
	next, err := patchDesktop(state, raw, "a")
	if err != nil {
		t.Fatal(err)
	}
	bytes, _ := json.Marshal(next)
	if strings.Contains(string(bytes), "secret") || strings.Contains(string(bytes), "credentials") {
		t.Fatal("private patch retained")
	}
	if desktopTask("a", next, taskmonitor.Task{ObservedAt: time.Now()}).State != taskmonitor.WaitingApproval {
		t.Fatal("request patch lost")
	}
	_, err = patchDesktop(state, json.RawMessage(`[{"op":"replace","path":["threadRuntimeStatus","type"],"value":"idle"},{"op":"remove","path":["turns",999]}]`), "a")
	if err == nil || desktopTask("a", state, taskmonitor.Task{}).State != taskmonitor.Running {
		t.Fatal("invalid batch committed partially")
	}
	if _, err = patchDesktop(state, json.RawMessage(`[{"op":"replace","path":[],"value":{"id":"other"}}]`), "a"); err == nil {
		t.Fatal("root identity changed")
	}
}
func TestDesktopFrameLimitsAndPartialReads(t *testing.T) {
	var frame bytes.Buffer
	body := []byte(`{"type":"test"}`)
	binary.Write(&frame, binary.LittleEndian, uint32(len(body)))
	frame.Write(body)
	got, err := readDesktopFrame(&frame)
	if err != nil || string(got) != string(body) {
		t.Fatal(err)
	}
	for _, n := range []uint32{0, maxDesktopFrame + 1} {
		var b bytes.Buffer
		binary.Write(&b, binary.LittleEndian, n)
		if _, err := readDesktopFrame(&b); err == nil {
			t.Fatal("frame limit bypassed")
		}
	}
	if _, err := readDesktopFrame(strings.NewReader("x")); err == nil {
		t.Fatal("partial header accepted")
	}
}
func writeDesktopFrame(conn io.Writer, v any) error {
	body, _ := json.Marshal(v)
	var frame bytes.Buffer
	binary.Write(&frame, binary.LittleEndian, uint32(len(body)))
	frame.Write(body)
	_, err := conn.Write(frame.Bytes())
	return err
}

// Uses the actual framed socket transport; the fake desktop rejects any operation
// outside passive registration/following/discovery responses.
func TestDesktopSocketRegistrationReadOnlyAndReconnect(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("Unix desktop IPC only")
	}
	// Short path fits macOS sockaddr_un (104 bytes).
	home, err := os.MkdirTemp("/tmp", "esp32-ipc-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(home)
	ipc := filepath.Join(home, "ipc")
	if os.Mkdir(ipc, 0700) != nil {
		t.Fatal("mkdir")
	}
	listener, err := net.Listen("unix", filepath.Join(ipc, "ipc.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	os.Chmod(filepath.Join(ipc, "ipc.sock"), 0600)
	d := New(home, []string{"a"}, time.Now)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	running := make(chan struct{})
	go func() { defer close(running); d.Run(ctx) }()
	var wg sync.WaitGroup
	handled := make(chan struct{}, 2)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range 2 {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			bytes, err := readDesktopFrame(conn)
			if err != nil {
				t.Error(err)
				conn.Close()
				return
			}
			var init struct {
				Type   string `json:"type"`
				Method string `json:"method"`
				ID     string `json:"requestId"`
			}
			json.Unmarshal(bytes, &init)
			if init.Type != "request" || init.Method != "initialize" {
				t.Error("unexpected mutating operation")
			}
			writeDesktopFrame(conn, map[string]any{"type": "response", "requestId": init.ID, "resultType": "success"})
			bytes, err = readDesktopFrame(conn)
			if err != nil {
				t.Error(err)
				conn.Close()
				return
			}
			var follow struct {
				Type   string `json:"type"`
				Method string `json:"method"`
			}
			json.Unmarshal(bytes, &follow)
			if follow.Type != "broadcast" || follow.Method != "thread-stream-following-changed" {
				t.Error("task ownership operation")
			}
			var params any
			json.Unmarshal(desktopMessage("a", "snapshot", "", 1, rawState("a", "active", "inProgress", "t"), nil, 0), &params)
			writeDesktopFrame(conn, map[string]any{"type": "broadcast", "method": "thread-stream-state-changed", "version": 11, "sourceClientId": "owner", "params": params})
			handled <- struct{}{}
			if len(handled) == 1 {
				time.Sleep(50 * time.Millisecond)
			}
			conn.Close()
		}
	}()
	for range 2 {
		select {
		case <-handled:
		case <-time.After(5 * time.Second):
			t.Fatal("did not reconnect")
		}
	}
	cancel()
	listener.Close()
	<-running
	wg.Wait()
	if d.Snapshot().State != taskmonitor.Unknown {
		t.Fatal("cancelled connection retained running state")
	}
}
func TestDesktopSelectionAndSocketPermissions(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("Unix desktop IPC only")
	}
	d := New(t.TempDir(), nil, time.Now)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d.Run(ctx)
	if d.Snapshot().Health.ErrorCode != "desktop_task_selection_required" {
		t.Fatal("missing selection masquerades as idle")
	}
	d = New(t.TempDir(), []string{"../bad"}, time.Now)
	d.Run(ctx)
	if d.Snapshot().Health.ErrorCode != "invalid_codex_thread_id" {
		t.Fatal("invalid id accepted")
	}
	home, err := os.MkdirTemp("/tmp", "esp32-ipc-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(home)
	ipc := filepath.Join(home, "ipc")
	os.Mkdir(ipc, 0700)
	file := filepath.Join(ipc, "ipc.sock")
	listener, err := net.Listen("unix", file)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	os.Chmod(file, 0600)
	if err := validateDesktopSocket(file); err != nil {
		t.Fatal(err)
	}
	os.Chmod(ipc, 0777)
	if err := validateDesktopSocket(file); err == nil {
		t.Fatal("shared-writable directory accepted")
	}
}

func TestDesktopMultiTaskCountsAndSelection(t *testing.T) {
	now := time.Now()
	d := New(t.TempDir(), []string{"b", "a"}, func() time.Time { return now })
	d.connected = true
	snapshot(d, rawState("a", "active", "inProgress", "a-turn"), 1)
	d.accept("thread-stream-state-changed", 11, "owner", desktopMessage("b", "snapshot", "", 1, rawState("b", "active", "inProgress", "b-turn"), nil, 0))
	s := d.Snapshot()
	if s.ActiveCount == nil || *s.ActiveCount != 2 || s.SelectedTask == nil || s.SelectedTask.ID != "b" {
		t.Fatal(s)
	}
	d.accept("thread-stream-state-changed", 11, "owner", desktopMessage("b", "snapshot", "", 2, map[string]any{"id": "b"}, nil, 0))
	s = d.Snapshot()
	if s.Health.State != providers.Degraded || s.ActiveCount != nil || s.SelectedTask.State != taskmonitor.Unknown {
		t.Fatal("unknown member misreported as complete coverage", s)
	}
}

func TestObserverDeclinesOwnershipDiscovery(t *testing.T) {
	d, _ := desktopFixture(t)
	a, b := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer a.Close()
	defer b.Close()
	done := make(chan struct{})
	go func() { defer close(done); _ = d.serve(ctx, a) }()
	raw, err := readDesktopFrame(b)
	if err != nil {
		t.Fatal(err)
	}
	var init struct {
		ID string `json:"requestId"`
	}
	json.Unmarshal(raw, &init)
	writeDesktopFrame(b, map[string]any{"type": "client-discovery-request", "requestId": "discovery"})
	raw, err = readDesktopFrame(b)
	if err != nil {
		t.Fatal(err)
	}
	var decline struct {
		Type     string `json:"type"`
		Response struct {
			CanHandle bool `json:"canHandle"`
		} `json:"response"`
	}
	if json.Unmarshal(raw, &decline) != nil || decline.Type != "client-discovery-response" || decline.Response.CanHandle {
		t.Fatal("observer claimed ownership")
	}
	cancel()
	b.Close()
	<-done
}

func TestDesktopPersistentSystemErrorAndNewTurnObservationTime(t *testing.T) {
	d, now := desktopFixture(t)
	snapshot(d, rawState("a", "systemError", "failed", "t"), 1)
	*now = now.Add(70 * time.Second)
	snapshot(d, rawState("a", "systemError", "failed", "t"), 2)
	if d.Snapshot().State != taskmonitor.Failed {
		t.Fatal("persistent system error expired as idle")
	}
	*now = now.Add(time.Second)
	snapshot(d, rawState("a", "active", "inProgress", "new-turn"), 3)
	s := d.Snapshot()
	if s.State != taskmonitor.Running || !s.Items[0].ObservedAt.Equal(*now) {
		t.Fatal("new turn inherited old terminal timestamp")
	}
}

func TestDesktopUnknownAttentionCannotMasqueradeAsRunning(t *testing.T) {
	d, _ := desktopFixture(t)
	state := rawState("a", "active", "inProgress", "t")
	state["threadRuntimeStatus"] = map[string]any{"type": "active", "activeFlags": []any{"futureFlag"}}
	snapshot(d, state, 1)
	if d.Snapshot().State != taskmonitor.Unknown {
		t.Fatal("unknown active flag mapped to running")
	}
	state = rawState("a", "active", "inProgress", "t")
	state["requests"] = []any{map[string]any{"method": "future/attention"}}
	snapshot(d, state, 2)
	if d.Snapshot().State != taskmonitor.Unknown {
		t.Fatal("unknown attention request mapped to running")
	}
}
