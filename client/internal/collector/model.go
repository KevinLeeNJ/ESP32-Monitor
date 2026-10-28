package collector

import (
	"encoding/json"
	"errors"
	"esp32-monitor/client/internal/collector/uploadv2"
	"slices"
	"strings"
	"time"
)

const SchemaVersion = 1
const maxBody = 256 * 1024

type Selection struct {
	ID string `json:"id"`
	// Empty Objects selects all observed objects; otherwise tags must match exactly.
	Objects []map[string]string `json:"objects,omitempty"`
}
type Config struct {
	IntervalSeconds int                    `json:"interval_seconds"`
	Selections      []Selection            `json:"selections"`
	Upload          *uploadv2.UploadConfig `json:"upload,omitempty"`
}

func DefaultConfig() Config { return Config{IntervalSeconds: 10, Selections: []Selection{}} }

type Metric struct {
	Name      string             `json:"name"`
	Tags      map[string]string  `json:"tags"`
	Fields    map[string]float64 `json:"fields"`
	Timestamp int64              `json:"timestamp"`
	Exact     map[string]string  `json:"exact,omitempty"`
}
type Batch struct {
	Metrics []Metric `json:"metrics"`
}
type MetricCapability struct {
	ID          string              `json:"id"`
	Measurement string              `json:"measurement"`
	Field       string              `json:"field"`
	Unit        string              `json:"unit"`
	Objects     []map[string]string `json:"objects"`
}
type Capability struct {
	ID        string             `json:"id"`
	Collector string             `json:"collector"`
	State     string             `json:"state"`
	Reason    string             `json:"reason,omitempty"`
	Metrics   []MetricCapability `json:"metrics"`
}
type Category struct {
	ID      string       `json:"id"`
	Name    string       `json:"name"`
	Sources []Capability `json:"sources"`
}
type Capabilities struct {
	SchemaVersion   int             `json:"schema_version"`
	CoreCollector   string          `json:"core_collector"`
	NativeCollector string          `json:"native_collector"`
	OS              string          `json:"os"`
	Arch            string          `json:"arch"`
	ObservedAt      time.Time       `json:"observed_at"`
	Categories      []Category      `json:"categories"`
	V2              *V2Capabilities `json:"v2,omitempty"`
}
type Snapshot struct {
	SchemaVersion   int              `json:"schema_version"`
	Revision        uint64           `json:"revision"`
	State           string           `json:"state"`
	ErrorCode       string           `json:"error_code,omitempty"`
	PID             int              `json:"pid"`
	Config          Config           `json:"config"`
	Preview         Batch            `json:"preview"`
	LastReceivedAt  *time.Time       `json:"last_received_at"`
	LastUploadAt    *time.Time       `json:"last_upload_at"`
	UploadError     string           `json:"upload_error,omitempty"`
	QueueDepth      int              `json:"queue_depth"`
	DroppedMetrics  uint64           `json:"dropped_metrics"`
	V2Sample        *uploadv2.Sample `json:"v2_sample,omitempty"`
	ProfileRevision uint64           `json:"profile_revision,omitempty"`
}

func detached[T any](v T) T {
	b, _ := json.Marshal(v)
	var out T
	_ = json.Unmarshal(b, &out)
	return out
}
func metricID(plugin, measurement, field string) string {
	return plugin + "/" + measurement + "/" + field
}
func sameTags(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || v != w {
			return false
		}
	}
	return true
}
func matches(s Selection, tags map[string]string) bool {
	if len(s.Objects) == 0 {
		return true
	}
	for _, o := range s.Objects {
		if sameTags(o, tags) {
			return true
		}
	}
	return false
}

func unit(measurement, field string) string {
	if measurement == "processes" || measurement == "netstat" {
		return "count"
	}
	if measurement == "diskio" && strings.HasSuffix(field, "time") {
		return "milliseconds"
	}
	if measurement == "swap" && (field == "in" || field == "out") {
		return "bytes"
	}
	switch {
	case strings.HasSuffix(field, "_bytes_per_second"):
		return "bytes_per_second"
	case strings.HasSuffix(field, "_celsius"):
		return "celsius"
	case strings.HasSuffix(field, "_rpm"):
		return "rpm"
	case strings.HasSuffix(field, "_hz"):
		return "hertz"
	case strings.HasSuffix(field, "_cores") || field == "pressure_level" || field == "charging":
		return "count"
	case strings.Contains(field, "percent") || strings.HasPrefix(field, "usage_"):
		return "percent"
	case strings.HasPrefix(field, "inodes_") || strings.HasPrefix(field, "packets_") || strings.HasPrefix(field, "err_") || strings.HasPrefix(field, "drop_") || field == "reads" || field == "writes" || field == "n_cpus" || field == "n_users":
		return "count"
	case field == "uptime":
		return "seconds"
	case strings.Contains(field, "bytes") || slices.Contains([]string{"total", "used", "available", "free", "cached", "buffered", "wired", "active", "inactive"}, field):
		return "bytes"
	default:
		return "native"
	}
}
func filter(b Batch, c Config) Batch {
	out := Batch{Metrics: []Metric{}}
	for _, m := range b.Metrics {
		f := map[string]float64{}
		exact := map[string]string{}
		for _, s := range c.Selections {
			p := strings.Split(s.ID, "/")
			if len(p) != 3 || p[1] != m.Name || !matches(s, m.Tags) {
				continue
			}
			if v, ok := m.Fields[p[2]]; ok {
				f[p[2]] = v
			}
			if v, ok := m.Exact[p[2]]; ok {
				exact[p[2]] = v
			}
		}
		if len(f) > 0 || len(exact) > 0 {
			out.Metrics = append(out.Metrics, Metric{Name: m.Name, Tags: m.Tags, Fields: f, Exact: exact, Timestamp: m.Timestamp})
		}
	}
	return out
}
func validate(c Config, caps Capabilities) error {
	if c.IntervalSeconds < 5 || c.IntervalSeconds > 60 || len(c.Selections) > 256 {
		return errors.New("invalid_collector_config")
	}
	available := map[string]MetricCapability{}
	for _, cat := range caps.Categories {
		for _, src := range cat.Sources {
			for _, m := range src.Metrics {
				available[m.ID] = m
			}
		}
	}
	seen := map[string]bool{}
	for _, s := range c.Selections {
		m, ok := available[s.ID]
		if !ok || seen[s.ID] {
			return errors.New("metric_unavailable_or_duplicate")
		}
		seen[s.ID] = true
		if len(s.Objects) > 256 {
			return errors.New("too_many_objects")
		}
		for _, o := range s.Objects {
			found := false
			for _, actual := range m.Objects {
				found = found || sameTags(o, actual)
			}
			if !found {
				return errors.New("metric_object_unavailable")
			}
		}
	}
	return nil
}
