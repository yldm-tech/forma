// Package logger builds the process-wide slog logger.
package logger

import (
	"io"
	"log/slog"

	"github.com/yldm-tech/forma/server/internal/platform/config"
)

// LevelAudit is the level audit events are written at. It sits above every other level, so LOG_LEVEL never filters audit events out, and it is labelled "AUDIT" so a log pipeline can route them apart from application logs, as forma2's dedicated pino level did.
const LevelAudit = slog.Level(12)

// New returns a JSON (or text, for local development) logger at the configured level and installs it as slog's default, so library code that logs through slog lands in the same stream.
func New(cfg config.Config, w io.Writer) *slog.Logger {
	opts := &slog.HandlerOptions{Level: cfg.LogLevel, ReplaceAttr: labelAudit}
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

func labelAudit(groups []string, a slog.Attr) slog.Attr {
	if len(groups) == 0 && a.Key == slog.LevelKey {
		if level, ok := a.Value.Any().(slog.Level); ok && level == LevelAudit {
			a.Value = slog.StringValue("AUDIT")
		}
	}
	return a
}
