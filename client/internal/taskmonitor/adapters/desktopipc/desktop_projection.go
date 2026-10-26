// Projection design references JustinTervala/monitor (MIT); see third-party notice.
package desktopipc

import (
	"encoding/json"
	"errors"
	"esp32-monitor/client/internal/taskmonitor"
	"strconv"
)

// Desktop IPC is internal. Only these fields survive parsing. Transcript text,
// prompts, paths and tool arguments are never retained in the observer state.
var turnFields = map[string]bool{"id": true, "turnId": true, "status": true, "turnStartedAtMs": true}

func projectDesktop(value any, path []string) any {
	if obj, ok := value.(map[string]any); ok {
		out := map[string]any{}
		for key, v := range obj {
			next := append(append([]string{}, path...), key)
			if relevantPath(next) {
				out[key] = projectDesktop(v, next)
			}
		}
		return out
	}
	if items, ok := value.([]any); ok {
		out := make([]any, len(items))
		for i, v := range items {
			out[i] = projectDesktop(v, append(append([]string{}, path...), strconv.Itoa(i)))
		}
		return out
	}
	return value
}
func index(s string) bool { n, err := strconv.Atoi(s); return err == nil && n >= 0 }
func relevantPath(p []string) bool {
	if len(p) == 0 {
		return true
	}
	switch p[0] {
	case "threadRuntimeStatus":
		return len(p) == 1 || (len(p) == 2 && (p[1] == "type" || p[1] == "activeFlags")) || (len(p) == 3 && p[1] == "activeFlags" && (index(p[2]) || p[2] == "length"))
	case "requests":
		return len(p) == 1 || (len(p) == 2 && (index(p[1]) || p[1] == "length")) || (len(p) == 3 && index(p[1]) && (p[2] == "id" || p[2] == "requestId" || p[2] == "method"))
	case "turns":
		return len(p) == 1 || (len(p) == 2 && (index(p[1]) || p[1] == "length")) || (len(p) == 3 && index(p[1]) && turnFields[p[2]])
	case "turnHistory":
		return len(p) == 1 || (len(p) == 2 && p[1] == "history") || (len(p) >= 3 && p[1] == "history" && p[2] == "entitiesByKey" && (len(p) <= 4 || (len(p) == 5 && turnFields[p[4]])))
	}
	return false
}

var errDesktopPatch = errors.New("desktop_patch_invalid")

