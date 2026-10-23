// Package logger builds the process-wide slog logger.
package logger

import (
	"io"
	"log/slog"
	"strings"

	"github.com/yldm-tech/forma/server/internal/platform/config"
)

// LevelAudit is the level audit events are written at. It sits above every other level, so no LOG_LEVEL but audit's own filters audit events out, and it is labelled "audit" so a log pipeline can route them apart from application logs, as forma2's dedicated pino level did.
const LevelAudit = config.LevelAudit

// New returns a JSON (or text, for local development) logger at the configured level and installs it as slog's default, so library code that logs through slog lands in the same stream. Level labels are forma2's lowercase pino names (debug, info, warn, error, fatal, audit). It then writes the configuration's deprecation warnings, once per process.
func New(cfg config.Config, w io.Writer) *slog.Logger {
	opts := &slog.HandlerOptions{Level: cfg.LogLevel, ReplaceAttr: labelLevel}
	var handler slog.Handler
	if cfg.LogFormat == "text" {
		handler = slog.NewTextHandler(w, opts)
	} else {
		handler = slog.NewJSONHandler(w, opts)
	}
	log := slog.New(handler)
	slog.SetDefault(log)
	for _, warning := range cfg.Warnings {
		log.Warn(warning)
	}
	return log
}

// labelLevel writes the level as forma2's pino formatter did, a lowercase label, so a filter written for forma2's logs (level == "audit", level == "error") keeps matching.
func labelLevel(groups []string, a slog.Attr) slog.Attr {
	if len(groups) > 0 || a.Key != slog.LevelKey {
		return a
	}
	level, ok := a.Value.Any().(slog.Level)
	if !ok {
		return a
	}
	switch level {
	case LevelAudit:
		a.Value = slog.StringValue("audit")
	case config.LevelFatal:
		a.Value = slog.StringValue("fatal")
	default:
		a.Value = slog.StringValue(strings.ToLower(level.String()))
	}
	return a
}
