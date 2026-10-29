//go:build !darwin && !linux && !windows

package ipc

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
)

func validateIPCPath(path string) error {
	if !filepath.IsAbs(path) {
		return errors.New("ipc_socket_path_must_be_absolute")
	}
	if len(path) > 100 {
		return errors.New("ipc_socket_path_too_long")
	}
	return nil
}

func listenIPC(path string) (net.Listener, error) { return nil, ErrPeerCheckRequired }

func closeIPCListener(listener net.Listener, path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func dialIPC(ctx context.Context, path string) (net.Conn, error) {
	return nil, ErrPeerCheckRequired
}

func ipcPeerOK(conn net.Conn) bool { return false }

func ipcServerPeerOK(conn net.Conn) bool { return false }
