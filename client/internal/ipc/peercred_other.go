//go:build !darwin && !linux

package ipc

// peerUID is unavailable without a peer-credential socket API; the IPC layer
// refuses connections rather than serving unauthenticated callers.
func peerUID(fd uintptr) (int32, bool) {
	return 0, false
}
