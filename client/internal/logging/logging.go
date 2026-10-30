// Package logging owns the bounded rotating logs written by the GUI host and
// the Agent. Both processes append to separate streams in the same private
// directory so a single export can cover the whole client.
package logging

import (
	"io"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"sync/atomic"

	"esp32-monitor/client/internal/platform"
)

const (
	// DefaultMaxBytes is the size at which one log file rotates.
	DefaultMaxBytes int64 = 2 << 20
	// DefaultBackups is the number of rotated files kept per stream.
	DefaultBackups = 5
)

type outputHolder struct{ writer io.Writer }

var (
	defaultLogger atomic.Pointer[slog.Logger]
	defaultOutput atomic.Pointer[outputHolder]
)

func init() {
	defaultLogger.Store(slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError})))
	defaultOutput.Store(&outputHolder{writer: io.Discard})
}

// L returns a logger tagged with a component. Before Setup it discards output
// so tests and one-shot library use never write files implicitly.
func L(component string) *slog.Logger {
	return defaultLogger.Load().With("component", component)
}

// Level returns a component logger with a stricter threshold for libraries
// whose info-level chatter would drown out lifecycle events.
func Level(component string, level slog.Level) *slog.Logger {
	holder := defaultOutput.Load()
	return slog.New(slog.NewTextHandler(holder.writer, &slog.HandlerOptions{Level: level})).With("component", component)
}

// SetDefault installs the process logger used by L and slog.Default.
func SetDefault(logger *slog.Logger) {
	defaultLogger.Store(logger)
	slog.SetDefault(logger)
}

// Ref shortens opaque device or session identifiers so separate log lines
// stay correlatable without persisting the full value.
func Ref(value string) string {
	if len(value) <= 8 {
		return value
	}
	return value[:8]
}

// Dir returns the private directory that holds client.log and agent.log.
func Dir() (string, error) {
	base, err := platform.AppDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "logs"), nil
}

// IsTerminal reports whether a standard stream is attached to a character
// device, so file logging stays quiet for Finder/Explorer launches.
func IsTerminal(file *os.File) bool {
	if file == nil {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// Options selects one process log stream.
type Options struct {
	Name       string // file stem: client or agent
	AlsoStderr bool   // keep terminal output when launched from a shell
}

// Handle owns the rotating writer created by Setup.
type Handle struct {
	Writer io.Writer
	close  func() error
}

func (h *Handle) Close() error {
	if h == nil || h.close == nil {
		return nil
	}
	return h.close()
}

// Setup installs a rotating text logger for one process and returns the writer
// so flag errors and child-process stderr can share the same stream.
func Setup(opts Options) (*Handle, error) {
	if opts.Name == "" {
		opts.Name = "client"
	}
	dir, err := Dir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	writer, err := NewRotatingWriter(dir, opts.Name, DefaultMaxBytes, DefaultBackups)
	if err != nil {
		return nil, err
	}
	var out io.Writer = writer
	if opts.AlsoStderr {
		out = io.MultiWriter(os.Stderr, writer)
	}
	logger := slog.New(slog.NewTextHandler(out, &slog.HandlerOptions{Level: slog.LevelInfo}))
	SetDefault(logger)
	defaultOutput.Store(&outputHolder{writer: out})
	log.SetOutput(out)
	logger.Info("log_ready", "file", filepath.Join(dir, opts.Name+".log"), "max_bytes", DefaultMaxBytes, "backups", DefaultBackups)
	return &Handle{Writer: out, close: writer.Close}, nil
}
