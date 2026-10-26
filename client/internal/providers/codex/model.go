// Package codex combines account quota with an independent task observation adapter.
package codex

import (
	"encoding/json"
	"esp32-monitor/client/internal/providers"
	"esp32-monitor/client/internal/taskmonitor"
	"math"
	"sort"
)

type Window struct {
	BucketID         string   `json:"bucket_id"`
	WindowMinutes    *int64   `json:"window_minutes"`
	UsedPercent      *float64 `json:"used_percent"`
	RemainingPercent *float64 `json:"remaining_percent"`
	ResetsAt         *int64   `json:"resets_at"`
}
type Quota struct {
	Health  providers.Health `json:"health"`
	Windows []Window         `json:"windows"`
}

// Task types remain aliases for callers of the existing Codex snapshot API.
type TaskState = taskmonitor.TaskState
type Task = taskmonitor.Task
type Tasks = taskmonitor.Tasks

const (
	Unknown         = taskmonitor.Unknown
	Idle            = taskmonitor.Idle
	Running         = taskmonitor.Running
	WaitingInput    = taskmonitor.WaitingInput
	WaitingApproval = taskmonitor.WaitingApproval
	Completed       = taskmonitor.Completed
	Failed          = taskmonitor.Failed
	Cancelled       = taskmonitor.Cancelled
)

type Snapshot struct {
	Quota Quota `json:"quota"`
	Tasks Tasks `json:"tasks"`
}
type wireWindow struct {
	Used    *float64 `json:"usedPercent"`
	Minutes *int64   `json:"windowDurationMins"`
	Reset   *int64   `json:"resetsAt"`
}
type wireBucket struct {
	ID        *string     `json:"limitId"`
	Primary   *wireWindow `json:"primary"`
	Secondary *wireWindow `json:"secondary"`
}
type wireLimits struct {
	Legacy  *wireBucket           `json:"rateLimits"`
	Buckets map[string]wireBucket `json:"rateLimitsByLimitId"`
}

func parseLimits(data json.RawMessage) ([]Window, error) {
	var v wireLimits
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, err
	}
	buckets := v.Buckets
	if len(buckets) == 0 && v.Legacy != nil {
		id := "codex"
		if v.Legacy.ID != nil && *v.Legacy.ID != "" {
			id = *v.Legacy.ID
		}
		buckets = map[string]wireBucket{id: *v.Legacy}
	}
	out := make([]Window, 0)
	keys := make([]string, 0, len(buckets))
	for key := range buckets {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, id := range keys {
		b := buckets[id]
		for _, w := range []*wireWindow{b.Primary, b.Secondary} {
			if w == nil {
				continue
			}
			window := Window{BucketID: id, WindowMinutes: w.Minutes, UsedPercent: w.Used, ResetsAt: w.Reset}
			if w.Used != nil {
				remaining := math.Max(0, math.Min(100, 100-*w.Used))
				window.RemainingPercent = &remaining
			}
			out = append(out, window)
		}
	}
	return out, nil
}
