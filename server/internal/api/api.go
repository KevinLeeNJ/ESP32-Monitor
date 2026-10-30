// Package api implements the persistent v2 collector and authenticated WebUI.
package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"esp32-monitor/server/internal/config"
	"esp32-monitor/server/internal/storage"
	"golang.org/x/time/rate"
)

const maxBody = 256 << 10

//go:embed web
var assets embed.FS

//go:embed schema.sql
var schema string

//go:embed web/index.html
var webHTML []byte

//go:embed web/app.js
var webJS []byte

//go:embed web/style.css
var webCSS []byte

type apiError struct {
	status     int
	code       string
	serverTime bool
}

func (e *apiError) Error() string            { return e.code }
func errf(status int, code string) *apiError { return &apiError{status: status, code: code} }

type Handler struct {
	cfg    config.Config
	db     *sql.DB
	now    func() time.Time
	log    *slog.Logger
	mu     sync.Mutex // serializes read/modify/write transactions, including credential rotation
	limits map[string]*rate.Limiter
}

func New(c config.Config, repo *storage.Store, now func() time.Time, logger *slog.Logger, secrets io.Writer) (*Handler, error) {
	if now == nil {
		now = time.Now
	}
	if logger == nil {
		logger = slog.Default()
	}
	h := &Handler{cfg: c, db: repo.Database(), now: now, log: logger, limits: map[string]*rate.Limiter{}}
	tx, err := h.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(schema); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	if err := h.bootstrap(secrets); err != nil {
		return nil, err
	}
	return h, nil
}
func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("secure random unavailable")
	}
	return hex.EncodeToString(b)
}
func digest(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func failure(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]any{"error": code})
}
func (h *Handler) fail(w http.ResponseWriter, err error) {
	var e *apiError
	if errors.As(err, &e) {
		body := map[string]any{"error": e.code}
		if e.serverTime {
			body["server_time"] = h.now().UTC().Format(time.RFC3339)
		}
		writeJSON(w, e.status, body)
		return
	}
	h.log.Error("request failed", "reason", "storage_unavailable")
	failure(w, 503, "storage_unavailable")
}

// strictJSON rejects ambiguous duplicate keys and nulls before typed decoding.
// Unknown fields and integer/string range checks are then enforced by the wire types.
func strictJSON(raw []byte, dst any) error {
	if !utf8.Valid(raw) {
		return errf(422, "invalid_json")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var walk func(int) error
	walk = func(depth int) error {
		if depth > 32 {
			return errors.New("json depth")
		}
		t, err := dec.Token()
		if err != nil {
			return err
		}
		if t == nil {
			return errors.New("null forbidden")
		}
		d, ok := t.(json.Delim)
		if !ok {
			return nil
		}
		switch d {
		case '{':
			seen := map[string]bool{}
			for dec.More() {
				k, e := dec.Token()
				if e != nil {
					return e
				}
				key, ok := k.(string)
				if !ok || seen[key] {
					return errors.New("duplicate key")
				}
				seen[key] = true
				if e = walk(depth + 1); e != nil {
					return e
				}
			}
		case '[':
			for dec.More() {
				if e := walk(depth + 1); e != nil {
					return e
				}
			}
		default:
			return errors.New("invalid delimiter")
		}
		_, err = dec.Token()
		return err
	}
	if err := walk(0); err != nil {
		return errf(422, "invalid_json")
	}
	if _, err := dec.Token(); err != io.EOF {
		return errf(400, "invalid_json")
	}
	dec = json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return errf(422, "invalid_payload")
	}
	return validateWireStrings(raw, dst)
}
func decodeBody(w http.ResponseWriter, r *http.Request, dst any) ([]byte, error) {
	ct, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || ct != "application/json" || (r.Header.Get("Content-Encoding") != "" && r.Header.Get("Content-Encoding") != "identity") {
		return nil, errf(415, "unsupported_media_type")
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		return nil, errf(413, "payload_too_large")
	}
	if err = strictJSON(raw, dst); err != nil {
		return nil, err
	}
	return raw, nil
}
func etag(revision uint64) string { return `"` + strconv.FormatUint(revision, 10) + `"` }

var batchIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

