// Package diagnostics builds redacted health reports. Provider payloads,
// contribution calendars, task titles, media metadata and credentials must
// never enter a report; only health states, error codes and counters do.
package diagnostics

import (
	"os"
	"time"

	"esp32-monitor/client/internal/app"
	"esp32-monitor/client/internal/collector"
	"esp32-monitor/client/internal/companion"
	"esp32-monitor/client/internal/providers"
)

// SchemaVersion is the report contract version.
const SchemaVersion = 1

// Report is the full redacted diagnostic snapshot returned over IPC.
type Report struct {
	SchemaVersion int               `json:"schema_version"`
	GeneratedAt   time.Time         `json:"generated_at"`
	PID           int               `json:"pid"`
	Collector     *CollectorSection `json:"collector,omitempty"`
	Companion     CompanionSection  `json:"companion"`
	Monitor       *MonitorSection   `json:"monitor,omitempty"`
}

// CollectorSection mirrors manager state without metric payloads.
type CollectorSection struct {
	State          string     `json:"state"`
	ErrorCode      string     `json:"error_code,omitempty"`
	Revision       uint64     `json:"revision"`
	ProfileRev     uint64     `json:"profile_revision"`
	IntervalSec    int        `json:"interval_seconds"`
	UploadEnabled  bool       `json:"upload_enabled"`
	SelectedCount  int        `json:"selected_count"`
	QueueDepth     int        `json:"queue_depth"`
	DroppedMetrics uint64     `json:"dropped_metrics"`
	UploadError    string     `json:"upload_error,omitempty"`
	LastReceivedAt *time.Time `json:"last_received_at,omitempty"`
	LastUploadAt   *time.Time `json:"last_upload_at,omitempty"`
	RunningPID     int        `json:"running_pid"`
}

// CompanionSection carries only provider health, never payloads.
type CompanionSection struct {
	GitHub providers.Health `json:"github"`
	Codex  providers.Health `json:"codex"`
	Music  providers.Health `json:"music"`
}

// MonitorSection summarizes paired Monitor connections.
type MonitorSection struct {
	DeviceCount int `json:"device_count"`
	PairedCount int `json:"paired_count"`
	OnlineCount int `json:"online_count"`
}

// Build assembles the redacted report from live application state.
func Build(application *app.Application, now time.Time) Report {
	report := Report{SchemaVersion: SchemaVersion, GeneratedAt: now, PID: os.Getpid()}
	if application == nil {
		return report
	}
	if application.Collector != nil {
		report.Collector = collectorSection(application.Collector.Snapshot())
	}
	report.Companion = companionSection(application.Companion.Snapshot())
	if application.Monitor != nil {
		report.Monitor = monitorSection(application)
	}
	return report
}

func collectorSection(snapshot collector.Snapshot) *CollectorSection {
	section := &CollectorSection{
		State:          snapshot.State,
		ErrorCode:      snapshot.ErrorCode,
		Revision:       snapshot.Revision,
		ProfileRev:     snapshot.ProfileRevision,
		IntervalSec:    snapshot.Config.IntervalSeconds,
		QueueDepth:     snapshot.QueueDepth,
		DroppedMetrics: snapshot.DroppedMetrics,
		UploadError:    snapshot.UploadError,
		LastReceivedAt: snapshot.LastReceivedAt,
		LastUploadAt:   snapshot.LastUploadAt,
		RunningPID:     snapshot.PID,
	}
	if snapshot.Config.Upload != nil {
		section.UploadEnabled = snapshot.Config.Upload.Enabled
		section.SelectedCount = len(snapshot.Config.Upload.Optional)
	}
	return section
}

func companionSection(snapshot companion.Snapshot) CompanionSection {
	return CompanionSection{GitHub: snapshot.GitHub.Health, Codex: snapshot.Codex.Quota.Health, Music: snapshot.Music.Health}
}

func monitorSection(application *app.Application) *MonitorSection {
	devices, err := application.GetMonitorDevices()
	if err != nil {
		return &MonitorSection{}
	}
	section := &MonitorSection{DeviceCount: len(devices)}
	for _, device := range devices {
		if device.Paired {
			section.PairedCount++
		}
		if device.ConfirmedOnline {
			section.OnlineCount++
		}
	}
	return section
}
