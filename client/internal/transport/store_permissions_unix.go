//go:build !windows

package transport

import "os"

func secureStoreDirectory(string) error             { return nil }
func privateStorePath(_ string, i os.FileInfo) bool { return i.Mode().Perm()&0077 == 0 }
