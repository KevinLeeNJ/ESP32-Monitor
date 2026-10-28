//go:build darwin

package ipc

import (
	"golang.org/x/sys/unix"
)

// peerUID returns the effective uid of the process on the other end of the
// socket using LOCAL_PEERCRED. The socket is ucred-cached by the kernel, so
// the answer cannot be spoofed by userland.
func peerUID(fd uintptr) (int32, bool) {
	cred, err := unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
	if err != nil || cred == nil {
		return 0, false
	}
	return int32(cred.Uid), true
}
