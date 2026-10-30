package logging

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

var errLogClosed = errors.New("log_closed")

// RotatingWriter appends complete log writes to a size-bounded file and keeps
// a fixed number of older files. Total disk use is (backups+1)*maxBytes.
type RotatingWriter struct {
	mu       sync.Mutex
	path     string
	maxBytes int64
	backups  int
	file     *os.File
	size     int64
	closed   bool
}

func NewRotatingWriter(dir, name string, maxBytes int64, backups int) (*RotatingWriter, error) {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	if backups < 0 {
		backups = 0
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	w := &RotatingWriter{path: filepath.Join(dir, name+".log"), maxBytes: maxBytes, backups: backups}
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.openLocked(); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *RotatingWriter) openLocked() error {
	file, err := os.OpenFile(w.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return err
	}
	w.file = file
	w.size = info.Size()
	return nil
}

func (w *RotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return 0, errLogClosed
	}
	if w.file == nil {
		if err := w.openLocked(); err != nil {
			return 0, err
		}
	}
	if w.size > 0 && w.size+int64(len(p)) > w.maxBytes {
		w.rotateLocked()
	}
	if w.file == nil {
		return 0, errors.New("log_file_unavailable")
	}
	n, err := w.file.Write(p)
	w.size += int64(n)
	return n, err
}

func (w *RotatingWriter) rotateLocked() {
	if w.file != nil {
		_ = w.file.Close()
		w.file = nil
	}
	if w.backups == 0 {
		_ = os.Remove(w.path)
		_ = w.openLocked()
		return
	}
	_ = os.Remove(fmt.Sprintf("%s.%d", w.path, w.backups))
	for i := w.backups - 1; i >= 1; i-- {
		_ = os.Rename(fmt.Sprintf("%s.%d", w.path, i), fmt.Sprintf("%s.%d", w.path, i+1))
	}
	_ = os.Rename(w.path, w.path+".1")
	_ = w.openLocked()
}

func (w *RotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}
