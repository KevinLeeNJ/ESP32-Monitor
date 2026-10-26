// Package taskmonitor defines the common, source-independent task observation contract.
package taskmonitor

import (
	"esp32-monitor/client/internal/providers"
	"time"
)

type TaskState string

const (
	Unknown         TaskState = "unknown"
	Idle            TaskState = "idle"
	Running         TaskState = "running"
	WaitingInput    TaskState = "waiting_input"
	WaitingApproval TaskState = "waiting_approval"
	Completed       TaskState = "completed"
	Failed          TaskState = "failed"
	Cancelled       TaskState = "cancelled"
)

type Task struct {
	ID         string    `json:"id"`
	TurnID     string    `json:"turn_id,omitempty"`
	State      TaskState `json:"state"`
	ObservedAt time.Time `json:"observed_at"`
}
type Tasks struct {
	Adapter string           `json:"adapter,omitempty"`
	Health  providers.Health `json:"health"`
	// Scope is never inferred from process existence or shared account credentials.
	Scope        string    `json:"scope"`
	State        TaskState `json:"state"`
	ActiveCount  *int      `json:"active_count"`
	Items        []Task    `json:"items"`
	SelectedTask *Task     `json:"selected_task,omitempty"`
}
