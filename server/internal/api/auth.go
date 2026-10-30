package api

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"golang.org/x/crypto/argon2"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const sessionLifetime = 7 * 24 * time.Hour
const sessionIdle = 24 * time.Hour

// RFC 9106's low-memory Argon2id profile. Parameters form part of schema v1.
func passwordHash(password string, salt []byte) []byte {
	return argon2.IDKey([]byte(password), salt, 3, 64*1024, 4, 32)
}
func newSalt() []byte {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("secure random unavailable")
	}
	return b
}
func (h *Handler) bootstrap(out io.Writer) error {
	tx, err := h.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("UPDATE web_credentials SET generation=generation WHERE id=1"); err != nil {
		return err
	}
	var salt, hash []byte
	var must int
	err = tx.QueryRow("SELECT salt,hash,must_change FROM web_credentials WHERE id=1").Scan(&salt, &hash, &must)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil && (len(salt) != 16 || len(hash) != 32) {
		return errors.New("invalid WebUI credential; restore database backup")
	}
	if err == nil && must == 0 {
		return tx.Commit()
	}
	if out == nil {
		return errors.New("bootstrap password output required")
	}
	password := randomHex(16)
	salt = newSalt()
	hash = passwordHash(password, salt)
	if _, err = tx.Exec(`INSERT INTO web_credentials(id,salt,hash,must_change,generation) VALUES(1,?,?,1,1) ON CONFLICT(id) DO UPDATE SET salt=excluded.salt,hash=excluded.hash,must_change=1,generation=generation+1`, salt, hash); err != nil {
		return err
	}
	if _, err = tx.Exec("DELETE FROM web_sessions"); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "WebUI 临时密码: %s\n首次登录后必须修改密码；未完成改密时，重启将重新生成。\n", password)
	return err
}
func (h *Handler) secureCookie(r *http.Request) bool {
	u, _ := url.Parse(h.cfg.PublicBaseURL)
	return r.TLS != nil || u.Scheme == "https" && strings.EqualFold(r.Host, u.Host)
}
func (h *Handler) cookieName(r *http.Request) string {
	if h.secureCookie(r) {
		return "__Host-monitor_session"
	}
	return "monitor_session"
}
func (h *Handler) setCookie(w http.ResponseWriter, r *http.Request, token string, ttl time.Duration) {
	age := int(ttl.Seconds())
	if ttl < 0 {
		age = -1
	}
	http.SetCookie(w, &http.Cookie{Name: h.cookieName(r), Value: token, Path: "/", HttpOnly: true, Secure: h.secureCookie(r), SameSite: http.SameSiteStrictMode, MaxAge: age, Expires: h.now().Add(ttl)})
}
func (h *Handler) validOrigin(r *http.Request) bool {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	scheme := "http"
	if h.secureCookie(r) {
		scheme = "https"
	}
	return r.Header.Get("Origin") == scheme+"://"+r.Host
}

type session struct {
	Hash, CSRF string
	MustChange bool
	Generation int
}

