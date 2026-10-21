package api

import (
	"log/slog"
	"net/http"
	"time"

	"esp32-monitor/server/internal/config"
)

// NewServer keeps the production transport settings shared with loopback API tests.
// The caller owns listening, shutdown and the repository lifetime.
func NewServer(c config.Config, repo Repository, now func() time.Time, logger *slog.Logger) *http.Server {
	return &http.Server{
		Addr: c.Listen, Handler: New(c, repo, now, logger),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second,
		WriteTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second,
		MaxHeaderBytes: 16 * 1024,
	}
}
