package codex

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestQuotaMultiBucketNullAndClamp(t *testing.T) {
	windows, err := parseLimits(json.RawMessage(`{"rateLimits":{"primary":{"usedPercent":99}},"rateLimitsByLimitId":{"codex":{"primary":{"usedPercent":25,"windowDurationMins":300,"resetsAt":123},"secondary":{"usedPercent":null,"windowDurationMins":10080}},"review":{"primary":{"usedPercent":120},"secondary":{"usedPercent":-10}}}}`))
	if err != nil || len(windows) != 4 {
		t.Fatalf("parse: %v %+v", err, windows)
	}
	if windows[0].BucketID != "codex" || *windows[0].RemainingPercent != 75 || *windows[0].WindowMinutes != 300 || windows[1].RemainingPercent != nil || *windows[2].RemainingPercent != 0 || *windows[3].RemainingPercent != 100 {
		t.Fatalf("incorrect quota: %+v", windows)
	}
	legacy, _ := parseLimits(json.RawMessage(`{"rateLimits":{"primary":{"usedPercent":20,"windowDurationMins":17}}}`))
	if len(legacy) != 1 || *legacy[0].WindowMinutes != 17 {
		t.Fatal("legacy/custom duration lost")
	}
	unknown, _ := parseLimits(json.RawMessage(`{"rateLimits":null}`))
	if len(unknown) != 0 {
		t.Fatal("invented quota")
	}
}
func event(method, payload string) Notification {
	return Notification{method, json.RawMessage(payload)}
}
func TestTaskLifecycleDoesNotInferCompletion(t *testing.T) {
	now := time.Now()
	tracker := NewTaskTracker()
	tracker.Heartbeat(now)
	tracker.Apply(event("thread/status/changed", `{"threadId":"a","status":{"type":"idle"}}`), now)
	if tracker.Snapshot(now).State != Idle {
		t.Fatal("idle inferred completed")
	}
	tracker.Apply(event("turn/started", `{"threadId":"a","turn":{"id":"turn1","status":"inProgress"}}`), now)
	tracker.Apply(event("thread/status/changed", `{"threadId":"a","status":{"type":"active","activeFlags":["waitingOnApproval"]}}`), now)
	if s := tracker.Snapshot(now); s.State != WaitingApproval || *s.ActiveCount != 1 {
		t.Fatal(s)
	}
	tracker.Apply(event("thread/status/changed", `{"threadId":"a","status":{"type":"active","activeFlags":["waitingOnUserInput"]}}`), now)
	if tracker.Snapshot(now).State != WaitingInput {
		t.Fatal("input state lost")
	}
	tracker.Apply(event("turn/completed", `{"threadId":"a","turn":{"id":"turn1","status":"completed"}}`), now)
	tracker.Apply(event("thread/status/changed", `{"threadId":"a","status":{"type":"idle"}}`), now)
	if tracker.Snapshot(now).State != Completed {
		t.Fatal("terminal event lost")
	}
	tracker.Heartbeat(now.Add(61 * time.Second))
	if tracker.Snapshot(now.Add(61*time.Second)).State != Idle {
		t.Fatal("completion not expired")
	}
	tracker.Disconnect()
	if s := tracker.Snapshot(now); s.State != Unknown || s.ActiveCount != nil {
		t.Fatal("disconnect inferred completion")
	}
}
func TestTaskFailureCancellationStalenessAndPrivacy(t *testing.T) {
	for _, tc := range []struct {
		status string
		want   TaskState
	}{{"failed", Failed}, {"interrupted", Cancelled}} {
		t.Run(tc.status, func(t *testing.T) {
			now := time.Now()
			tracker := NewTaskTracker()
			tracker.Heartbeat(now)
			tracker.Apply(event("turn/completed", `{"threadId":"a","turn":{"id":"t","status":"`+tc.status+`","items":[{"text":"private prompt"}]},"cwd":"private directory"}`), now)
			if tracker.Snapshot(now).State != tc.want {
				t.Fatal("terminal status incorrect")
			}
			data, _ := json.Marshal(tracker.Snapshot(now))
			if strings.Contains(string(data), "private") {
				t.Fatal("task data leak")
			}
			if tracker.Snapshot(now.Add(16*time.Second)).State != Unknown {
				t.Fatal("stale stream visible as current")
			}
		})
	}
}
func TestOldTurnAndRestartDoNotOverwrite(t *testing.T) {
	now := time.Now()
	tracker := NewTaskTracker()
	tracker.Heartbeat(now)
	tracker.Apply(event("turn/started", `{"threadId":"a","turn":{"id":"new","status":"inProgress"}}`), now)
	if tracker.Apply(event("turn/completed", `{"threadId":"a","turn":{"id":"old","status":"completed"}}`), now) {
		t.Fatal("old turn accepted")
	}
	tracker.Reset(now, []Task{{ID: "b", State: Running, ObservedAt: now}})
	s := tracker.Snapshot(now)
	if len(s.Items) != 1 || s.Items[0].ID != "b" {
		t.Fatal("restart retained previous session")
	}
}
