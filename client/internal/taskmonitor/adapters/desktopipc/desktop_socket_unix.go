//go:build darwin || linux

package desktopipc

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

func desktopEndpoint(home string) (string, string) {
	if !filepath.IsAbs(home) {
		return "", "desktop_home_must_be_absolute"
	}
	return filepath.Join(home, "ipc", "ipc.sock"), ""
}

func validateDesktopSocket(path string) error {
	file, err := os.Lstat(path)
	if err != nil {
		return errors.New("desktop_socket_unavailable")
	}
	dir, err := os.Lstat(filepath.Dir(path))
	if err != nil {
		return errors.New("desktop_socket_unavailable")
	}
	fs, ok := file.Sys().(*syscall.Stat_t)
	ds, okDir := dir.Sys().(*syscall.Stat_t)
	if !ok || !okDir || file.Mode()&os.ModeSocket == 0 || !dir.IsDir() || int(fs.Uid) != os.Getuid() || int(ds.Uid) != os.Getuid() || dir.Mode().Perm()&0022 != 0 || file.Mode().Perm()&0022 != 0 {
		return errors.New("desktop_socket_permission_denied")
	}
	return nil
}

func connectDesktop(ctx context.Context, path string) (net.Conn, error) {
	if err := validateDesktopSocket(path); err != nil {
		return nil, err
	}
	dialer := net.Dialer{Timeout: 3 * time.Second}
	conn, err := dialer.DialContext(ctx, "unix", path)
	if err != nil {
		return nil, errors.New("desktop_socket_unavailable")
	}
	return conn, nil
}
