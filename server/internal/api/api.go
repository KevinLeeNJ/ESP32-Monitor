// Package api exposes HTTP handlers; repository and clock are injected for module UTs.
package api

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strings"
	"time"

	"esp32-monitor/server/internal/config"
	"esp32-monitor/server/internal/metrics"
	"esp32-monitor/server/internal/panel"
	"esp32-monitor/server/internal/storage"
	"golang.org/x/time/rate"
)

type Repository interface {
	Ingest(context.Context, string, []metrics.Sample, int64) error
	Snapshot(context.Context) (storage.Snapshot, error)
	Ready(context.Context) error
}
type credential struct {
	id    string
	hash  [32]byte
	write bool
}
type Handler struct {
	cfg         config.Config
	repo        Repository
	now         func() time.Time
	log         *slog.Logger
	credentials []credential
	limits      map[string]*rate.Limiter
}

func New(c config.Config, repo Repository, now func() time.Time, logger *slog.Logger) *Handler {
	h := &Handler{cfg: c, repo: repo, now: now, log: logger, limits: map[string]*rate.Limiter{}}
	h.credentials = append(h.credentials, credential{hash: sha256.Sum256([]byte(c.ReadToken))})
	for _, a := range c.Agents {
		h.credentials = append(h.credentials, credential{id: a.ID, hash: sha256.Sum256([]byte(a.WriteToken)), write: true})
		h.limits[a.ID] = rate.NewLimiter(rate.Every(time.Second), 10)
	}
	return h
}
func (h *Handler) authenticate(r *http.Request, write bool) (string, int) {
	headers := r.Header.Values("Authorization")
	if len(headers) != 1 {
		return "", http.StatusUnauthorized
	}
	parts := strings.Fields(headers[0])
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || len(parts[1]) != 64 {
		return "", http.StatusUnauthorized
	}
	hash := sha256.Sum256([]byte(parts[1]))
	index := -1
	for i, c := range h.credentials {
		if subtle.ConstantTimeCompare(hash[:], c.hash[:]) == 1 {
			index = i
		}
	}
	if index < 0 {
		return "", http.StatusUnauthorized
	}
	c := h.credentials[index]
	if c.write != write {
		return "", http.StatusForbidden
	}
	return c.id, 0
}
func failure(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
}
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	defer func() {
		if recover() != nil {
			h.log.Error("request failed", "reason", "internal_error")
			failure(w, 500, "internal_error")
		}
	}()
	var method string
	switch r.URL.Path {
	case "/v1/ingest/telegraf":
		method = "POST"
	case "/v1/devices/summary", "/healthz", "/readyz":
		method = "GET"
	default:
		failure(w, 404, "not_found")
		return
	}
	if r.Method != method {
		w.Header().Set("Allow", method)
		failure(w, 405, "method_not_allowed")
		return
	}
	switch r.URL.Path {
	case "/healthz":
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = io.WriteString(w, `{"status":"ok"}`)
	case "/readyz":
		if h.repo.Ready(ctx) != nil {
			failure(w, 503, "storage_unavailable")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = io.WriteString(w, `{"status":"ready"}`)
	case "/v1/ingest/telegraf":
		h.ingest(w, r)
	case "/v1/devices/summary":
		h.summary(w, r)
	}
}
func (h *Handler) ingest(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	id, status := h.authenticate(r, true)
	accepted := 0
	rejected := 0
	reason := ""
	defer func() {
		h.log.Info("ingest", "device_id", id, "status", status, "accepted_fields", accepted, "rejected_batches", rejected, "reason", reason, "duration_ms", time.Since(start).Milliseconds())
	}()
	reject := func(s int, code string) { status = s; reason = code; rejected = 1; failure(w, s, code) }
	if status != 0 {
		reject(status, "unauthorized")
		return
	}
	now := h.now()
	if !h.limits[id].AllowN(now, 1) {
		w.Header().Set("Retry-After", "1")
		reject(429, "rate_limited")
		return
	}
	contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	encoding := r.Header.Get("Content-Encoding")
	if err != nil || contentType != "application/json" || (encoding != "" && !strings.EqualFold(encoding, "identity")) {
		reject(415, "unsupported_media_type")
		return
	}
	if r.ContentLength > metrics.MaxBody {
		reject(413, "payload_too_large")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, metrics.MaxBody))
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			reject(413, "payload_too_large")
		} else {
			reject(400, "invalid_body")
		}
		return
	}
	samples, err := metrics.Parse(body, now.Unix())
	if err != nil {
		switch {
		case errors.Is(err, metrics.ErrJSON):
			reject(400, "invalid_json")
		case errors.Is(err, metrics.ErrTooLarge):
			reject(413, "payload_too_large")
		default:
			reject(422, "invalid_payload")
		}
		return
	}
	if err = h.repo.Ingest(r.Context(), id, samples, now.Unix()); err != nil {
		if errors.Is(err, storage.ErrSeriesLimit) {
			reject(422, "series_limit_exceeded")
		} else {
			reject(503, "storage_unavailable")
		}
		return
	}
	accepted = len(samples)
	status = 204
	w.WriteHeader(204)
}
func (h *Handler) summary(w http.ResponseWriter, r *http.Request) {
	if _, status := h.authenticate(r, false); status != 0 {
		failure(w, status, "unauthorized")
		return
	}
	snapshot, err := h.repo.Snapshot(r.Context())
	if err != nil {
		failure(w, 503, "storage_unavailable")
		return
	}
	body, err := json.Marshal(panel.Project(h.cfg, snapshot, h.now().Unix()))
	if err != nil || len(body) > 4096 {
		failure(w, 503, "summary_unavailable")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(200)
	_, _ = w.Write(body)
}
