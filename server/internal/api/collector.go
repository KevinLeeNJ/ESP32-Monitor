package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	v2 "esp32-monitor/server/internal/uploadv2"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (h *Handler) collect(w http.ResponseWriter, r *http.Request) error {
	token := bearer(r)
	if token == "" {
		return errf(401, "unauthorized")
	}
	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Reserve write transaction before loading state, including across Server processes.
	if _, err = tx.ExecContext(r.Context(), "UPDATE v2_devices SET id=id WHERE token_hash=?", digest(token)); err != nil {
		return err
	}
	d, err := loadDevice(r.Context(), tx, "token_hash", digest(token))
	var ae *apiError
	if errors.As(err, &ae) {
		return errf(401, "unauthorized")
	}
	if err != nil {
		return err
	}
	if !h.allow("device:"+d.ID, time.Second, 20) {
		w.Header().Set("Retry-After", "1")
		return errf(429, "rate_limited")
	}
	if r.URL.Path == "/v2/collector/profile" {
		return h.profile(w, r, tx, d)
	}
	if r.Method != "POST" {
		return errf(405, "method_not_allowed")
	}
	var batch v2.SampleBatch
	raw, err := decodeBody(w, r, &batch)
	if err != nil {
		return err
	}
	if !batchIDPattern.MatchString(batch.ID) || batch.Revision == 0 || batch.Revision > 9007199254740991 || len(batch.Samples) == 0 || len(batch.Samples) > 30 {
		return errf(422, "invalid_batch")
	}
	var hash string
	err = tx.QueryRowContext(r.Context(), "SELECT hash FROM v2_batches WHERE device_id=? AND batch_id=?", d.ID, batch.ID).Scan(&hash)
	if err == nil {
		if hash != digest(string(raw)) {
			return errf(409, "batch_id_conflict")
		}
		w.WriteHeader(204)
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if d.Profile == nil {
		return errf(409, "profile_required")
	}
	if batch.Revision != d.Revision {
		return errf(409, "profile_outdated")
	}
	newest := int64(0)
	// All writes remain in a single transaction: a bad final sample rolls back every earlier one.
	for i := range batch.Samples {
		sm := &batch.Samples[i]
		if err = validateSample(d, sm, h.now().Unix()); err != nil {
			return err
		}
		if err = validateSampleShape(sm); err != nil {
			return err
		}
		for key, v := range flattenSample(sm, d.Revision) {
			var old []byte
			e := tx.QueryRowContext(r.Context(), "SELECT value FROM v2_values WHERE device_id=? AND metric=? AND sampled_at=?", d.ID, key, v.Time).Scan(&old)
			if e != nil && !errors.Is(e, sql.ErrNoRows) {
				return e
			}
			if e == nil && !bytes.Equal(old, v.Value) {
				return errf(409, "sample_time_conflict")
			}
			if errors.Is(e, sql.ErrNoRows) {
				if _, err = tx.ExecContext(r.Context(), "INSERT INTO v2_values(device_id,metric,sampled_at,value,revision,received_at) VALUES(?,?,?,?,?,?)", d.ID, key, v.Time, []byte(v.Value), d.Revision, h.now().Unix()); err != nil {
					return err
				}
			}
			current, exists := d.Latest[key]
			if !exists || v.Time > current.Time {
				d.Latest[key] = v
			}
		}
		if sm.Timestamp > newest {
			newest = sm.Timestamp
		}
	}
	if newest >= h.now().Unix()-maxStalenessSeconds(d.Profile) {
		d.LastSeen = max(d.LastSeen, newest)
	}
	if _, err = tx.ExecContext(r.Context(), "INSERT INTO v2_batches(device_id,batch_id,hash,revision,received_at,body) VALUES(?,?,?,?,?,?)", d.ID, batch.ID, digest(string(raw)), d.Revision, h.now().Unix(), raw); err != nil {
		return err
	}
	if err = saveDevice(r.Context(), tx, d); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	w.WriteHeader(204)
	return nil
}
func (h *Handler) profile(w http.ResponseWriter, r *http.Request, tx *sql.Tx, d *Device) error {
	if r.Method == "GET" {
		if d.Profile == nil {
			return errf(404, "profile_required")
		}
		w.Header().Set("ETag", etag(d.Revision))
		if r.Header.Get("If-None-Match") == etag(d.Revision) {
			w.WriteHeader(304)
		} else {
			writeJSON(w, 200, d.Profile)
		}
		return nil
	}
	var candidate *v2.Profile
	switch r.Method {
	case "PUT":
		var p v2.Profile
		if _, err := decodeBody(w, r, &p); err != nil {
			return err
		}
		candidate = &p
		if d.Profile == nil {
			if r.Header.Get("If-None-Match") != "*" {
				return errf(412, "profile_conflict")
			}
		} else {
			if r.Header.Get("If-Match") != etag(d.Revision) {
				return errf(412, "profile_conflict")
			}
			if !profilesEqual(d.Profile, candidate) {
				return errf(409, "profile_conflict")
			}
		}
	case "PATCH":
		if d.Profile == nil {
			return errf(404, "profile_required")
		}
		if r.Header.Get("If-Match") != etag(d.Revision) {
			return errf(412, "profile_conflict")
		}
		var patch v2.ProfilePatch
		if _, err := decodeBody(w, r, &patch); err != nil {
			return err
		}
		if len(patch) > 2048 {
			return errf(413, "patch_too_large")
		}
		var err error
		candidate, err = applyPatch(d.Profile, patch)
		if err != nil {
			return err
		}
	default:
		return errf(405, "method_not_allowed")
	}
	if err := validateProfile(candidate); err != nil {
		return err
	}
	if err := validateInventory(candidate); err != nil {
		return err
	}
	raw, err := json.Marshal(candidate)
	if err != nil {
		return err
	}
	if len(raw) > maxBody {
		return errf(413, "profile_too_large")
	}
	if d.Profile == nil || !profilesEqual(d.Profile, candidate) {
		if d.Revision >= 9007199254740991 {
			return errf(409, "revision_exhausted")
		}
		d.Revision++
		d.Profile = candidate
		// Prune disabled fields in the same commit as the new profile. Retain observation time for enabled fields.
		for key := range d.Latest {
			if !metricEnabled(candidate, key) {
				delete(d.Latest, key)
			}
		}
		if _, err = tx.ExecContext(r.Context(), "INSERT INTO v2_profiles(device_id,revision,body) VALUES(?,?,?)", d.ID, d.Revision, raw); err != nil {
			return err
		}
		if err = saveDevice(r.Context(), tx, d); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	w.Header().Set("ETag", etag(d.Revision))
	writeJSON(w, 200, map[string]uint64{"r": d.Revision})
	return nil
}

var scalarPaths = map[string]string{
	"c": "cpu.usage", "m": "memory.usage", "u": "system.uptime",
	"cpu/l1": "cpu.load1", "cpu/l5": "cpu.load5", "cpu/l15": "cpu.load15", "cpu/t": "cpu.temperature",
	"mem/u": "memory.used", "mem/a": "memory.app", "mem/w": "memory.wired", "mem/c": "memory.compressed", "mem/st": "memory.swap_total", "mem/su": "memory.swap_used", "mem/p": "memory.pressure",
}
var objectPaths = map[string]map[string]string{
	"cores": {"u": "core.usage"}, "gpu": {"u": "gpu.usage", "t": "gpu.temperature"},
	"disk": {"u": "drive.used", "r": "drive.read_rate", "w": "drive.write_rate"},
	"net":  {"up": "interface.up", "tx": "interface.upload_rate", "rx": "interface.download_rate", "ip": "interface.ipv4"},
	"bat":  {"l": "battery.level", "c": "battery.charging"}, "fan": {"r": "fan.rpm"},
}

func flattenSample(sm *v2.Sample, rev uint64) map[string]latestValue {
	raw, _ := json.Marshal(sm)
	var tree map[string]json.RawMessage
	_ = json.Unmarshal(raw, &tree)
	out := map[string]latestValue{}
	put := func(key string, v json.RawMessage, t int64) { out[key] = latestValue{Value: v, Time: t, Revision: rev} }
	for path, metric := range scalarPaths {
		p := strings.Split(path, "/")
		v := tree[p[0]]
		if len(p) > 1 {
			var group map[string]json.RawMessage
			_ = json.Unmarshal(v, &group)
			v = group[p[1]]
		}
		if len(v) > 0 {
			put(metric, v, sm.Timestamp)
		}
	}
	for group, fields := range objectPaths {
		var list []map[string]json.RawMessage
		_ = json.Unmarshal(tree[group], &list)
		for _, obj := range list {
			suffix := "/" + string(obj["i"])
			for field, metric := range fields {
				if v := obj[field]; len(v) > 0 {
					put(metric+suffix, v, sm.Timestamp)
				}
			}
			if group == "disk" && len(obj["sm"]) > 0 {
				var ob struct {
					Time   int64           `json:"t"`
					Health json.RawMessage `json:"h"`
				}
				_ = json.Unmarshal(obj["sm"], &ob)
				put("drive.smart_health"+suffix, ob.Health, ob.Time)
			}
		}
	}
	if sm.PublicIPv4 != nil {
		v, _ := json.Marshal(sm.PublicIPv4.Address)
		put("network.public_ipv4", v, sm.PublicIPv4.Timestamp)
	}
	return out
}
func metricEnabled(p *v2.Profile, key string) bool {
	parts := strings.Split(key, "/")
	metric := parts[0]
	if implicitMetrics[metric] {
		return true
	}
	sel := selectionsOf(p)
	if len(parts) == 1 {
		return sel[metric] != nil
	}
	id, _ := strconv.ParseUint(parts[1], 10, 16)
	return selectedFor(sel, metric, uint16(id))
}
func (h *Handler) fresh(d *Device, key string, v latestValue) bool {
	ttl := maxStalenessSeconds(d.Profile)
	if strings.HasPrefix(key, "drive.smart_health/") || key == "network.public_ipv4" {
		ttl = 900
	}
	return metricEnabled(d.Profile, key) && v.Time >= h.now().Unix()-ttl && v.Time <= h.now().Unix()+30
}
