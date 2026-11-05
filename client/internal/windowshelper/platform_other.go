//go:build !windows

package windowshelper

// Other platforms retain their existing native collectors and music adapters.
func NewPlatform() *Client { return nil }
