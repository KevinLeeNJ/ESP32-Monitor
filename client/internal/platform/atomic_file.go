package platform

import (
	"os"
	"path/filepath"
)

// WriteFileAtomic writes data to dir/name through a synced temporary file and
// an atomic rename, so readers never observe a partial record.
func WriteFileAtomic(dir, pattern, name string, data []byte, perm os.FileMode) error {
	f, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return err
	}
	temp := filepath.Base(f.Name())
	defer os.Remove(filepath.Join(dir, temp))
	if err = writeSyncedFile(f, data, perm); err != nil {
		return err
	}
	return os.Rename(filepath.Join(dir, temp), filepath.Join(dir, name))
}

func writeSyncedFile(f *os.File, data []byte, perm os.FileMode) error {
	if err := f.Chmod(perm); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
