package collector

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	v2 "esp32-monitor/client/internal/collector/uploadv2"
	"io"
	"net/http"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
)

func queueBytes(q []queued) int {
	n := 0
	for _, v := range q {
		if v.v2 != nil {
			b, _ := json.Marshal(v.v2)
			n += len(b)
		}
	}
	return n
}

func (m *Manager) enqueueV2ProfileLocked(c Config, revision uint64) {
	p := m.profileV2(c)
	m.queue = append(m.queue, queued{v2: &v2.SampleBatch{ID: randomToken()[:32], Samples: []v2.Sample{}}, profile: &p, revision: revision, at: time.Now()})
	select {
	case m.wake <- struct{}{}:
	default:
	}
}
func (m *Manager) enqueueV2Locked(g *generation, s v2.Sample, at time.Time) {
	if s.PublicIPv4 != nil {
		if g.slow["ip"] == s.PublicIPv4.Timestamp {
			s.PublicIPv4 = nil
		} else {
			g.slow["ip"] = s.PublicIPv4.Timestamp
		}
	}
	for n := len(s.Drives) - 1; n >= 0; n-- {
		d := &s.Drives[n]
		if d.SMART != nil {
			key := "smart:" + strconv.Itoa(int(d.ID))
			if g.slow[key] == d.SMART.Timestamp {
				d.SMART = nil
			} else {
				g.slow[key] = d.SMART.Timestamp
			}
		}
		if d.SMART == nil && d.Used == nil && d.ReadRate == nil && d.WriteRate == nil {
			s.Drives = append(s.Drives[:n], s.Drives[n+1:]...)
		}
	}
	b := &v2.SampleBatch{ID: randomToken()[:32], Samples: []v2.Sample{s}}
	m.queue = append(m.queue, queued{v2: b, profile: g.profile, revision: g.revision, at: at})
	m.trimQueueLocked(at)
	select {
	case m.wake <- struct{}{}:
	default:
	}
}
func (m *Manager) validateV2Selection(c Config, b Batch) error {
	for _, sel := range c.Upload.Optional {
		bind, dynamic := v2Bindings[sel.Metric]
		if !dynamic {
			continue
		}
		found := map[uint16]bool{}
		scalar := false
		for _, v := range b.Metrics {
			if v.Name != bind.source {
				continue
			}
			present := false
			if sel.Metric == "drive.smart_attribute" {
				present = true
				for _, detail := range sel.Detail {
					if _, ok := smartInteger(v, smartFields[detail-1]); !ok {
						present = false
					}
				}
			} else {
				_, present = v.Fields[bind.field]
				if _, ok := v.Exact[bind.field]; ok {
					present = true
				}
			}
			if !present {
				continue
			}
			if bind.kind != "" {
				found[m.v2.object(bind.kind, v)] = true
			} else {
				scalar = true
			}
		}
		if bind.kind == "" && !scalar {
			return errors.New("v2_metric_unavailable")
		}
		for _, id := range sel.Objects {
			if !found[id] {
				return errors.New("v2_metric_object_unavailable")
			}
		}
	}
	return nil
}
func (m *Manager) requestV2(ctx context.Context, method, path string, body []byte, condition string) (int, http.Header, []byte, error) {
	token, e := m.opts.UploadToken(ctx)
	if e != nil || token == "" {
		return 401, nil, nil, errors.New("upload_auth_required")
	}
	r, e := http.NewRequestWithContext(ctx, method, path, bytes.NewReader(body))
	if e != nil {
		return 0, nil, nil, e
	}
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	if condition != "" {
		key := "If-Match"
		if condition == "*" {
			key = "If-None-Match"
		}
		r.Header.Set(key, condition)
	}
	response, e := m.opts.HTTPClient.Do(r)
	if e != nil {
		return 0, nil, nil, e
	}
	defer response.Body.Close()
	data, e := io.ReadAll(io.LimitReader(response.Body, maxBody+1))
	if e != nil || len(data) > maxBody {
		return 0, response.Header, nil, errors.New("v2_response_too_large")
	}
	return response.StatusCode, response.Header, data, nil
}
func revisionHeader(h http.Header) (uint64, error) {
	etag := h.Get("ETag")
	if len(etag) < 3 || etag[0] != '"' || etag[len(etag)-1] != '"' {
		return 0, errors.New("invalid_v2_revision")
	}
	n, e := strconv.ParseUint(etag[1:len(etag)-1], 10, 64)
	if e != nil || n == 0 || n > 9007199254740991 {
		return 0, errors.New("invalid_v2_revision")
	}
	return n, nil
}
func (m *Manager) syncV2(ctx context.Context, want *v2.Profile) (int, http.Header, error) {
	if m.v2.Profile != nil && m.v2.Revision > 0 && reflect.DeepEqual(*m.v2.Profile, *want) {
		return 200, nil, nil
	}
	status, headers, data, e := m.requestV2(ctx, "GET", m.profileURL(), nil, "")
	if e != nil {
		return status, headers, e
	}
	method, condition := "PUT", "*"
	var body []byte
	if status == 200 {
		var remote v2.Profile
		if e = json.Unmarshal(data, &remote); e != nil || remote.Version != 2 {
			return 0, headers, errors.New("invalid_v2_profile")
		}
		revision, e := revisionHeader(headers)
		if e != nil {
			return 0, headers, e
		}
		if reflect.DeepEqual(remote, *want) {
			m.v2.Profile = &remote
			m.v2.Revision = revision
			return 200, headers, m.persistV2()
		}
		body, e = json.Marshal(profileDiff(remote, *want))
		if e != nil {
			return 0, headers, e
		}
		method, condition = "PATCH", strconv.Quote(strconv.FormatUint(revision, 10))
	} else if status == 404 {
		body, e = json.Marshal(want)
	} else {
		return status, headers, errors.New("v2_profile_sync_failed")
	}
	if len(body) > maxBody {
		return 413, nil, errors.New("v2_profile_too_large")
	}
	status, headers, data, e = m.requestV2(ctx, method, m.profileURL(), body, condition)
	if e != nil {
		return status, headers, e
	}
	if status != 200 {
		return status, headers, errors.New("v2_profile_sync_failed")
	}
	var ack struct {
		Revision uint64 `json:"r"`
	}
	if json.Unmarshal(data, &ack) != nil || ack.Revision == 0 || ack.Revision > 9007199254740991 {
		return 0, headers, errors.New("invalid_v2_revision")
	}
	headerRevision, e := revisionHeader(headers)
	if e != nil || headerRevision != ack.Revision {
		return 0, headers, errors.New("invalid_v2_revision")
	}
	m.v2.Profile = ptr(detached(*want))
	m.v2.Revision = ack.Revision
	return 200, headers, m.persistV2()
}
func (m *Manager) deliverV2(q queued) {
	ctx, cancel := context.WithTimeout(m.ctx, 8*time.Second)
	defer cancel()
	status, headers, e := m.syncV2(ctx, q.profile)
	if e != nil {
		if status >= 200 && status < 300 {
			status = 503
		}
		if status == 412 || status == 0 {
			status = 503
		}
		m.finishDelivery(q, status, headers)
		return
	}
	if q.v2.Revision == 0 {
		m.mu.Lock()
		q.v2.Revision = m.v2.Revision
		m.snapshot.ProfileRevision = m.v2.Revision
		m.mu.Unlock()
	}
	if len(q.v2.Samples) == 0 {
		m.finishDelivery(q, 204, nil)
		return
	}
	body, _ := json.Marshal(q.v2)
	if len(body) > maxBody {
		m.finishDelivery(q, 413, nil)
		return
	}
	status, headers, data, e := m.requestV2(ctx, "POST", m.opts.UploadURL, body, "")
	if e != nil && status == 0 {
		m.finishDelivery(q, 0, headers)
		return
	}
	if status == 409 {
		var failure struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(data, &failure)
		if failure.Error == "profile_required" || failure.Error == "profile_outdated" {
			m.v2.Profile = nil
			m.v2.Revision = 0
			_ = m.persistV2()
			m.mu.Lock()
			if len(m.queue) > 0 && m.queue[0].v2 == q.v2 {
				m.queue[0].v2 = &v2.SampleBatch{ID: randomToken()[:32], Samples: q.v2.Samples}
				q = m.queue[0]
			}
			m.mu.Unlock()
			m.finishDelivery(q, 503, headers)
			return
		}
	}
	m.finishDelivery(q, status, headers)
}
func profileDiff(old, next v2.Profile) v2.ProfilePatch {
	patch := v2.ProfilePatch{}
	if old.IntervalSeconds != next.IntervalSeconds {
		patchOp(&patch, "replace", "/interval_seconds", next.IntervalSeconds)
	}
	if !reflect.DeepEqual(old.Enabled, next.Enabled) {
		patchOp(&patch, "replace", "/enabled", next.Enabled)
	}
	a, _ := json.Marshal(old.Inventory)
	b, _ := json.Marshal(next.Inventory)
	var x, y map[string]any
	_ = json.Unmarshal(a, &x)
	_ = json.Unmarshal(b, &y)
	diffInventory(&patch, x, y)
	return patch
}
func patchOp(p *v2.ProfilePatch, op, path string, value any) {
	var raw json.RawMessage
	if op != "remove" {
		raw, _ = json.Marshal(value)
	}
	*p = append(*p, v2.ProfileOperation{Op: op, Path: path, Value: raw})
}
func pointerPath(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "~", "~0"), "/", "~1")
}

