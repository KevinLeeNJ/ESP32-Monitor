package api

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	v2 "esp32-monitor/server/internal/uploadv2"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type latestValue struct {
	Value    json.RawMessage `json:"value"`
	Time     int64           `json:"sampled_at"`
	Revision uint64          `json:"revision"`
}
type Device struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Code        string                 `json:"code,omitempty"`
	CodeExpiry  int64                  `json:"code_expires_at,omitempty"`
	Created     int64                  `json:"created_at"`
	Registered  bool                   `json:"registered"`
	LastSeen    int64                  `json:"last_seen_at,omitempty"`
	Profile     *v2.Profile            `json:"profile,omitempty"`
	Revision    uint64                 `json:"revision"`
	Latest      map[string]latestValue `json:"latest"`
	Panel       bool                   `json:"panel_enabled"`
	DisplayName string                 `json:"display_name"`
	DriveID     uint16                 `json:"disk_id"`
}

func loadDevice(ctx context.Context, tx *sql.Tx, where, value string) (*Device, error) {
	var raw []byte
	// where is always a package-owned column literal.
	err := tx.QueryRowContext(ctx, "SELECT state FROM v2_devices WHERE "+where+"=?", value).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errf(404, "device_not_found")
	}
	if err != nil {
		return nil, err
	}
	var d Device
	if err = json.Unmarshal(raw, &d); err != nil {
		return nil, err
	}
	return &d, nil
}
func saveDevice(ctx context.Context, tx *sql.Tx, d *Device) error {
	raw, err := json.Marshal(d)
	if err != nil {
		return err
	}
	var code any
	if d.Code != "" {
		code = d.Code
	}
	_, err = tx.ExecContext(ctx, "UPDATE v2_devices SET state=?,code=? WHERE id=?", raw, code, d.ID)
	return err
}
func validDeviceName(name string) bool {
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 64 {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func validDisplayName(name string) bool {
	if len(name) < 1 || len(name) > 8 {
		return false
	}
	for _, b := range []byte(name) {
		if b < 33 || b > 126 {
			return false
		}
	}
	return true
}
func (h *Handler) issueCode(ctx context.Context, tx *sql.Tx, d *Device) error {
	for i := 0; i < 32; i++ {
		n, err := rand.Int(rand.Reader, big.NewInt(900000))
		if err != nil {
			return err
		}
		code := fmt.Sprintf("%06d", n.Int64()+100000)
		var count int
		if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM v2_devices WHERE code=?", code).Scan(&count); err != nil {
			return err
		}
		if count == 0 {
			d.Code = code
			d.CodeExpiry = h.now().Add(10 * time.Minute).Unix()
			return saveDevice(ctx, tx, d)
		}
	}
	return errors.New("cannot allocate code")
}
func (h *Handler) deviceView(d *Device) map[string]any {
	// Explicit allowlist: no token verifier or batch bodies cross this boundary.
	status := "expired"
	if d.Registered {
		status = "offline"
		if d.Profile != nil && d.LastSeen != 0 && h.now().Unix()-d.LastSeen <= maxStalenessSeconds(d.Profile) {
			status = "online"
		}
	}
	v := map[string]any{"id": d.ID, "name": d.Name, "registered": d.Registered, "created_at": d.Created, "last_seen_at": d.LastSeen, "status": status, "revision": d.Revision, "panel_enabled": d.Panel, "display_name": d.DisplayName, "disk_id": d.DriveID}
	if d.Code != "" && d.CodeExpiry > h.now().Unix() {
		v["code"] = d.Code
		v["code_expires_at"] = d.CodeExpiry
		v["status"] = "pending"
	}
	return v
}
func (h *Handler) adminDevices(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if r.URL.Path == "/api/devices" {
		switch r.Method {
		case "GET":
			rows, err := tx.QueryContext(ctx, "SELECT state FROM v2_devices ORDER BY rowid")
			if err != nil {
				return err
			}
			list := []any{}
			for rows.Next() {
				var raw []byte
				if err = rows.Scan(&raw); err != nil {
					rows.Close()
					return err
				}
				var d Device
				if err = json.Unmarshal(raw, &d); err != nil {
					rows.Close()
					return err
				}
				list = append(list, h.deviceView(&d))
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return err
			}
			writeJSON(w, 200, map[string]any{"devices": list})
			return nil
		case "POST":
			var req struct {
				Name string `json:"name"`
			}
			if _, err = decodeBody(w, r, &req); err != nil {
				return err
			}
			req.Name = strings.TrimSpace(req.Name)
			if !validDeviceName(req.Name) {
				return errf(422, "invalid_name")
			}
			var count, panels int
			if err = tx.QueryRowContext(ctx, "SELECT count(*),coalesce(sum(json_extract(state,'$.panel_enabled')),0) FROM v2_devices").Scan(&count, &panels); err != nil {
				return err
			}
			if count >= 256 {
				return errf(409, "device_limit")
			}
			name := req.Name
			if !validDisplayName(name) {
				name = "DEVICE"
			}
			d := &Device{ID: randomHex(8), Name: req.Name, Created: h.now().Unix(), Latest: map[string]latestValue{}, Panel: panels < 4, DisplayName: name}
			if _, err = tx.ExecContext(ctx, "INSERT INTO v2_devices(id,state) VALUES(?,?)", d.ID, []byte(`{}`)); err != nil {
				return err
			}
			if err = h.issueCode(ctx, tx, d); err != nil {
				return err
			}
			if err = h.audit(ctx, tx, "device_created", d.ID); err != nil {
				return err
			}
			if err = tx.Commit(); err != nil {
				return err
			}
			writeJSON(w, 201, h.deviceView(d))
			return nil
		default:
			return errf(405, "method_not_allowed")
		}
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/devices/"), "/")
	if !strings.HasPrefix(r.URL.Path, "/api/devices/") || len(parts) > 2 {
		return errf(404, "not_found")
	}
	d, err := loadDevice(ctx, tx, "id", parts[0])
	if err != nil {
		return err
	}
	action := ""
	if len(parts) == 2 {
		action = parts[1]
	}
	switch {
	case action == "" && r.Method == "DELETE":
		if _, err = tx.ExecContext(ctx, "DELETE FROM v2_devices WHERE id=?", d.ID); err != nil {
			return err
		}
		if err = h.audit(ctx, tx, "device_deleted", d.ID); err != nil {
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
		w.WriteHeader(204)
		return nil
	case action == "code" && r.Method == "POST":
		if err = h.issueCode(ctx, tx, d); err != nil {
			return err
		}
		if err = h.audit(ctx, tx, "code_issued", d.ID); err != nil {
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
		writeJSON(w, 200, h.deviceView(d))
		return nil
	case action == "profile" && r.Method == "GET":
		if d.Profile == nil {
			return errf(404, "profile_required")
		}
		writeJSON(w, 200, map[string]any{"device_id": d.ID, "name": d.Name, "revision": d.Revision, "interval_seconds": d.Profile.IntervalSeconds, "inventory": d.Profile.Inventory, "enabled": d.Profile.Enabled})
		return nil
	case action == "metrics" && r.Method == "GET":
		latest := map[string]latestValue{}
		if d.Profile != nil {
			for k, v := range d.Latest {
				if h.fresh(d, k, v) {
					latest[k] = v
				}
			}
		}
		writeJSON(w, 200, map[string]any{"device": h.deviceView(d), "profile": d.Profile, "latest": latest, "server_time": h.now().Unix()})
		return nil
	case action == "history" && r.Method == "GET":
		rows, err := tx.QueryContext(ctx, "SELECT revision,received_at,body FROM v2_batches WHERE device_id=? AND received_at>=? ORDER BY seq DESC LIMIT 100", d.ID, h.now().Unix()-h.cfg.HistoryRetentionHours*3600)
		if err != nil {
			return err
		}
		list := []any{}
		for rows.Next() {
			var rev uint64
			var at int64
			var body []byte
			if err = rows.Scan(&rev, &at, &body); err != nil {
				rows.Close()
				return err
			}
			list = append(list, map[string]any{"revision": rev, "received_at": at, "batch": json.RawMessage(body)})
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		writeJSON(w, 200, map[string]any{"batches": list})
		return nil
	case action == "panel" && r.Method == "PUT":
		var req struct {
			Enabled bool   `json:"enabled"`
			Name    string `json:"name"`
			Disk    uint16 `json:"disk_id"`
		}
		if _, err = decodeBody(w, r, &req); err != nil {
			return err
		}
		if !validDisplayName(req.Name) {
			return errf(422, "invalid_display_name")
		}
		if req.Disk != 0 {
			found := false
			if d.Profile != nil {
				for _, o := range d.Profile.Inventory.Objects {
					found = found || (o.Kind == "drive" && o.ID == req.Disk)
				}
				for _, dr := range d.Profile.Inventory.Drives {
					found = found || dr.ID == req.Disk
				}
			}
			if !found {
				return errf(422, "unknown_disk")
			}
		}
		if req.Enabled && !d.Panel {
			var n int
			if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM v2_devices WHERE json_extract(state,'$.panel_enabled')=1").Scan(&n); err != nil {
				return err
			}
			if n >= 4 {
				return errf(409, "panel_full")
			}
		}
		d.Panel = req.Enabled
		d.DisplayName = req.Name
		d.DriveID = req.Disk
		if err = saveDevice(ctx, tx, d); err != nil {
			return err
		}
		if err = h.audit(ctx, tx, "panel_updated", d.ID); err != nil {
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
		writeJSON(w, 200, h.deviceView(d))
		return nil
	default:
		return errf(405, "method_not_allowed")
	}
}
func (h *Handler) register(w http.ResponseWriter, r *http.Request) error {
	if r.Method != "POST" {
		return errf(405, "method_not_allowed")
	}
	// Global rate is essential behind a reverse proxy: never trust arbitrary X-Forwarded-For.
	if !h.allow("register-global", 12*time.Second, 5) || !h.allow("ip:register:"+peer(r), 12*time.Second, 5) {
		w.Header().Set("Retry-After", "12")
		return errf(429, "rate_limited")
	}
	var req struct {
		Code string `json:"code"`
	}
	if _, err := decodeBody(w, r, &req); err != nil {
		return err
	}
	if len(req.Code) != 6 || strings.IndexFunc(req.Code, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
		return errf(404, "invalid_code")
	}
	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	d, err := loadDevice(r.Context(), tx, "code", req.Code)
	var ae *apiError
	if errors.As(err, &ae) {
		return errf(404, "invalid_code")
	}
	if err != nil {
		return err
	}
	if h.now().Unix() >= d.CodeExpiry {
		return errf(410, "code_expired")
	}
	token := randomHex(32)
	if _, err = tx.ExecContext(r.Context(), "UPDATE v2_devices SET token_hash=? WHERE id=?", digest(token), d.ID); err != nil {
		return err
	}
	d.Code = ""
	d.CodeExpiry = 0
	d.Registered = true
	if err = saveDevice(r.Context(), tx, d); err != nil {
		return err
	}
	if err = h.audit(r.Context(), tx, "device_registered", d.ID); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	writeJSON(w, 200, map[string]string{"device_id": d.ID, "name": d.Name, "token": token})
	return nil
}
func maxStalenessSeconds(p *v2.Profile) int64 { return max(30, int64(p.IntervalSeconds)*3) }
