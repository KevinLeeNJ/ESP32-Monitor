package transport

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"esp32-monitor/client/internal/companion"
	"esp32-monitor/client/internal/providers"
	"esp32-monitor/client/internal/taskmonitor"
	"esp32-monitor/client/internal/transport/wire"
	"math"
	"sort"
	"time"
)

type ageObservation struct {
	stamp    time.Time
	received time.Time
	initial  int64
}

func (s *session) section(path string, h providers.Health, stale, expire int64, data any) wire.Section {
	state := string(h.State)
	switch state {
	case "disabled", "starting", "healthy", "degraded", "auth_required", "permission_denied", "unsupported", "error":
	default:
		state = "unsupported"
	}
	x := health(state, stale, expire)
	if !h.ObservedAt.IsZero() {
		x.ObservedAt = wire.Ptr(h.ObservedAt.Unix())
	}
	if h.ErrorCode != "" {
		x.ErrorCode = wire.Ptr(wire.Clip(h.ErrorCode, 64))
	}
	if h.SourceVersion != "" {
		x.SourceVersion = wire.Ptr(wire.Clip(h.SourceVersion, 64))
	}
	if h.LastSuccessAt != nil {
		t := *h.LastSuccessAt
		x.LastSuccessAt = wire.Ptr(t.Unix())
		a, ok := s.ages[path]
		if !ok || !a.stamp.Equal(t) {
			a = ageObservation{t, time.Now(), max(int64(0), time.Since(t).Milliseconds())}
			s.ages[path] = a
		}
		age := a.initial + time.Since(a.received).Milliseconds()
		x.AgeMS = &age
		if age < expire && (state == "healthy" || state == "degraded") {
			x.Data = data
		}
		if age >= stale && state == "healthy" {
			x.State = "degraded"
		}
	}
	return x
}
func (s *session) project(b *Backend) wire.Sections {
	result := wire.Sections{GitHub: health("disabled", 86400000, 604800000), Codex: health("disabled", 15000, 15000), Music: health("disabled", 15000, 15000)}
	if b.opts.Providers != nil {
		raw := b.opts.Providers.Snapshot()
		s.projectProviders(raw, &result)
	}
	d := s.proxy.snapshot(s.transport == "serial")
	result.Proxy = aggregate([]wire.Section{d.Time, d.Weather, d.AQI, d.Devices}, d)
	return result
}
func (s *session) projectProviders(raw companion.Snapshot, out *wire.Sections) {
	var gh any
	if c := raw.GitHub.Data; c != nil && len(c.Days) > 0 && len(c.Days) <= 371 {
		levels := ""
		valid := wire.Text(c.Login, 39)
		start, e := time.Parse("2006-01-02", c.Days[0].Date)
		valid = valid && e == nil
		for i, d := range c.Days {
			if d.Count > math.MaxInt32 || c.Total < 0 || c.Total > math.MaxInt32 || d.Date != start.AddDate(0, 0, i).Format("2006-01-02") || d.Level < 0 || d.Level > 4 || d.Count < 0 {
				valid = false
				break
			}
			levels += string(rune('0' + d.Level))
		}
		if valid {
			gh = map[string]any{"login": c.Login, "start_date": c.Days[0].Date, "days": len(c.Days), "levels": levels, "today_count": c.Today, "total_count": c.Total}
		}
	}
	out.GitHub = s.section("github", raw.GitHub.Health, 86400000, 604800000, gh)
	if gh == nil && out.GitHub.State == "healthy" {
		out.GitHub.State = "degraded"
	}
	windows := append([]structWindow{}, convertWindows(raw)...)
	sort.SliceStable(windows, func(i, j int) bool {
		if windows[i].BucketID != windows[j].BucketID {
			return windows[i].BucketID < windows[j].BucketID
		}
		return minutes(windows[i].WindowMinutes) < minutes(windows[j].WindowMinutes)
	})
	truncated := len(windows) > 4
	if truncated {
		windows = windows[:4]
	}
	quota := s.section("codex.quota", raw.Codex.Quota.Health, 180000, 300000, map[string]any{"windows": windows, "truncated": truncated})
	ts := raw.Codex.Tasks
	state := taskState(ts.State)
	var selected any
	var completed *int64
	if ts.SelectedTask != nil {
		t := ts.SelectedTask
		hash := sha256.Sum256([]byte(t.ID))
		selected = map[string]any{"id": hex.EncodeToString(hash[:16]), "state": taskState(t.State), "observed_at": t.ObservedAt.Unix()}
		if t.State == taskmonitor.Completed {
			completed = wire.Ptr(t.ObservedAt.Unix())
		}
		if (state == "completed" || state == "failed" || state == "cancelled") && time.Since(t.ObservedAt) > 60*time.Second {
			state = "unknown"
			selected = nil
		}
	}
	count := ts.ActiveCount
	if count != nil && (*count < 0 || *count > 65535) {
		count = nil
	}
	tasks := s.section("codex.tasks", ts.Health, 15000, 15000, map[string]any{"adapter": wire.Clip(ts.Adapter, 64), "scope": wire.Clip(ts.Scope, 64), "state": state, "active_count": count, "selected_task": selected, "last_completed_at": completed})
	codexData := map[string]any{"quota": quota, "tasks": tasks}
	out.Codex = aggregate([]wire.Section{quota, tasks}, codexData)
	m := raw.Music
	var current any
	if c := m.Current; c != nil {
		title, artist, source := clipPtr(c.Title, 160), clipPtr(c.Artist, 96), clipPtr(c.Source, 64)
		var observed *int64
		if c.PositionObservedAt != nil {
			observed = wire.Ptr(c.PositionObservedAt.Unix())
		}
		state := string(c.State)
		if state != "playing" && state != "not_playing" {
			m.Health.State = providers.Unsupported
			state = "not_playing"
		}
		current = map[string]any{"state": state, "title": title, "artist": artist, "source": source, "duration": nonnegative(c.Duration), "position": nonnegative(c.Position), "position_observed_at": observed}
	}
	if cur, ok := current.(map[string]any); ok && s.lastMusic != nil && sameMusic(cur, s.lastMusic) {
		current = s.lastMusic
	}
	out.Music = s.section("music", m.Health, 15000, 15000, map[string]any{"adapter": wire.Clip(m.Adapter, 64), "coverage": wire.Clip(m.Coverage, 64), "current": current})
}

