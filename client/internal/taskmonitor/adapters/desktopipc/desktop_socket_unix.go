//go:build darwin || linux

package desktopipc

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

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
