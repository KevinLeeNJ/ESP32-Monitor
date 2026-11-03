package diagnostics

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"esp32-monitor/client/internal/collector"
	"esp32-monitor/client/internal/collector/uploadv2"
	"esp32-monitor/client/internal/companion"
	"esp32-monitor/client/internal/nowplaying"
	"esp32-monitor/client/internal/providers"
	"esp32-monitor/client/internal/providers/codex"
	"esp32-monitor/client/internal/providers/github"
)

func TestBuildWithoutApplicationReturnsSchemaOnly(t *testing.T) {
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	report := Build(nil, now)
	if report.SchemaVersion != SchemaVersion || !report.GeneratedAt.Equal(now) || report.PID == 0 {
		t.Fatalf("unexpected report: %+v", report)
	}
	if report.Collector != nil || report.Monitor != nil {
		t.Fatalf("disabled sections should be nil: %+v", report)
	}
	if report.Companion.GitHub.State != "" || report.Companion.Codex.State != "" || report.Companion.Music.State != "" {
		t.Fatalf("unexpected companion state: %+v", report.Companion)
	}
}

func TestCollectorSectionKeepsCountersNotPayloads(t *testing.T) {
	received := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	uploaded := received.Add(time.Second)
	snapshot := collector.Snapshot{
		State:           "running",
		ErrorCode:       "upload_retrying",
		Revision:        7,
		ProfileRevision: 3,
		Config: collector.Config{
			IntervalSeconds: 15,
			Upload: &uploadv2.UploadConfig{
				Enabled:  true,
				Optional: []uploadv2.UploadSelection{{Metric: "gpu.usage"}, {Metric: "drive.used"}},
			},
		},
		QueueDepth:     4,
		DroppedMetrics: 9,
		UploadError:    "server_unavailable",
		LastReceivedAt: &received,
		LastUploadAt:   &uploaded,
		PID:            1234,
	}
	section := collectorSection(snapshot)
	if section.State != "running" || section.IntervalSec != 15 || !section.UploadEnabled ||
		section.SelectedCount != 2 || section.Revision != 7 || section.ProfileRev != 3 ||
		section.QueueDepth != 4 || section.DroppedMetrics != 9 || section.RunningPID != 1234 {
		t.Fatalf("unexpected collector section: %+v", section)
	}
	if section.UploadError != "server_unavailable" || section.LastReceivedAt == nil || section.LastUploadAt == nil {
		t.Fatalf("missing error/time fields: %+v", section)
	}
}

func TestCompanionSectionKeepsOnlyHealth(t *testing.T) {
	healthy := providers.Health{State: providers.Healthy, ErrorCode: ""}
	degraded := providers.Health{State: providers.Degraded, ErrorCode: "rate_limited",
		SourceVersion: "fixture", LastSuccessAt: ptrTime(time.Now())}
	snapshot := companion.Snapshot{
		GitHub: github.Snapshot{Health: healthy},
		Codex: codex.Snapshot{
			Quota: codex.Quota{Health: providers.Health{State: providers.AuthRequired}},
			Tasks: codex.Tasks{Health: degraded},
		},
		Music: nowplaying.Snapshot{Health: providers.Health{State: providers.Unsupported}},
	}
	section := companionSection(snapshot)
	if section.GitHub.State != providers.Healthy || section.Codex.State != providers.AuthRequired ||
		section.Music.State != providers.Unsupported {
		t.Fatalf("unexpected companion section: %+v", section)
	}
	body, err := json.Marshal(section)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{`"data"`, `"tasks"`, `"windows"`, `"current"`, `"title"`, `"artist"`} {
		if strings.Contains(string(body), forbidden) {
			t.Fatalf("report leaked provider payload %q: %s", forbidden, body)
		}
	}
}

func TestReportJSONOmitsPayloadFields(t *testing.T) {
	report := Report{
		SchemaVersion: SchemaVersion,
		GeneratedAt:   time.Now().UTC(),
		PID:           42,
		Companion: CompanionSection{
			GitHub: providers.Health{State: providers.Healthy},
			Codex:  providers.Health{State: providers.Healthy},
			Music:  providers.Health{State: providers.Healthy},
		},
	}
	body, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"preview", "token", "title", "artist", "calendar", "tasks"} {
		if strings.Contains(string(body), forbidden) {
			t.Fatalf("report leaked %q: %s", forbidden, body)
		}
	}
}

func TestCompanionSnapshotWithoutAdaptersIsEmpty(t *testing.T) {
	service := companion.Service{}
	before := time.Now().UTC()
	snapshot := service.Snapshot()
	if snapshot.SchemaVersion != 1 || snapshot.ObservedAt.Before(before) {
		t.Fatalf("unexpected snapshot metadata: %+v", snapshot)
	}
	if snapshot.GitHub.Health.State != "" || snapshot.Codex.Quota.Health.State != "" || snapshot.Music.Health.State != "" {
		t.Fatalf("nil adapters produced state: %+v", snapshot)
	}
}

func ptrTime(value time.Time) *time.Time { return &value }
