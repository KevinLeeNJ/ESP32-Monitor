// Package providers defines privacy-safe state shared by companion adapters.
package providers

import "time"

type State string

const (
	Disabled         State = "disabled"
	Starting         State = "starting"
	Healthy          State = "healthy"
	Degraded         State = "degraded"
	AuthRequired     State = "auth_required"
	PermissionDenied State = "permission_denied"
	Unsupported      State = "unsupported"
	Error            State = "error"
)

type Health struct {
	State         State      `json:"state"`
	ObservedAt    time.Time  `json:"observed_at"`
	LastSuccessAt *time.Time `json:"last_success_at"`
	ErrorCode     string     `json:"error_code,omitempty"`
	SourceVersion string     `json:"source_version,omitempty"`
}
