// Package logger builds the process-wide slog logger.
package logger

import (
	"io"
	"log/slog"

	"github.com/yldm-tech/forma/server/internal/platform/config"
)

// New returns a JSON (or text, for local development) logger at the configured level and installs it as slog's default, so library code that logs through slog lands in the same stream.
func New(cfg config.Config, w io.Writer) *slog.Logger {
	opts := &slog.HandlerOptions{Level: cfg.LogLevel}
	var handler slog.Handler
	if cfg.LogFormat == "text" {
		handler = slog.NewTextHandler(w, opts)
	} else {
		handler = slog.NewJSONHandler(w, opts)
	}
	log := slog.New(handler)
	slog.SetDefault(log)
	return log
}
