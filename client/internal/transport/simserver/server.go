// Package simserver implements a local ESP32-Monitor Server simulator for the
// Client → Server v2 collector protocol defined in docs/COLLECTOR_UPLOAD_V2.md.
// Device identity is bound to the Bearer token; every request is logged.
package simserver

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/big"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	v2 "esp32-monitor/client/internal/collector/uploadv2"
)

const maxBody = 256 << 10 // 256 KiB, per doc §6.6

const pairingTTL = 10 * time.Minute

// maxFeedEntries bounds the WebUI batch feed (oldest entries are dropped).
const maxFeedEntries = 100

//go:embed webui.html
var webuiHTML []byte

type apiError struct {
	status     int
	code       string
	serverTime bool
}

func (e *apiError) Error() string { return e.code }

func errf(status int, code string) *apiError { return &apiError{status: status, code: code} }

// Device holds per-token simulator state: the confirmed profile, its revision,
// batch idempotency records and the latest observed values (log-only). In
// registration mode a device also carries the Server WebUI name and pending
// six-digit verification code.
type Device struct {
	ID         string
	Name       string
	Token      string
	Code       string
	CodeExpiry time.Time
	Created    time.Time
	Registered bool
	Profile    *v2.Profile
	Revision   uint64
	Batches    map[string]batchRecord
	Latest     map[string]latestValue
	LastSeen   time.Time
}

type batchRecord struct {
	bodyHash [sha256.Size]byte
	at       time.Time
}

type latestValue struct {
	Value    string
	Time     int64
	Revision uint64
}

// batchEntry is one accepted sample batch, kept verbatim for the WebUI feed.
type batchEntry struct {
	Seq      uint64    `json:"seq"`
	DeviceID string    `json:"device_id,omitempty"`
	Device   string    `json:"device"`
	Received time.Time `json:"received_at"`
	Revision uint64    `json:"revision"`
	Samples  int       `json:"samples"`
	Raw      string    `json:"raw"`
}

// Server is the in-memory simulator. It is safe for concurrent use.
type Server struct {
	mu           sync.Mutex
	devices      map[string]*Device // by Bearer token
	byID         map[string]*Device
	byCode       map[string]*Device
	feed         []batchEntry
	feedSeq      uint64
	registration bool
	requiredTok  string
	now          func() time.Time
	log          *log.Logger
}

// New creates a simulator. When requiredToken is non-empty, only that Bearer
// token is accepted; otherwise every distinct token becomes its own device.
func New(requiredToken string, logger *log.Logger) *Server {
	return newServer(false, requiredToken, logger)
}

// NewRegistration creates a simulator whose tokens must be obtained through
// the Server WebUI device flow (POST /v2/collector/register).
func NewRegistration(logger *log.Logger) *Server {
	return newServer(true, "", logger)
}

func newServer(registration bool, requiredToken string, logger *log.Logger) *Server {
	if logger == nil {
		logger = log.Default()
	}
	return &Server{
		devices:      map[string]*Device{},
		byID:         map[string]*Device{},
		byCode:       map[string]*Device{},
		registration: registration,
		requiredTok:  requiredToken,
		now:          time.Now,
		log:          logger,
	}
}

func (s *Server) device(token string) *Device {
	d, ok := s.devices[token]
	if !ok {
		d = &Device{Token: token, Registered: true, Batches: map[string]batchRecord{}, Latest: map[string]latestValue{}}
		s.devices[token] = d
	}
	return d
}

// Handler returns the HTTP handler with request logging around every route.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/collector/profile", s.withAuth(s.profile))
	mux.HandleFunc("/v2/collector/samples", s.withAuth(s.samples))
	mux.HandleFunc("/v2/collector/register", s.register)
	mux.HandleFunc("/api/devices", s.adminDevices)
	mux.HandleFunc("/api/devices/", s.adminDevice)
	mux.HandleFunc("/api/batches", s.adminBatches)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("/", s.webUI)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := s.now()
		rec := &statusRecorder{ResponseWriter: w, status: 200}
		mux.ServeHTTP(rec, r)
		// The WebUI polls the device list and batch feed every few seconds;
		// logging it would bury registration and collector events.
		if r.Method == http.MethodGet && (r.URL.Path == "/" || r.URL.Path == "/api/devices" || r.URL.Path == "/api/batches") {
			return
		}
		token, _ := bearer(r)
		s.log.Printf(`{"at":%q,"event":"http","method":%q,"path":%q,"status":%d,"ms":%.1f,"device":%q}`,
			start.UTC().Format(time.RFC3339Nano), r.Method, r.URL.Path, rec.status,
			float64(s.now().Sub(start).Microseconds())/1000, shortToken(token))
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = 200
	}
	return r.ResponseWriter.Write(b)
}

