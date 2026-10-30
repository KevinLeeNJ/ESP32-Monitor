//go:build linux

package ipc

import (
	"golang.org/x/sys/unix"
)

// peerUID returns the effective uid of the process on the other end of the
// socket using SO_PEERCRED.
func peerUID(fd uintptr) (int32, bool) {
	cred, err := unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	if err != nil || cred == nil {
		return 0, false
	}
	return int32(cred.Uid), true
}