func (h *Handler) session(r *http.Request) (session, error) {
	cookies := r.CookiesNamed(h.cookieName(r))
	if len(cookies) != 1 || len(cookies[0].Value) != 64 {
		return session{}, errf(401, "authentication_required")
	}
	s := session{Hash: digest(cookies[0].Value)}
	var must int
	err := h.db.QueryRowContext(r.Context(), `SELECT s.csrf,c.must_change,c.generation FROM web_sessions s JOIN web_credentials c ON c.id=1 AND c.generation=s.generation WHERE s.hash=? AND s.expires_at>? AND s.last_used>?`, s.Hash, h.now().Unix(), h.now().Add(-sessionIdle).Unix()).Scan(&s.CSRF, &must, &s.Generation)
	if errors.Is(err, sql.ErrNoRows) {
		return s, errf(401, "authentication_required")
	}
	s.MustChange = must != 0
	return s, err
}
func (h *Handler) newSession(ctx context.Context, tx *sql.Tx, generation int) (token, csrf string, err error) {
	token = randomHex(32)
	csrf = randomHex(32)
	_, err = tx.ExecContext(ctx, `INSERT INTO web_sessions(hash,csrf,created_at,expires_at,last_used,generation) VALUES(?,?,?,?,?,?)`, digest(token), csrf, h.now().Unix(), h.now().Add(sessionLifetime).Unix(), h.now().Unix(), generation)
	if err == nil {
		_, err = tx.ExecContext(ctx, `DELETE FROM web_sessions WHERE hash IN (SELECT hash FROM web_sessions ORDER BY created_at DESC,rowid DESC LIMIT -1 OFFSET 16)`)
	}
	return
}
func passwordValid(s string) bool {
	if !utf8.ValidString(s) || utf8.RuneCountInString(s) < 12 || len(s) > 256 {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func (h *Handler) authLimit(w http.ResponseWriter, r *http.Request) error {
	if !h.allow("auth-global", 6*time.Second, 10) || !h.allow("ip:auth:"+peer(r), 12*time.Second, 5) {
		w.Header().Set("Retry-After", "12")
		return errf(429, "rate_limited")
	}
	return nil
}
func (h *Handler) login(w http.ResponseWriter, r *http.Request) error {
	if r.Method != "POST" {
		return errf(405, "method_not_allowed")
	}
	if !h.validOrigin(r) {
		return errf(403, "origin_rejected")
	}
	if err := h.authLimit(w, r); err != nil {
		return err
	}
	var req struct {
		Password string `json:"password"`
	}
	if _, err := decodeBody(w, r, &req); err != nil {
		return err
	}
	if len(req.Password) == 0 || len(req.Password) > 256 {
		return errf(401, "invalid_password")
	}
	var salt, hash []byte
	var generation, must int
	if err := h.db.QueryRowContext(r.Context(), "SELECT salt,hash,generation,must_change FROM web_credentials WHERE id=1").Scan(&salt, &hash, &generation, &must); err != nil {
		return err
	}
	if subtle.ConstantTimeCompare(passwordHash(req.Password, salt), hash) != 1 {
		h.log.Warn("login_failed")
		return errf(401, "invalid_password")
	}
	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	token, csrf, err := h.newSession(r.Context(), tx, generation)
	if err != nil {
		return err
	}
	if err = h.audit(r.Context(), tx, "login_succeeded", ""); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	h.setCookie(w, r, token, sessionLifetime)
	writeJSON(w, 200, map[string]any{"must_change_password": must != 0, "csrf_token": csrf})
	return nil
}
func (h *Handler) changePassword(w http.ResponseWriter, r *http.Request, s session) error {
	if r.Method != "POST" {
		return errf(405, "method_not_allowed")
	}
	if err := h.authLimit(w, r); err != nil {
		return err
	}
	var req struct {
		Current  string `json:"current_password"`
		Password string `json:"new_password"`
	}
	if _, err := decodeBody(w, r, &req); err != nil {
		return err
	}
	if !passwordValid(req.Password) {
		return errf(422, "password_requires_12_characters")
	}
	if len(req.Current) > 256 || req.Current == req.Password {
		return errf(422, "new_password_must_differ")
	}
	var salt, hash []byte
	if err := h.db.QueryRowContext(r.Context(), "SELECT salt,hash FROM web_credentials WHERE id=1").Scan(&salt, &hash); err != nil {
		return err
	}
	if subtle.ConstantTimeCompare(passwordHash(req.Current, salt), hash) != 1 {
		return errf(401, "invalid_password")
	}
	salt = newSalt()
	hash = passwordHash(req.Password, salt)
	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(r.Context(), "UPDATE web_credentials SET salt=?,hash=?,must_change=0,generation=generation+1 WHERE id=1 AND generation=?", salt, hash, s.Generation)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return errf(401, "authentication_required")
	}
	if _, err = tx.ExecContext(r.Context(), "DELETE FROM web_sessions"); err != nil {
		return err
	}
	token, csrf, err := h.newSession(r.Context(), tx, s.Generation+1)
	if err != nil {
		return err
	}
	if err = h.audit(r.Context(), tx, "password_changed", ""); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	h.setCookie(w, r, token, sessionLifetime)
	writeJSON(w, 200, map[string]any{"must_change_password": false, "csrf_token": csrf})
	return nil
}
func (h *Handler) admin(w http.ResponseWriter, r *http.Request) error {
	if r.URL.Path == "/api/auth/login" {
		return h.login(w, r)
	}
	s, err := h.session(r)
	if err != nil {
		return err
	}
	if r.Method != "GET" && (!h.validOrigin(r) || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(s.CSRF)) != 1) {
		return errf(403, "csrf_rejected")
	}
	if r.URL.Path == "/api/auth/state" && r.Method == "GET" {
		writeJSON(w, 200, map[string]any{"must_change_password": s.MustChange, "csrf_token": s.CSRF})
		return nil
	}
	if r.URL.Path == "/api/auth/password" {
		return h.changePassword(w, r, s)
	}
	if r.URL.Path == "/api/auth/logout" && r.Method == "POST" {
		if _, err = h.db.ExecContext(r.Context(), "DELETE FROM web_sessions WHERE hash=?", s.Hash); err != nil {
			return err
		}
		h.setCookie(w, r, "", -time.Hour)
		w.WriteHeader(204)
		return nil
	}
	if s.MustChange {
		return errf(403, "password_change_required")
	}
	if _, err = h.db.ExecContext(r.Context(), "UPDATE web_sessions SET last_used=? WHERE hash=?", h.now().Unix(), s.Hash); err != nil {
		return err
	}
	if r.Method != "GET" && !h.allow("admin", time.Second, 20) {
		return errf(429, "rate_limited")
	}
	if r.URL.Path == "/api/read-token" {
		if r.Method != "POST" {
			return errf(405, "method_not_allowed")
		}
		writeJSON(w, 200, map[string]string{"read_token": h.cfg.ReadToken})
		return nil
	}
	return h.adminDevices(w, r)
}
