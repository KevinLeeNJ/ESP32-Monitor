// Package metrics validates the Telegraf JSON serializer's native batch format.
package metrics

import (
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"unicode/utf8"
)

const MaxBody = 256 * 1024
const MaxMetrics = 100

var ErrJSON = errors.New("invalid_json")
var ErrPayload = errors.New("invalid_payload")
var ErrTooLarge = errors.New("payload_too_large")

type Sample struct {
	Measurement string
	TagsJSON    string
	Field       string
	SampledAt   int64
	Value       float64
}
type wireMetric struct {
	Name      *string                    `json:"name"`
	Tags      map[string]json.RawMessage `json:"tags"`
	Fields    map[string]json.RawMessage `json:"fields"`
	Timestamp json.RawMessage            `json:"timestamp"`
}

func bounded(s string, empty bool) bool {
	return utf8.ValidString(s) && len(s) <= 128 && (empty || len(s) > 0)
}
func Parse(body []byte, now int64) ([]Sample, error) {
	if len(body) > MaxBody {
		return nil, ErrTooLarge
	}
	if !utf8.Valid(body) || !json.Valid(body) {
		return nil, ErrJSON
	}
	var batch struct {
		Metrics json.RawMessage `json:"metrics"`
	}
	if err := json.Unmarshal(body, &batch); err != nil {
		return nil, ErrPayload
	}
	var raw []json.RawMessage
	if len(batch.Metrics) == 0 || string(batch.Metrics) == "null" || json.Unmarshal(batch.Metrics, &raw) != nil {
		return nil, ErrPayload
	}
	if len(raw) > MaxMetrics {
		return nil, ErrTooLarge
	}
	out := make([]Sample, 0)
	for _, r := range raw {
		var m wireMetric
		if json.Unmarshal(r, &m) != nil || m.Name == nil || m.Tags == nil || m.Fields == nil {
			return nil, ErrPayload
		}
		if !bounded(*m.Name, false) || len(m.Tags) > 16 || len(m.Fields) > 64 {
			return nil, ErrPayload
		}
		ts, err := strconv.ParseInt(string(m.Timestamp), 10, 64)
		if err != nil || ts < now-86400 || ts > now+30 {
			return nil, ErrPayload
		}
		tagsMap := make(map[string]string, len(m.Tags))
		for k, rawTag := range m.Tags {
			var v string
			if len(rawTag) == 0 || rawTag[0] != '"' || json.Unmarshal(rawTag, &v) != nil {
				return nil, ErrPayload
			}
			if !bounded(k, false) || !bounded(v, true) {
				return nil, ErrPayload
			}
			tagsMap[k] = v
		}
		delete(tagsMap, "host")
		tags, _ := json.Marshal(tagsMap)
		supported := *m.Name == "cpu" || *m.Name == "mem" || *m.Name == "disk" || *m.Name == "system"
		for field, rawValue := range m.Fields {
			if !bounded(field, false) {
				return nil, ErrPayload
			}
			// Strings, bools, nulls and composite fields do not represent numeric samples.
			if len(rawValue) == 0 || !(rawValue[0] == '-' || rawValue[0] >= '0' && rawValue[0] <= '9') {
				continue
			}
			v, err := strconv.ParseFloat(string(rawValue), 64)
			if err != nil || math.IsInf(v, 0) || math.IsNaN(v) {
				return nil, ErrPayload
			}
			if supported {
				out = append(out, Sample{*m.Name, string(tags), field, ts, v})
			}
		}
	}
	if len(out) == 0 {
		return nil, ErrPayload
	}
	return out, nil
}
