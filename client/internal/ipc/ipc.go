// Package ipc implements the user-authenticated Unix-socket channel between
// the GUI host and the background Agent. Every connection is validated with
// socket peer credentials: the peer must run as the same OS user as this
// process. No bearer token is required on top of the peer check, and no
// credential-bearing DTO ever crosses this boundary in either direction.
package ipc

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"syscall"
)

const (
	// MaxBody bounds every request body; collector config batches stay far below it.
	MaxBody = 256 << 10
	// SchemaVersion is the IPC contract version reported by Status.
	SchemaVersion = 1
)

// Sentinels mirror the existing application error strings so the GUI can
// branch on the same codes regardless of transport.
var (
	ErrUnauthorized      = errors.New("unauthorized")
	ErrNotFound          = errors.New("not_found")
	ErrAlreadyRunning    = errors.New("ipc_already_owned")
	ErrPeerCheckRequired = errors.New("ipc_peer_unavailable")
)

type errorReply struct {
	ErrorCode string `json:"error_code"`
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	_ = enc.Encode(value)
}

func writeError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, errorReply{ErrorCode: code})
}

func writeAppError(w http.ResponseWriter, err error) {
	code := err.Error()
	switch {
	case code == "companion_disabled" || code == "collector_disabled":
		writeError(w, http.StatusConflict, code)
	case code == "unauthorized" || code == "ipc_already_owned":
		writeError(w, http.StatusUnauthorized, code)
	case code == "not_found":
		writeError(w, http.StatusNotFound, code)
	default:
		writeError(w, http.StatusUnprocessableEntity, code)
	}
}

// decodeBody parses a strict JSON body; unknown fields and trailing data fail.
func decodeBody(r *http.Request, target any) error {
	if r.Body == nil {
		return errors.New("empty_body")
	}
	defer r.Body.Close()
	dec := json.NewDecoder(io.LimitReader(r.Body, MaxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		return fmt.Errorf("invalid_json")
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		return fmt.Errorf("trailing_json")
	}
	return nil
}

// unixPeerUID resolves the authenticated peer user id for a Unix socket
// connection. darwin and linux implement it with their native APIs. Control
// keeps the connection non-blocking; File().Fd() would switch it to blocking
// mode and break the HTTP server deadlines.
func unixPeerUID(conn net.Conn) (int32, bool) {
	sysconn, ok := conn.(syscall.Conn)
	if !ok {
		return 0, false
	}
	raw, err := sysconn.SyscallConn()
	if err != nil {
		return 0, false
	}
	var uid int32
	var resolved bool
	if ctlErr := raw.Control(func(fd uintptr) { uid, resolved = peerUID(fd) }); ctlErr != nil {
		return 0, false
	}
	return uid, resolved
}