type structWindow struct {
	BucketID      string   `json:"bucket_id"`
	WindowMinutes *int64   `json:"window_minutes"`
	UsedPercent   *float64 `json:"used_percent"`
	ResetsAt      *int64   `json:"resets_at"`
}

func convertWindows(raw companion.Snapshot) []structWindow {
	out := make([]structWindow, 0, len(raw.Codex.Quota.Windows))
	for _, w := range raw.Codex.Quota.Windows {
		used := w.UsedPercent
		if used != nil && (math.IsNaN(*used) || math.IsInf(*used, 0) || *used < 0 || *used > 100) {
			used = nil
		}
		mins := w.WindowMinutes
		if mins != nil && *mins <= 0 {
			mins = nil
		}
		out = append(out, structWindow{wire.Clip(w.BucketID, 64), mins, used, w.ResetsAt})
	}
	return out
}
func minutes(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}
func clipPtr(p *string, n int) *string {
	if p == nil {
		return nil
	}
	return wire.Ptr(wire.Clip(*p, n))
}
func nonnegative(p *float64) *float64 {
	if p != nil && (!math.IsNaN(*p) && !math.IsInf(*p, 0) && *p >= 0) {
		return p
	}
	return nil
}
func taskState(s taskmonitor.TaskState) string {
	switch s {
	case taskmonitor.Unknown, taskmonitor.Idle, taskmonitor.Running, taskmonitor.WaitingInput, taskmonitor.WaitingApproval, taskmonitor.Completed, taskmonitor.Failed, taskmonitor.Cancelled:
		return string(s)
	}
	return "unknown"
}
func aggregate(children []wire.Section, data any) wire.Section {
	x := health("disabled", 15000, 15000)
	active := false
	failed := false
	for _, c := range children {
		if c.State == "disabled" {
			continue
		}
		if !active {
			x.StaleAfterMS = c.StaleAfterMS
			x.ExpireAfterMS = c.ExpireAfterMS
		}
		active = true
		x.StaleAfterMS = min(x.StaleAfterMS, c.StaleAfterMS)
		x.ExpireAfterMS = min(x.ExpireAfterMS, c.ExpireAfterMS)
		if c.State != "healthy" {
			failed = true
		}
		if c.ObservedAt != nil && (x.ObservedAt == nil || *c.ObservedAt < *x.ObservedAt) {
			x.ObservedAt = c.ObservedAt
		}
		if c.AgeMS != nil && (x.AgeMS == nil || *c.AgeMS > *x.AgeMS) {
			x.AgeMS = c.AgeMS
			x.LastSuccessAt = c.LastSuccessAt
		}
	}
	if active {
		x.State = "healthy"
		if failed {
			x.State = "degraded"
		}
		x.Data = data
	}
	return x
}

