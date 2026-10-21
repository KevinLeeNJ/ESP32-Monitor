package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"esp32-monitor/server/internal/api"
	"esp32-monitor/server/internal/cli"
	"esp32-monitor/server/internal/config"
	"esp32-monitor/server/internal/storage"
	"golang.org/x/term"
)

func main() {
	if err := cli.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd())), serve); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func serve(c config.Config) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	ids := make([]string, len(c.Agents))
	for i, a := range c.Agents {
		ids[i] = a.ID
	}
	repo, err := storage.Open(ctx, c.Database, ids, time.Now().Unix())
	if err != nil {
		return errors.New("cannot initialize SQLite; check database path, permissions and schema")
	}
	defer repo.Close()
	server := api.NewServer(c, repo, time.Now, logger)
	done := make(chan error, 1)
	go func() { done <- server.ListenAndServe() }()
	logger.Info("server starting", "listen", c.Listen)
	logDatabaseSize(c.Database, logger)
	timer := time.NewTicker(time.Hour)
	defer timer.Stop()
	for {
		select {
		case err := <-done:
			if errors.Is(err, http.ErrServerClosed) {
				return nil
			}
			return errors.New("HTTP server stopped unexpectedly; check listen address")
		case <-ctx.Done():
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err = server.Shutdown(shutdownCtx); err != nil {
				_ = server.Close()
				return errors.New("HTTP shutdown deadline exceeded")
			}
			return nil
		case now := <-timer.C:
			// Delete in bounded batches and check cancellation between batches.
			logDatabaseSize(c.Database, logger)
			if err = cleanup(ctx, repo, now.Unix()-c.HistoryRetentionHours*3600, logger); err != nil {
				logger.Error("history cleanup failed", "reason", "storage_unavailable")
			}
		}
	}
}
func cleanup(ctx context.Context, repo *storage.Store, cutoff int64, logger *slog.Logger) error {
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := repo.Cleanup(ctx, cutoff)
		if err != nil {
			return err
		}
		total += n
		if n < storage.CleanupBatch {
			break
		}
	}
	logger.Info("history cleanup", "deleted_rows", total)
	return nil
}

func logDatabaseSize(path string, logger *slog.Logger) {
	var databaseBytes, walBytes int64
	if st, err := os.Stat(path); err == nil {
		databaseBytes = st.Size()
	}
	if st, err := os.Stat(path + "-wal"); err == nil {
		walBytes = st.Size()
	}
	logger.Info("database size", "database_bytes", databaseBytes, "wal_bytes", walBytes)
}