// applyPatch implements the bounded map/array subset of Immer patches used by
// the projection. Missing containers or impossible indices force a resnapshot.
func applyPatch(node any, path []string, op string, value any) (any, error) {
	if len(path) == 0 {
		if op == "remove" {
			return nil, errDesktopPatch
		}
		return value, nil
	}
	key := path[0]
	switch obj := node.(type) {
	case map[string]any:
		old, exists := obj[key]
		if len(path) > 1 {
			if !exists {
				return nil, errDesktopPatch
			}
			v, err := applyPatch(old, path[1:], op, value)
			if err != nil {
				return nil, err
			}
			obj[key] = v
			return obj, nil
		}
		if op != "add" && !exists {
			return nil, errDesktopPatch
		}
		if op == "remove" {
			delete(obj, key)
		} else {
			obj[key] = value
		}
		return obj, nil
	case []any:
		if key == "length" && len(path) == 1 && op == "replace" {
			n, ok := value.(float64)
			if !ok || n < 0 || int(n) > len(obj) || float64(int(n)) != n {
				return nil, errDesktopPatch
			}
			return obj[:int(n)], nil
		}
		n, err := strconv.Atoi(key)
		if err != nil || n < 0 || n > len(obj) {
			return nil, errDesktopPatch
		}
		if len(path) > 1 {
			if n == len(obj) {
				return nil, errDesktopPatch
			}
			obj[n], err = applyPatch(obj[n], path[1:], op, value)
			return obj, err
		}
		if op == "add" {
			obj = append(obj, nil)
			copy(obj[n+1:], obj[n:])
			obj[n] = value
			return obj, nil
		}
		if n == len(obj) {
			return nil, errDesktopPatch
		}
		if op == "remove" {
			return append(obj[:n], obj[n+1:]...), nil
		}
		obj[n] = value
		return obj, nil
	}
	return nil, errDesktopPatch
}
func patchDesktop(state map[string]any, raw json.RawMessage, id string) (map[string]any, error) {
	var patches []struct {
		Op    string `json:"op"`
		Path  []any  `json:"path"`
		Value any    `json:"value"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &patches) != nil || len(patches) > 10000 {
		return nil, errDesktopPatch
	}
	// Commit only after the entire patch batch validates.
	bytes, _ := json.Marshal(state)
	var next any
	_ = json.Unmarshal(bytes, &next)
	for _, p := range patches {
		if p.Op != "add" && p.Op != "replace" && p.Op != "remove" {
			return nil, errDesktopPatch
		}
		path := make([]string, len(p.Path))
		for i, key := range p.Path {
			switch k := key.(type) {
			case string:
				path[i] = k
			case float64:
				if k < 0 || float64(int(k)) != k {
					return nil, errDesktopPatch
				}
				path[i] = strconv.Itoa(int(k))
			default:
				return nil, errDesktopPatch
			}
		}
		if len(path) > 8 {
			return nil, errDesktopPatch
		}
		if !relevantPath(path) {
			continue
		}
		if len(path) == 0 {
			obj, ok := p.Value.(map[string]any)
			if !ok || obj["id"] != id {
				return nil, errDesktopPatch
			}
		}
		value := projectDesktop(p.Value, path)
		var err error
		next, err = applyPatch(next, path, p.Op, value)
		if err != nil {
			return nil, err
		}
	}
	result, ok := next.(map[string]any)
	if !ok {
		return nil, errDesktopPatch
	}
	return result, nil
}
func desktopTask(id string, state map[string]any, nowTask taskmonitor.Task) taskmonitor.Task {
	out := taskmonitor.Task{ID: id, State: taskmonitor.Unknown, ObservedAt: nowTask.ObservedAt}
	bytes, _ := json.Marshal(state)
	var v struct {
		Runtime *struct {
			Type  string   `json:"type"`
			Flags []string `json:"activeFlags"`
		} `json:"threadRuntimeStatus"`
		Requests []struct {
			Method string `json:"method"`
		} `json:"requests"`
		Turns   []desktopTurn `json:"turns"`
		History struct {
			History struct {
				Turns map[string]desktopTurn `json:"entitiesByKey"`
			} `json:"history"`
		} `json:"turnHistory"`
	}
	if json.Unmarshal(bytes, &v) != nil || v.Runtime == nil {
		return out
	}
	latest := desktopTurn{}
	for _, turn := range v.Turns {
		if turn.Started >= latest.Started {
			latest = turn
		}
	}
	for _, turn := range v.History.History.Turns {
		if turn.Started > latest.Started {
			latest = turn
		}
	}
	out.TurnID = latest.ID
	if latest.TurnID != "" {
		out.TurnID = latest.TurnID
	}
	switch v.Runtime.Type {
	case "active":
		out.State = taskmonitor.Running
	case "idle":
		out.State = taskmonitor.Idle
	case "systemError":
		out.State = taskmonitor.Failed
	default:
		return out
	}
	if out.State == taskmonitor.Idle && out.TurnID != "" {
		terminalState := taskmonitor.Unknown
		switch latest.Status {
		case "completed":
			terminalState = taskmonitor.Completed
		case "failed":
			terminalState = taskmonitor.Failed
		case "interrupted":
			terminalState = taskmonitor.Cancelled
		}
		// Historical completions seen on first attach are not new completion events.
		if terminalState != taskmonitor.Unknown && nowTask.TurnID == out.TurnID {
			if terminal(nowTask.State) {
				out.State = nowTask.State
				out.ObservedAt = nowTask.ObservedAt
			} else if nowTask.State == taskmonitor.Running || nowTask.State == taskmonitor.WaitingInput || nowTask.State == taskmonitor.WaitingApproval {
				out.State = terminalState
			}
		}
	}
	for _, flag := range v.Runtime.Flags {
		if flag != "waitingOnUserInput" && flag != "waitingOnApproval" {
			return taskmonitor.Task{ID: id, TurnID: out.TurnID, State: taskmonitor.Unknown, ObservedAt: out.ObservedAt}
		}
		if flag == "waitingOnUserInput" {
			out.State = taskmonitor.WaitingInput
		}
	}
	if len(v.Requests) > 0 && out.State != taskmonitor.WaitingInput && out.State != taskmonitor.WaitingApproval {
		out.State = taskmonitor.Unknown
	}
	for _, r := range v.Requests {
		if r.Method == "item/tool/requestUserInput" {
			out.State = taskmonitor.WaitingInput
		}
	}
	for _, flag := range v.Runtime.Flags {
		if flag == "waitingOnApproval" {
			out.State = taskmonitor.WaitingApproval
		}
	}
	for _, r := range v.Requests {
		if r.Method == "item/commandExecution/requestApproval" || r.Method == "item/fileChange/requestApproval" || r.Method == "item/permissions/requestApproval" {
			out.State = taskmonitor.WaitingApproval
		}
	}
	return out
}

type desktopTurn struct {
	ID      string  `json:"id"`
	TurnID  string  `json:"turnId"`
	Status  string  `json:"status"`
	Started float64 `json:"turnStartedAtMs"`
}