// Semantic equality excludes time passage and successful re-observation metadata.
func semantic(x wire.Sections) string {
	data, _ := json.Marshal(x)
	var v any
	_ = json.Unmarshal(data, &v)
	stripMetadata(v)
	data, _ = json.Marshal(v)
	return string(data)
}
func stripMetadata(v any) {
	switch m := v.(type) {
	case map[string]any:
		for _, k := range []string{"observed_at", "last_success_at", "age_ms", "position_observed_at"} {
			delete(m, k)
		}
		if _, ok := m["utc_unix_ms"]; ok {
			delete(m, "utc_unix_ms")
		}
		for _, v := range m {
			stripMetadata(v)
		}
	case []any:
		for _, v := range m {
			stripMetadata(v)
		}
	}
}
func leases(sections wire.Sections, seq uint32) []wire.SourceLease {
	out := []wire.SourceLease{}
	add := func(path string, s wire.Section) {
		if s.State == "healthy" && s.LastSuccessAt != nil && s.ObservedAt != nil && s.AgeMS != nil {
			out = append(out, wire.SourceLease{Path: path, SnapshotSeq: seq, ObservedAt: *s.ObservedAt, LastSuccessAt: *s.LastSuccessAt, AgeMS: *s.AgeMS})
		}
	}
	add("github", sections.GitHub)
	add("music", sections.Music)
	if d, ok := sections.Codex.Data.(map[string]any); ok {
		if q, ok := d["quota"].(wire.Section); ok {
			add("codex.quota", q)
		}
		if t, ok := d["tasks"].(wire.Section); ok {
			add("codex.tasks", t)
		}
	}
	if d, ok := sections.Proxy.Data.(wire.ProxyData); ok {
		add("proxy.weather", d.Weather)
		add("proxy.aqi", d.AQI)
		add("proxy.devices", d.Devices)
	}
	return out
}

func sameMusic(current, anchor map[string]any) bool {
	for _, k := range []string{"state", "title", "artist", "source", "duration"} {
		a, _ := json.Marshal(current[k])
		b, _ := json.Marshal(anchor[k])
		if string(a) != string(b) {
			return false
		}
	}
	p, ok := current["position"].(*float64)
	q, ok2 := anchor["position"].(*float64)
	t, ok3 := current["position_observed_at"].(*int64)
	u, ok4 := anchor["position_observed_at"].(*int64)
	if !ok || !ok2 || !ok3 || !ok4 || p == nil || q == nil || t == nil || u == nil {
		return false
	}
	expected := *q
	if current["state"] == "playing" {
		expected += float64(*t - *u)
	}
	if duration, ok := anchor["duration"].(*float64); ok && duration != nil {
		expected = min(expected, *duration)
	}
	return math.Abs(*p-expected) <= 2
}