func shortToken(token string) string {
	if token == "" {
		return ""
	}
	if len(token) <= 8 {
		return "****"
	}
	return token[:4] + "…" + token[len(token)-4:]
}

func bearer(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return "", false
	}
	token := strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	return token, token != ""
}

func (s *Server) withAuth(next func(http.ResponseWriter, *http.Request, *Device)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, ok := bearer(r)
		if !ok {
			s.fail(w, errf(http.StatusUnauthorized, "unauthorized"))
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.registration {
			d := s.devices[token]
			if d == nil || !d.Registered || d.Token != token {
				s.fail(w, errf(http.StatusUnauthorized, "unauthorized"))
				return
			}
			next(w, r, d)
			return
		}
		if s.requiredTok != "" && subtle.ConstantTimeCompare([]byte(token), []byte(s.requiredTok)) != 1 {
			s.fail(w, errf(http.StatusUnauthorized, "unauthorized"))
			return
		}
		next(w, r, s.device(token))
	}
}

func (s *Server) fail(w http.ResponseWriter, e *apiError) {
	body := map[string]any{"error": e.code}
	if e.serverTime {
		body["server_time"] = s.now().UTC().Format(time.RFC3339)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(e.status)
	_ = json.NewEncoder(w).Encode(body)
	s.log.Printf(`{"at":%q,"event":"reject","status":%d,"error":%q}`,
		s.now().UTC().Format(time.RFC3339Nano), e.status, e.code)
}

func etag(revision uint64) string { return `"` + strconv.FormatUint(revision, 10) + `"` }

func revisionOf(et string) (uint64, bool) {
	et = strings.TrimPrefix(et, "W/")
	if len(et) < 2 || et[0] != '"' || et[len(et)-1] != '"' {
		return 0, false
	}
	n, err := strconv.ParseUint(et[1:len(et)-1], 10, 64)
	if err != nil || n == 0 || n > 9007199254740991 {
		return 0, false
	}
	return n, true
}

func decode(w http.ResponseWriter, r *http.Request, dst any) error {
	if ct := r.Header.Get("Content-Type"); ct != "" && !strings.HasPrefix(ct, "application/json") {
		return errf(http.StatusUnsupportedMediaType, "content_type_required")
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, maxBody+1))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var too *apiError
		if errors.As(err, &too) {
			return too
		}
		return errf(http.StatusBadRequest, "invalid_json")
	}
	return nil
}

// --- Device registration (Server WebUI + Client claim) --------------------

