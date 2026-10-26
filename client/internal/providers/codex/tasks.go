package codex

import (
	"encoding/json"
	"esp32-monitor/client/internal/providers"
	"sort"
	"sync"
	"time"
)

// TaskTracker is a reducer for a future verified desktop event bridge. Calling it
// proves only that events were parsed; it does not establish desktop coverage.
// Stream liveness must be signalled by a bridge heartbeat, never by process presence.
type TaskTracker struct {
	mu        sync.Mutex
	tasks     map[string]Task
	live      time.Time
	connected bool
}

func NewTaskTracker() *TaskTracker { return &TaskTracker{tasks: map[string]Task{}} }
func (t *TaskTracker) Heartbeat(now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.live = now
	t.connected = true
}
func (t *TaskTracker) Disconnect() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.connected = false
	t.tasks = map[string]Task{}
}

// Reset clears previous-session state before a bridge applies its authoritative snapshot.
func (t *TaskTracker) Reset(now time.Time, tasks []Task) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.tasks = map[string]Task{}
	for _, task := range tasks {
		if len(t.tasks) < 128 {
			t.tasks[task.ID] = task
		}
	}
	t.live = now
	t.connected = true
}
func (t *TaskTracker) Apply(n Notification, now time.Time) bool {
	var p struct {
		ThreadID string `json:"threadId"`
		Status   struct {
			Type  string   `json:"type"`
			Flags []string `json:"activeFlags"`
		} `json:"status"`
		Turn struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"turn"`
	}
	if json.Unmarshal(n.Params, &p) != nil || p.ThreadID == "" {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	old := t.tasks[p.ThreadID]
	task := Task{ID: p.ThreadID, TurnID: old.TurnID, State: Unknown, ObservedAt: now}
	switch n.Method {
	case "turn/started":
		if p.Turn.ID == "" || p.Turn.Status != "inProgress" {
			return false
		}
		task.State = Running
		task.TurnID = p.Turn.ID
	case "turn/completed":
		if p.Turn.ID == "" || (old.TurnID != "" && old.TurnID != p.Turn.ID) {
			return false
		}
		task.TurnID = p.Turn.ID
		switch p.Turn.Status {
		case "completed":
			task.State = Completed
		case "failed":
			task.State = Failed
		case "interrupted":
			task.State = Cancelled
		default:
			return false
		}
	case "thread/status/changed":
		switch p.Status.Type {
		case "active":
			task.State = Running
			for _, flag := range p.Status.Flags {
				if flag == "waitingOnUserInput" {
					task.State = WaitingInput
				}
			}
			for _, flag := range p.Status.Flags {
				if flag == "waitingOnApproval" {
					task.State = WaitingApproval
				}
			}
		case "idle":
			// Preserve a real terminal event for the requested 60s notification window.
			if terminal(old.State) && now.Sub(old.ObservedAt) < time.Minute {
				return true
			}
			task.State = Idle
		case "systemError":
			task.State = Failed
		case "notLoaded":
			task.State = Unknown
		default:
			return false
		}
	default:
		return false
	}
	if _, ok := t.tasks[task.ID]; !ok && len(t.tasks) >= 128 {
		var oldest Task
		for _, v := range t.tasks {
			if oldest.ID == "" || v.ObservedAt.Before(oldest.ObservedAt) {
				oldest = v
			}
		}
		delete(t.tasks, oldest.ID)
	}
	t.tasks[task.ID] = task
	return true
}
func terminal(s TaskState) bool { return s == Completed || s == Failed || s == Cancelled }
func (t *TaskTracker) Snapshot(now time.Time) Tasks {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := Tasks{Scope: "bridge_events_only", State: Unknown, Health: providers.Health{State: providers.Degraded, ObservedAt: now, ErrorCode: "desktop_bridge_unverified"}, Items: []Task{}}
	if !t.connected || now.Sub(t.live) > 15*time.Second {
		out.Health.ErrorCode = "event_stream_stale"
		return out
	}
	active := 0
	out.State = Idle
	for _, v := range t.tasks {
		if terminal(v.State) && now.Sub(v.ObservedAt) >= time.Minute {
			delete(t.tasks, v.ID)
			continue
		}
		out.Items = append(out.Items, v)
		if v.State == Running || v.State == WaitingInput || v.State == WaitingApproval {
			active++
		}
	}
	sort.Slice(out.Items, func(i, j int) bool { return out.Items[i].ID < out.Items[j].ID })
	// Waiting and failure remain visible even if other threads are running.
	priority := map[TaskState]int{Idle: 0, Completed: 1, Cancelled: 2, Unknown: 3, Running: 4, Failed: 5, WaitingInput: 6, WaitingApproval: 7}
	for _, v := range out.Items {
		if priority[v.State] > priority[out.State] {
			out.State = v.State
		}
	}
	out.ActiveCount = &active
	return out
}