func bearer(r *http.Request) string {
	if len(r.Header.Values("Authorization")) != 1 {
		return ""
	}
	f := strings.Fields(r.Header.Get("Authorization"))
	if len(f) != 2 || !strings.EqualFold(f[0], "Bearer") || len(f[1]) != 64 {
		return ""
	}
	return f[1]
}
func (h *Handler) allow(key string, interval time.Duration, burst int) bool {
	l := h.limits[key]
	if l == nil {
		if len(h.limits) > 4096 { // bounded: global buckets still protect login and registration
			for k := range h.limits {
				if strings.HasPrefix(k, "ip:") {
					delete(h.limits, k)
				}
			}
		}
		l = rate.NewLimiter(rate.Every(interval), burst)
		h.limits[key] = l
	}
	return l.AllowN(h.now(), 1)
}
func peer(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// Accept literal local addresses without resolving arbitrary hostnames. Docker
// port publishing can forward a host LAN address to a different container IP.
func localHostRequest(r *http.Request) bool {
	remote := net.ParseIP(peer(r))
	if remote == nil || !(remote.IsLoopback() || remote.IsPrivate() || remote.IsLinkLocalUnicast()) {
		return false
	}
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	} else {
		host = strings.Trim(host, "[]")
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast())
}
func (h *Handler) audit(ctx context.Context, tx *sql.Tx, event, id string) error {
	_, err := tx.ExecContext(ctx, "INSERT INTO web_audit(at,event,device_id) VALUES(?,?,?)", h.now().Unix(), event, id)
	return err
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if h.secureCookie(r) {
		w.Header().Set("Strict-Transport-Security", "max-age=31536000")
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self' data:; font-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	defer func() {
		if recover() != nil {
			h.log.Error("request panic", "reason", "internal_error")
			failure(w, 500, "internal_error")
		}
	}()
	// Host allowlist also protects local HTTP against DNS rebinding. Forwarded headers are ignored.
	u, _ := url.Parse(h.cfg.PublicBaseURL)
	if !strings.EqualFold(r.Host, u.Host) && !localHostRequest(r) {
		failure(w, 403, "invalid_host")
		return
	}
	if strings.HasPrefix(r.URL.Path, "/fonts/") || strings.HasPrefix(r.URL.Path, "/icons/") || r.URL.Path == "/app-icon.png" {
		if r.Method != "GET" {
			failure(w, 405, "method_not_allowed")
			return
		}
		path := "web" + r.URL.Path
		info, err := fs.Stat(assets, path)
		if err != nil || info.IsDir() {
			failure(w, 404, "not_found")
			return
		}
		b, err := assets.ReadFile(path)
		if err != nil {
			failure(w, 404, "not_found")
			return
		}
		switch {
		case strings.HasSuffix(path, ".woff2"):
			w.Header().Set("Content-Type", "font/woff2")
		case strings.HasSuffix(path, ".ttf"):
			w.Header().Set("Content-Type", "font/ttf")
		case strings.HasSuffix(path, ".css"):
			w.Header().Set("Content-Type", "text/css")
		case strings.HasSuffix(path, ".png"):
			w.Header().Set("Content-Type", "image/png")
		default:
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		}
		_, _ = w.Write(b)
		return
	}
	if r.URL.Path == "/" || r.URL.Path == "/app.js" || r.URL.Path == "/style.css" {
		if r.Method != "GET" {
			failure(w, 405, "method_not_allowed")
			return
		}
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(webHTML)
		case "/app.js":
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
			_, _ = w.Write(webJS)
		case "/style.css":
			w.Header().Set("Content-Type", "text/css; charset=utf-8")
			_, _ = w.Write(webCSS)
		}
		return
	}
	if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
		if r.Method != "GET" {
			failure(w, 405, "method_not_allowed")
			return
		}
		if r.URL.Path == "/readyz" {
			if err := h.ready(ctx); err != nil {
				h.fail(w, err)
				return
			}
		}
		writeJSON(w, 200, map[string]string{"status": "ok"})
		return
	}
	// Authentication KDFs and mutations are bounded and serialized. Persist before responding.
	h.mu.Lock()
	defer h.mu.Unlock()
	if r.Context().Err() != nil {
		failure(w, 503, "request_timeout")
		return
	}
	var e error
	switch {
	case strings.HasPrefix(r.URL.Path, "/api/"):
		e = h.admin(w, r)
	case r.URL.Path == "/v2/collector/register":
		e = h.register(w, r)
	case r.URL.Path == "/v2/collector/profile" || r.URL.Path == "/v2/collector/samples":
		e = h.collect(w, r)
	case r.URL.Path == "/v2/devices/summary":
		e = h.summary(w, r)
	default:
		e = errf(404, "not_found")
	}
	if e != nil {
		h.fail(w, e)
	}
}

// Cleanup keeps idempotency at least 24h even when history retention is shorter.
func (h *Handler) Cleanup(ctx context.Context) error {
	retention := max(h.cfg.HistoryRetentionHours, 24) * 3600
	tasks := []struct {
		query string
		args  []any
	}{
		{`DELETE FROM v2_batches WHERE seq IN (SELECT seq FROM v2_batches WHERE received_at<? LIMIT 1000)`, []any{h.now().Unix() - retention}},
		{`DELETE FROM v2_values WHERE rowid IN (SELECT rowid FROM v2_values WHERE received_at<? LIMIT 1000)`, []any{h.now().Unix() - retention}},
		{`DELETE FROM v2_profiles WHERE rowid IN (SELECT p.rowid FROM v2_profiles p JOIN v2_devices d ON d.id=p.device_id WHERE p.revision!=json_extract(d.state,'$.revision') AND NOT EXISTS(SELECT 1 FROM v2_batches b WHERE b.device_id=p.device_id AND b.revision=p.revision) LIMIT 1000)`, nil},
		{`DELETE FROM web_sessions WHERE rowid IN (SELECT rowid FROM web_sessions WHERE expires_at<=? OR last_used<=? LIMIT 1000)`, []any{h.now().Unix(), h.now().Add(-sessionIdle).Unix()}},
		{`DELETE FROM web_audit WHERE id IN (SELECT id FROM web_audit WHERE at<? LIMIT 1000)`, []any{h.now().Add(-30 * 24 * time.Hour).Unix()}},
	}
	// Yield the single DB connection between bounded deletes so HTTP work can run.
	for _, task := range tasks {
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			result, err := h.db.ExecContext(ctx, task.query, task.args...)
			if err != nil {
				return err
			}
			n, err := result.RowsAffected()
			if err != nil {
				return err
			}
			if n < 1000 {
				break
			}
		}
	}
	return nil
}

func (h *Handler) ready(ctx context.Context) error {
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var count int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM v2_devices").Scan(&count); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "UPDATE web_credentials SET id=id WHERE 0")
	return err
}
