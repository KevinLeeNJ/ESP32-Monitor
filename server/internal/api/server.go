package api

import (
	"esp32-monitor/server/internal/config"
	"esp32-monitor/server/internal/storage"
	"io"
	"log/slog"
	"net/http"
	"time"
)

func NewServer(c config.Config, repo *storage.Store, now func() time.Time, logger *slog.Logger, secrets io.Writer) (*http.Server, error) {
	h, err := New(c, repo, now, logger, secrets)
	if err != nil {
		return nil, err
	}
	return &http.Server{Addr: c.Listen, Handler: h, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 * 1024}, nil
}
