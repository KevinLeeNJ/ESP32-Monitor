package collector

import (
	"encoding/json"
	"errors"
	"math"
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
	Upload  bool                `json:"upload"`
}
type Config struct {
	IntervalSeconds int         `json:"interval_seconds"`
	Selections      []Selection `json:"selections"`
}

func DefaultConfig() Config { return Config{IntervalSeconds: 10, Selections: []Selection{}} }

type Metric struct {
	Name      string             `json:"name"`
	Tags      map[string]string  `json:"tags"`
	Fields    map[string]float64 `json:"fields"`
	Timestamp int64              `json:"timestamp"`
}
type Batch struct {
	Metrics []Metric `json:"metrics"`
}
type MetricCapability struct {
	ID              string              `json:"id"`
	Measurement     string              `json:"measurement"`
	Field           string              `json:"field"`
	Unit            string              `json:"unit"`
	UploadSupported bool                `json:"upload_supported"`
	UploadReason    string              `json:"upload_reason,omitempty"`
	Objects         []map[string]string `json:"objects"`
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
	SchemaVersion   int        `json:"schema_version"`
	CoreCollector   string     `json:"core_collector"`
	NativeCollector string     `json:"native_collector"`
	OS              string     `json:"os"`
	Arch            string     `json:"arch"`
	ObservedAt      time.Time  `json:"observed_at"`
	Categories      []Category `json:"categories"`
}
type Snapshot struct {
	SchemaVersion  int        `json:"schema_version"`
	Revision       uint64     `json:"revision"`
	State          string     `json:"state"`
	ErrorCode      string     `json:"error_code,omitempty"`
	PID            int        `json:"pid"`
	Config         Config     `json:"config"`
	Preview        Batch      `json:"preview"`
	LastReceivedAt *time.Time `json:"last_received_at"`
	LastUploadAt   *time.Time `json:"last_upload_at"`
	UploadError    string     `json:"upload_error,omitempty"`
	QueueDepth     int        `json:"queue_depth"`
	DroppedMetrics uint64     `json:"dropped_metrics"`
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

// Versioned Client upload contract, deliberately narrower than the Server parser.
// Richer inputs remain local until their complete Server projection is defined.
var uploadFields = map[string][]string{
	"cpu":    {"usage_active", "usage_idle", "usage_user", "usage_system", "usage_nice", "usage_iowait", "usage_irq", "usage_softirq", "usage_steal", "usage_guest", "usage_guest_nice"},
	"mem":    {"total", "available", "used", "free", "used_percent", "available_percent", "cached", "buffered", "active", "inactive", "wired"},
	"disk":   {"total", "free", "used", "used_percent", "inodes_total", "inodes_free", "inodes_used", "inodes_used_percent"},
	"system": {"uptime", "load1", "load5", "load15", "n_cpus", "n_users"},
}

func uploadSupported(name, field string) bool { return slices.Contains(uploadFields[name], field) }
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
func parseBatch(body []byte) (Batch, error) {
	if len(body) > maxBody {
		return Batch{}, errors.New("payload_too_large")
	}
	// Non-numeric Telegraf fields are ignored; their presence is not a zero sample.
	var raw struct {
		Metrics []struct {
			Name      string                     `json:"name"`
			Tags      map[string]string          `json:"tags"`
			Fields    map[string]json.RawMessage `json:"fields"`
			Timestamp int64                      `json:"timestamp"`
		} `json:"metrics"`
	}
	if json.Unmarshal(body, &raw) != nil || raw.Metrics == nil || len(raw.Metrics) > 100 {
		return Batch{}, errors.New("invalid_payload")
	}
	b := Batch{Metrics: []Metric{}}
	for _, r := range raw.Metrics {
		if r.Name == "" || len(r.Name) > 128 || r.Tags == nil || len(r.Tags) > 16 || r.Fields == nil || len(r.Fields) > 64 || r.Timestamp <= 0 {
			return Batch{}, errors.New("invalid_payload")
		}
		delete(r.Tags, "host")
		for k, v := range r.Tags {
			if len(k) == 0 || len(k) > 128 || len(v) > 128 {
				return Batch{}, errors.New("invalid_payload")
			}
		}
		m := Metric{r.Name, r.Tags, map[string]float64{}, r.Timestamp}
		for k, v := range r.Fields {
			if len(k) == 0 || len(k) > 128 {
				return Batch{}, errors.New("invalid_payload")
			}
			if len(v) == 0 || (v[0] != '-' && (v[0] < '0' || v[0] > '9')) {
				continue
			}
			var f float64
			if json.Unmarshal(v, &f) != nil || math.IsNaN(f) || math.IsInf(f, 0) {
				return Batch{}, errors.New("invalid_payload")
			}
			m.Fields[k] = f
		}
		if len(m.Fields) > 0 {
			b.Metrics = append(b.Metrics, m)
		}
	}
	return b, nil
}
func filter(b Batch, c Config, upload bool) Batch {
	out := Batch{Metrics: []Metric{}}
	for _, m := range b.Metrics {
		f := map[string]float64{}
		for _, s := range c.Selections {
			p := strings.Split(s.ID, "/")
			if len(p) != 3 || p[1] != m.Name || !matches(s, m.Tags) || (upload && !s.Upload) {
				continue
			}
			if v, ok := m.Fields[p[2]]; ok {
				f[p[2]] = v
			}
		}
		if len(f) > 0 {
			out.Metrics = append(out.Metrics, Metric{m.Name, m.Tags, f, m.Timestamp})
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
		if s.Upload && !m.UploadSupported {
			return errors.New("metric_upload_unsupported")
		}
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