type deviceView struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	Status        string     `json:"status"`
	Registered    bool       `json:"registered"`
	Code          string     `json:"code,omitempty"`
	CodeExpiresAt *time.Time `json:"code_expires_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	LastSeenAt    *time.Time `json:"last_seen_at,omitempty"`
}

func (d *Device) view(now time.Time) deviceView {
	v := deviceView{ID: d.ID, Name: d.Name, Registered: d.Registered, CreatedAt: d.Created}
	if d.Code != "" && now.Before(d.CodeExpiry) {
		v.Status = "pending"
		v.Code = d.Code
		expiry := d.CodeExpiry
		v.CodeExpiresAt = &expiry
	} else if d.Registered {
		v.Status = "registered"
	} else {
		v.Status = "expired"
	}
	if !d.LastSeen.IsZero() {
		seen := d.LastSeen
		v.LastSeenAt = &seen
	}
	return v
}

func randomCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(900000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()+100000), nil
}

func randomHex(size int) (string, error) {
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func validDeviceName(name string) bool {
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) == 0 || utf8.RuneCountInString(name) > 64 {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func (s *Server) webUI(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" || r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(webuiHTML)
}

// recordBatchLocked appends an accepted batch to the WebUI feed ring buffer.
// Callers must hold s.mu.
func (s *Server) recordBatchLocked(d *Device, batch *v2.SampleBatch, raw []byte) {
	label := d.Name
	if label == "" {
		label = "令牌 " + shortToken(d.Token)
	}
	s.feedSeq++
	s.feed = append(s.feed, batchEntry{
		Seq:      s.feedSeq,
		DeviceID: d.ID,
		Device:   label,
		Received: s.now(),
		Revision: batch.Revision,
		Samples:  len(batch.Samples),
		Raw:      string(raw),
	})
	if len(s.feed) > maxFeedEntries {
		s.feed = s.feed[len(s.feed)-maxFeedEntries:]
	}
}

// adminBatches serves recent accepted batches for the WebUI feed. It works in
// both fixed-token and registration mode; ?after=<seq> returns only newer
// entries, and a cursor newer than the simulator's sequence triggers a full
// resend flagged as reset (the simulator was restarted).
func (s *Server) adminBatches(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.fail(w, errf(http.StatusMethodNotAllowed, "method_not_allowed"))
		return
	}
	after := uint64(0)
	if v := r.URL.Query().Get("after"); v != "" {
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			s.fail(w, errf(http.StatusBadRequest, "invalid_after"))
			return
		}
		after = n
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	reset := after > s.feedSeq
	if reset {
		after = 0
	}
	entries := make([]batchEntry, 0, len(s.feed))
	for _, e := range s.feed {
		if e.Seq > after {
			entries = append(entries, e)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"batches": entries, "latest": s.feedSeq, "reset": reset})
}

func (s *Server) adminDevices(w http.ResponseWriter, r *http.Request) {
	if !s.registration {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.listDevices(w)
	case http.MethodPost:
		s.createDevice(w, r)
	default:
		s.fail(w, errf(http.StatusMethodNotAllowed, "method_not_allowed"))
	}
}

func (s *Server) listDevices(w http.ResponseWriter) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	devices := make([]deviceView, 0, len(s.byID))
	for _, d := range s.byID {
		devices = append(devices, d.view(now))
	}
	sortDevices(devices)
	writeJSON(w, http.StatusOK, map[string]any{"devices": devices})
}

func sortDevices(devices []deviceView) {
	for i := 1; i < len(devices); i++ {
		for j := i; j > 0 && devices[j].CreatedAt.Before(devices[j-1].CreatedAt); j-- {
			devices[j], devices[j-1] = devices[j-1], devices[j]
		}
	}
}

func (s *Server) createDevice(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := decode(w, r, &req); err != nil {
		s.fail(w, asAPIError(err))
		return
	}
	name := strings.TrimSpace(req.Name)
	if !validDeviceName(name) {
		s.fail(w, errf(http.StatusBadRequest, "invalid_name"))
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	id, err := randomHex(8)
	if err != nil {
		s.fail(w, errf(http.StatusInternalServerError, "random_failed"))
		return
	}
	for s.byID[id] != nil {
		if id, err = randomHex(8); err != nil {
			s.fail(w, errf(http.StatusInternalServerError, "random_failed"))
			return
		}
	}
	code, err := s.uniqueCodeLocked()
	if err != nil {
		s.fail(w, errf(http.StatusInternalServerError, "random_failed"))
		return
	}
	now := s.now()
	d := &Device{
		ID: id, Name: name, Code: code, CodeExpiry: now.Add(pairingTTL), Created: now,
		Batches: map[string]batchRecord{}, Latest: map[string]latestValue{},
	}
	s.byID[id] = d
	s.byCode[code] = d
	s.log.Printf(`{"at":%q,"event":"device","op":"create","device":%q,"name":%q}`,
		now.UTC().Format(time.RFC3339Nano), id, name)
	writeJSON(w, http.StatusCreated, d.view(now))
}

func (s *Server) uniqueCodeLocked() (string, error) {
	for attempt := 0; attempt < 32; attempt++ {
		code, err := randomCode()
		if err != nil {
			return "", err
		}
		if s.byCode[code] == nil {
			return code, nil
		}
	}
	return "", errors.New("code_space_exhausted")
}

func (s *Server) adminDevice(w http.ResponseWriter, r *http.Request) {
	if !s.registration {
		http.NotFound(w, r)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/devices/")
	parts := strings.Split(rest, "/")
	id := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}
	s.mu.Lock()
	d := s.byID[id]
	s.mu.Unlock()
	if d == nil {
		s.fail(w, errf(http.StatusNotFound, "device_not_found"))
		return
	}
	switch {
	case r.Method == http.MethodDelete && action == "":
		s.deleteDevice(w, d)
	case r.Method == http.MethodPost && action == "code":
		s.issueCode(w, d)
	case r.Method == http.MethodGet && action == "profile":
		s.deviceProfile(w, d)
	default:
		s.fail(w, errf(http.StatusMethodNotAllowed, "method_not_allowed"))
	}
}

// deviceProfile serves the device's confirmed profile inventory for the WebUI
// 设备 tab: the long-term hardware descriptors uploaded once per profile sync.
func (s *Server) deviceProfile(w http.ResponseWriter, d *Device) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d.Profile == nil {
		s.fail(w, errf(http.StatusNotFound, "profile_required"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"device_id":        d.ID,
		"name":             d.Name,
		"revision":         d.Revision,
		"interval_seconds": d.Profile.IntervalSeconds,
		"inventory":        d.Profile.Inventory,
	})
}

func (s *Server) deleteDevice(w http.ResponseWriter, d *Device) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.byID, d.ID)
	if d.Code != "" {
		delete(s.byCode, d.Code)
	}
	if d.Token != "" {
		delete(s.devices, d.Token)
	}
	s.log.Printf(`{"at":%q,"event":"device","op":"delete","device":%q}`,
		s.now().UTC().Format(time.RFC3339Nano), d.ID)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) issueCode(w http.ResponseWriter, d *Device) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d.Code != "" {
		delete(s.byCode, d.Code)
	}
	code, err := s.uniqueCodeLocked()
	if err != nil {
		s.fail(w, errf(http.StatusInternalServerError, "random_failed"))
		return
	}
	now := s.now()
	d.Code = code
	d.CodeExpiry = now.Add(pairingTTL)
	s.byCode[code] = d
	s.log.Printf(`{"at":%q,"event":"device","op":"code","device":%q}`,
		now.UTC().Format(time.RFC3339Nano), d.ID)
	writeJSON(w, http.StatusOK, d.view(now))
}

// register exchanges a six-digit WebUI verification code for a bearer token.
// The previous token of a re-registered device is revoked atomically.
func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	if !s.registration {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		s.fail(w, errf(http.StatusMethodNotAllowed, "method_not_allowed"))
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if err := decode(w, r, &req); err != nil {
		s.fail(w, asAPIError(err))
		return
	}
	code := strings.TrimSpace(req.Code)
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.byCode[code]
	if d == nil {
		s.fail(w, errf(http.StatusNotFound, "invalid_code"))
		return
	}
	now := s.now()
	if !now.Before(d.CodeExpiry) {
		delete(s.byCode, code)
		d.Code = ""
		s.fail(w, errf(http.StatusGone, "code_expired"))
		return
	}
	token, err := randomHex(32)
	if err != nil {
		s.fail(w, errf(http.StatusInternalServerError, "random_failed"))
		return
	}
	if d.Token != "" {
		delete(s.devices, d.Token)
	}
	d.Token = token
	d.Registered = true
	d.Code = ""
	delete(s.byCode, code)
	s.devices[token] = d
	s.log.Printf(`{"at":%q,"event":"device","op":"claim","device":%q,"name":%q}`,
		now.UTC().Format(time.RFC3339Nano), d.ID, d.Name)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]string{"device_id": d.ID, "name": d.Name, "token": token})
}

// --- Profile -------------------------------------------------------------

func (s *Server) profile(w http.ResponseWriter, r *http.Request, d *Device) {
	switch r.Method {
	case http.MethodGet:
		s.getProfile(w, r, d)
	case http.MethodPut:
		s.putProfile(w, r, d)
	case http.MethodPatch:
		s.patchProfile(w, r, d)
	default:
		s.fail(w, errf(http.StatusMethodNotAllowed, "method_not_allowed"))
	}
}

func (s *Server) getProfile(w http.ResponseWriter, r *http.Request, d *Device) {
	if d.Profile == nil {
		s.fail(w, errf(http.StatusNotFound, "profile_required"))
		return
	}
	if r.Header.Get("If-None-Match") == etag(d.Revision) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("ETag", etag(d.Revision))
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(d.Profile)
}

func (s *Server) putProfile(w http.ResponseWriter, r *http.Request, d *Device) {
	var profile v2.Profile
	if err := decode(w, r, &profile); err != nil {
		s.fail(w, asAPIError(err))
		return
	}
	if err := validateProfile(&profile); err != nil {
		s.fail(w, asAPIError(err))
		return
	}
	inm := r.Header.Get("If-None-Match")
	im := r.Header.Get("If-Match")
	if d.Profile != nil {
		// PUT after registration is only a retry of the same profile.
		sameRevision, hasIM := revisionOf(im)
		if !hasIM || sameRevision != d.Revision {
			s.fail(w, errf(http.StatusPreconditionFailed, "profile_conflict"))
			return
		}
		if !profilesEqual(d.Profile, &profile) {
			s.fail(w, errf(http.StatusConflict, "profile_conflict"))
			return
		}
		s.ack(w, d.Revision, "put retry (unchanged)")
		return
	}
	if inm != "*" {
		s.fail(w, errf(http.StatusPreconditionFailed, "profile_conflict"))
		return
	}
	d.Profile = &profile
	d.Revision = 1
	s.ack(w, d.Revision, "put create")
}

func (s *Server) patchProfile(w http.ResponseWriter, r *http.Request, d *Device) {
	if d.Profile == nil {
		s.fail(w, errf(http.StatusNotFound, "profile_required"))
		return
	}
	rev, ok := revisionOf(r.Header.Get("If-Match"))
	if !ok {
		s.fail(w, errf(http.StatusPreconditionFailed, "profile_conflict"))
		return
	}
	if rev != d.Revision {
		s.fail(w, errf(http.StatusPreconditionFailed, "profile_conflict"))
		return
	}
	var patch v2.ProfilePatch
	if err := decode(w, r, &patch); err != nil {
		s.fail(w, asAPIError(err))
		return
	}
	if len(patch) > 2048 {
		s.fail(w, errf(http.StatusRequestEntityTooLarge, "patch_too_large"))
		return
	}
	candidate, err := applyPatch(d.Profile, patch)
	if err != nil {
		s.fail(w, asAPIError(err))
		return
	}
	if err := validateProfile(candidate); err != nil {
		s.fail(w, asAPIError(err))
		return
	}
	if profilesEqual(candidate, d.Profile) {
		s.ack(w, d.Revision, "patch no-op")
		return
	}
	d.Profile = candidate
	d.Revision++
	s.ack(w, d.Revision, "patch")
}

func (s *Server) ack(w http.ResponseWriter, revision uint64, event string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("ETag", etag(revision))
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]uint64{"r": revision})
	s.log.Printf(`{"at":%q,"event":"profile","op":%q,"revision":%d}`,
		s.now().UTC().Format(time.RFC3339Nano), event, revision)
}

func asAPIError(err error) *apiError {
	var e *apiError
	if errors.As(err, &e) {
		return e
	}
	return errf(http.StatusBadRequest, "invalid_json")
}

var batchIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

// --- Samples --------------------------------------------------------------

func (s *Server) samples(w http.ResponseWriter, r *http.Request, d *Device) {
	if r.Method != http.MethodPost {
		s.fail(w, errf(http.StatusMethodNotAllowed, "method_not_allowed"))
		return
	}
	if d.Profile == nil {
		s.fail(w, errf(http.StatusConflict, "profile_required"))
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
	if err != nil || len(raw) > maxBody {
		s.fail(w, errf(http.StatusRequestEntityTooLarge, "batch_too_large"))
		return
	}
	var batch v2.SampleBatch
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&batch); err != nil {
		s.fail(w, errf(http.StatusBadRequest, "invalid_json"))
		return
	}
	if !batchIDPattern.MatchString(batch.ID) {
		s.fail(w, errf(http.StatusBadRequest, "invalid_batch_id"))
		return
	}
	// Idempotency is checked before the active-revision check (doc §6.5).
	hash := sha256.Sum256(raw)
	if record, ok := d.Batches[batch.ID]; ok {
		if record.bodyHash == hash {
			w.WriteHeader(http.StatusNoContent)
			s.log.Printf(`{"at":%q,"event":"samples","op":"duplicate","batch":%q,"samples":%d}`,
				s.now().UTC().Format(time.RFC3339Nano), batch.ID, len(batch.Samples))
			return
		}
		s.fail(w, errf(http.StatusConflict, "batch_id_conflict"))
		return
	}
	if batch.Revision != d.Revision {
		s.fail(w, errf(http.StatusConflict, "profile_outdated"))
		return
	}
	if len(batch.Samples) == 0 || len(batch.Samples) > 30 {
		s.fail(w, errf(http.StatusBadRequest, "invalid_batch_size"))
		return
	}
	serverNow := s.now().Unix()
	for i := range batch.Samples {
		if err := s.validateSample(d, &batch.Samples[i], serverNow); err != nil {
			s.fail(w, asAPIError(err))
			return
		}
	}
	for i := range batch.Samples {
		s.recordSample(d, &batch.Samples[i])
	}
	d.Batches[batch.ID] = batchRecord{bodyHash: hash, at: s.now()}
	if t := batch.Samples[len(batch.Samples)-1].Timestamp; time.Since(time.Unix(t, 0)) < maxStaleness(d.Profile) {
		d.LastSeen = s.now()
	}
	s.recordBatchLocked(d, &batch, raw)
	w.WriteHeader(http.StatusNoContent)
	s.log.Printf(`{"at":%q,"event":"samples","op":"accept","batch":%q,"samples":%d,"revision":%d}`,
		s.now().UTC().Format(time.RFC3339Nano), batch.ID, len(batch.Samples), batch.Revision)
}

func maxStaleness(p *v2.Profile) time.Duration {
	stale := 3 * time.Duration(p.IntervalSeconds) * time.Second
	if stale < 30*time.Second {
		stale = 30 * time.Second
	}
	return stale
}

// selection indexes the profile's enabled declarations by semantic ID.
type selection struct {
	objects map[uint16]bool
}

func selectionsOf(p *v2.Profile) map[string]*selection {
	out := map[string]*selection{}
	for _, sel := range p.Enabled {
		s, ok := out[sel.Metric]
		if !ok {
			s = &selection{objects: map[uint16]bool{}}
			out[sel.Metric] = s
		}
		for _, id := range sel.Objects {
			s.objects[id] = true
		}
	}
	return out
}

func objectIDs(list []v2.ObjectInfo) map[uint16]bool {
	out := map[uint16]bool{}
	for _, o := range list {
		out[o.ID] = true
	}
	return out
}

func objectID[T any](list []T, id func(*T) uint16) map[uint16]bool {
	out := map[uint16]bool{}
	for i := range list {
		out[id(&list[i])] = true
	}
	return out
}

func coreIDs(p *v2.Profile) map[uint16]bool {
	return objectID(p.Inventory.Cores, func(c *v2.CoreInfo) uint16 { return c.ID })
}

func (s *Server) validateSample(d *Device, sm *v2.Sample, serverNow int64) error {
	if sm.Timestamp == 0 {
		return errf(http.StatusUnprocessableEntity, "timestamp_required")
	}
	if sm.Timestamp > serverNow+30 {
		return errf(http.StatusUnprocessableEntity, "timestamp_in_future").withTime()
	}
	if sm.Timestamp < serverNow-86400 {
		return errf(http.StatusUnprocessableEntity, "timestamp_too_old").withTime()
	}
	if sm.CPUUsage == nil || sm.MemoryUsage == nil || sm.Uptime == nil {
		return errf(http.StatusUnprocessableEntity, "required_metric_missing")
	}
	if *sm.CPUUsage > 10000 || *sm.MemoryUsage > 10000 {
		return errf(http.StatusUnprocessableEntity, "metric_out_of_range")
	}
	sel := selectionsOf(d.Profile)
	inv := d.Profile.Inventory
	if mem := sm.Memory; mem != nil {
		// memory.used is an implicit auto-upload field; the rest need selections.
		for _, pair := range []struct {
			selected bool
			set      bool
			name     string
		}{
			{true, mem.Used != nil, "memory.used"},
			{sel["memory.app"] != nil, mem.App != nil, "memory.app"},
			{sel["memory.wired"] != nil, mem.Wired != nil, "memory.wired"},
			{sel["memory.compressed"] != nil, mem.Compressed != nil, "memory.compressed"},
			{sel["memory.swap_total"] != nil, mem.SwapTotal != nil, "memory.swap_total"},
			{sel["memory.swap_used"] != nil, mem.SwapUsed != nil, "memory.swap_used"},
			{sel["memory.pressure"] != nil, mem.Pressure != nil, "memory.pressure"},
		} {
			if pair.set && !pair.selected {
				return errf(http.StatusUnprocessableEntity, "metric_not_enabled:"+pair.name)
			}
		}
	}
	if cpu := sm.CPU; cpu != nil {
		for _, pair := range []struct {
			selected bool
			set      bool
			name     string
		}{
			{sel["cpu.load1"] != nil, cpu.Load1 != nil, "cpu.load1"},
			{sel["cpu.load5"] != nil, cpu.Load5 != nil, "cpu.load5"},
			{sel["cpu.load15"] != nil, cpu.Load15 != nil, "cpu.load15"},
			{sel["cpu.temperature"] != nil, cpu.Temperature != nil, "cpu.temperature"},
		} {
			if pair.set && !pair.selected {
				return errf(http.StatusUnprocessableEntity, "metric_not_enabled:"+pair.name)
			}
		}
	}
	cores := coreIDs(d.Profile)
	for _, core := range sm.Cores {
		if !cores[core.ID] {
			return errf(http.StatusUnprocessableEntity, "metric_not_enabled:core.usage object")
		}
		if sel["core.usage"] == nil || !sel["core.usage"].objects[core.ID] {
			return errf(http.StatusUnprocessableEntity, "metric_not_enabled:core.usage")
		}
	}
	gpus := objectIDs(inv.GPUs)
	for _, gpu := range sm.GPUs {
		if !gpus[gpu.ID] || sel["gpu.usage"] == nil || !sel["gpu.usage"].objects[gpu.ID] {
			return errf(http.StatusUnprocessableEntity, "metric_not_enabled:gpu.usage")
		}
		if gpu.Temperature != nil && (sel["gpu.temperature"] == nil || !sel["gpu.temperature"].objects[gpu.ID]) {
			return errf(http.StatusUnprocessableEntity, "metric_not_enabled:gpu.temperature")
		}
	}
	drives := objectID(inv.Drives, func(dr *v2.DriveInfo) uint16 { return dr.ID })
	for _, drive := range sm.Drives {
		if !drives[drive.ID] {
			return errf(http.StatusUnprocessableEntity, "metric_not_enabled:drive object")
		}
		if (drive.ReadRate != nil && !selectedFor(sel, "drive.read_rate", drive.ID)) ||
			(drive.WriteRate != nil && !selectedFor(sel, "drive.write_rate", drive.ID)) ||
			(drive.Used != nil && !selectedFor(sel, "drive.used", drive.ID)) {
			return errf(http.StatusUnprocessableEntity, "metric_not_enabled:drive rates")
		}
		if drive.SMART != nil {
			if drive.SMART.Timestamp > sm.Timestamp {
				return errf(http.StatusUnprocessableEntity, "timestamp_in_future").withTime()
			}
			if !selectedFor(sel, "drive.smart_health", drive.ID) && drive.SMART.Healthy != nil {
				return errf(http.StatusUnprocessableEntity, "metric_not_enabled:drive.smart_health")
			}
		}
	}
	interfaces := objectID(inv.Interfaces, func(in *v2.InterfaceInfo) uint16 { return in.ID })
	for _, net := range sm.Interfaces {
		if !interfaces[net.ID] {
			return errf(http.StatusUnprocessableEntity, "metric_not_enabled:interface object")
		}
		if (net.Up != nil && !selectedFor(sel, "interface.up", net.ID)) ||
			(net.UploadRate != nil && !selectedFor(sel, "interface.upload_rate", net.ID)) ||
			(net.DownloadRate != nil && !selectedFor(sel, "interface.download_rate", net.ID)) ||
			(net.IPv4 != nil && !selectedFor(sel, "interface.ipv4", net.ID)) {
			return errf(http.StatusUnprocessableEntity, "metric_not_enabled:interface fields")
		}
	}
	if sm.PublicIPv4 != nil {
		if sm.PublicIPv4.Timestamp > sm.Timestamp {
			return errf(http.StatusUnprocessableEntity, "timestamp_in_future").withTime()
		}
		if sel["network.public_ipv4"] == nil {
			return errf(http.StatusUnprocessableEntity, "metric_not_enabled:network.public_ipv4")
		}
	}
	batteries := objectIDs(inv.Batteries)
	for _, bat := range sm.Batteries {
		if !batteries[bat.ID] || !selectedFor(sel, "battery.level", bat.ID) || !selectedFor(sel, "battery.charging", bat.ID) {
			return errf(http.StatusUnprocessableEntity, "metric_not_enabled:battery.level")
		}
		if bat.Level == nil || bat.Charging == nil {
			return errf(http.StatusUnprocessableEntity, "metric_not_enabled:battery pair")
		}
	}
	fans := objectID(inv.Fans, func(f *v2.FanInfo) uint16 { return f.ID })
	for _, fan := range sm.Fans {
		if !fans[fan.ID] || sel["fan.rpm"] == nil || !sel["fan.rpm"].objects[fan.ID] {
			return errf(http.StatusUnprocessableEntity, "metric_not_enabled:fan.rpm")
		}
	}
	if len(sm.Volumes) > 0 {
		return errf(http.StatusUnprocessableEntity, "metric_not_enabled:volume (legacy)")
	}
	return nil
}

func selectedFor(sel map[string]*selection, metric string, id uint16) bool {
	s := sel[metric]
	return s != nil && s.objects[id]
}

func (e *apiError) withTime() *apiError {
	e.serverTime = true
	return e
}

// recordSample folds a validated sample into the log-only latest projection.
func (s *Server) recordSample(d *Device, sm *v2.Sample) {
	put := func(key string, value any) {
		d.Latest[key] = latestValue{Value: fmt.Sprint(value), Time: sm.Timestamp, Revision: d.Revision}
	}
	put("cpu.usage", *sm.CPUUsage)
	put("memory.usage", *sm.MemoryUsage)
	put("system.uptime", *sm.Uptime)
	if sm.Memory != nil {
		if sm.Memory.Used != nil {
			put("memory.used", *sm.Memory.Used)
		}
		if sm.Memory.SwapUsed != nil {
			put("memory.swap_used", *sm.Memory.SwapUsed)
		}
	}
	for _, core := range sm.Cores {
		put(fmt.Sprintf("core.usage[%d]", core.ID), derefU16(core.Usage))
	}
	for _, drive := range sm.Drives {
		if drive.SMART != nil && drive.SMART.Healthy != nil {
			put(fmt.Sprintf("drive.smart_health[%d]", drive.ID), *drive.SMART.Healthy)
		}
	}
	for _, net := range sm.Interfaces {
		if net.IPv4 != nil {
			put(fmt.Sprintf("interface.ipv4[%d]", net.ID), strings.Join(*net.IPv4, ","))
		}
	}
	if sm.PublicIPv4 != nil {
		put("network.public_ipv4", sm.PublicIPv4.Address)
	}
	for _, bat := range sm.Batteries {
		put(fmt.Sprintf("battery.level[%d]", bat.ID), derefU16(bat.Level))
	}
}

func derefU16(v *uint16) string {
	if v == nil {
		return "null"
	}
	return strconv.Itoa(int(*v))
}
