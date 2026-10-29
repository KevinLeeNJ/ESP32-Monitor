//go:build darwin || linux

package ipc

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

func validateIPCPath(path string) error {
	if !filepath.IsAbs(path) {
		return errors.New("ipc_socket_path_must_be_absolute")
	}
	// darwin truncates sun_path at 104 bytes; keep margin for the file name.
	if len(path) > 100 {
		return errors.New("ipc_socket_path_too_long")
	}
	return nil
}

// listenIPC removes a stale socket file, binds the socket and restricts it to
// the current user. A live socket means another Agent owns the endpoint.
func listenIPC(path string) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if conn, err := net.DialTimeout("unix", path, 200*time.Millisecond); err == nil {
		_ = conn.Close()
		return nil, ErrAlreadyRunning
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = listener.Close()
		return nil, err
	}
	return listener, nil
}

func closeIPCListener(listener net.Listener, path string) error {
	err := listener.Close()
	if rmErr := os.Remove(path); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
		err = errors.Join(err, rmErr)
	}
	return err
}

func dialIPC(ctx context.Context, path string) (net.Conn, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
	}
	var dialer net.Dialer
	return dialer.DialContext(ctx, "unix", path)
}

func ipcPeerOK(conn net.Conn) bool {
	uid, ok := unixPeerUID(conn)
	return ok && uid == int32(os.Getuid())
}

func ipcServerPeerOK(conn net.Conn) bool { return ipcPeerOK(conn) }

// unixPeerUID resolves the authenticated peer user id for a Unix socket
// connection. Control keeps the connection non-blocking; File().Fd() would
// switch it to blocking mode and break the HTTP server deadlines.
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