// The patch grammar forbids replacing /inventory or the cpu/os information
// groups wholesale. Missing object groups are created leaf by leaf, arrays are
// changed member by member, and scalar fields use replace so an absent value
// can be added without a group-level operation.
var inventoryArrays = map[string]bool{"cores": true, "gpus": true, "drives": true, "volumes": true, "interfaces": true, "batteries": true, "fans": true}

func diffInventory(p *v2.ProfilePatch, old, next map[string]any) {
	keys := []string{}
	for key := range old {
		keys = append(keys, key)
	}
	for key := range next {
		if _, ok := old[key]; !ok {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		x, hasOld := old[key]
		y, hasNext := next[key]
		path := "/inventory/" + pointerPath(key)
		switch {
		case key == "cpu" || key == "os":
			var a, b map[string]any
			if hasOld {
				a, _ = x.(map[string]any)
			}
			if hasNext {
				b, _ = y.(map[string]any)
			}
			diffObjectGroup(p, path, a, b)
		case inventoryArrays[key]:
			var a, b []any
			if hasOld {
				a, _ = x.([]any)
			}
			if hasNext {
				b, _ = y.([]any)
			}
			mergeObjects(p, path, a, b)
		case !hasNext:
			patchOp(p, "remove", path, nil)
		case !hasOld || !reflect.DeepEqual(x, y):
			patchOp(p, "replace", path, y)
		}
	}
}

func diffObjectGroup(p *v2.ProfilePatch, path string, old, next map[string]any) {
	keys := []string{}
	for key := range old {
		keys = append(keys, key)
	}
	for key := range next {
		if _, ok := old[key]; !ok {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		x, hasOld := old[key]
		y, hasNext := next[key]
		child := path + "/" + pointerPath(key)
		switch {
		case !hasNext:
			if hasOld {
				patchOp(p, "remove", child, nil)
			}
		case !hasOld:
			patchOp(p, "add", child, y)
		case !reflect.DeepEqual(x, y):
			patchOp(p, "replace", child, y)
		}
	}
}

// diffValue handles fields inside an inventory object member.
func diffValue(p *v2.ProfilePatch, path string, old, next any) {
	if reflect.DeepEqual(old, next) {
		return
	}
	a, am := old.(map[string]any)
	b, bm := next.(map[string]any)
	if am && bm {
		keys := []string{}
		for key := range a {
			keys = append(keys, key)
		}
		for key := range b {
			if _, ok := a[key]; !ok {
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)
		for _, key := range keys {
			x, ax := a[key]
			y, by := b[key]
			child := path + "/" + pointerPath(key)
			switch {
			case !by:
				patchOp(p, "remove", child, nil)
			case !ax:
				patchOp(p, "add", child, y)
			default:
				diffValue(p, child, x, y)
			}
		}
		return
	}
	aa, as := old.([]any)
	bb, bs := next.([]any)
	if as && bs && strings.HasPrefix(path, "/inventory/") && objectArray(aa) && objectArray(bb) {
		mergeObjects(p, path, aa, bb)
		return
	}
	patchOp(p, "replace", path, next)
}

func mergeObjects(p *v2.ProfilePatch, path string, aa, bb []any) {
	wanted := map[any]bool{}
	for _, x := range bb {
		wanted[x.(map[string]any)["id"]] = true
	}
	kept := append([]any{}, aa...)
	for n := len(kept) - 1; n >= 0; n-- {
		if !wanted[kept[n].(map[string]any)["id"]] {
			patchOp(p, "remove", path+"/"+strconv.Itoa(n), nil)
			kept = append(kept[:n], kept[n+1:]...)
		}
	}
	for _, v := range bb {
		id := v.(map[string]any)["id"]
		found := false
		for n, x := range kept {
			if x.(map[string]any)["id"] == id {
				diffValue(p, path+"/"+strconv.Itoa(n), x, v)
				found = true
				break
			}
		}
		if !found {
			patchOp(p, "add", path+"/-", v)
		}
	}
}
func objectArray(a []any) bool {
	for _, v := range a {
		m, ok := v.(map[string]any)
		if !ok || m["id"] == nil {
			return false
		}
	}
	return true
}
