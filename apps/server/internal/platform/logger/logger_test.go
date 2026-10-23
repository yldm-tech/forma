package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
)

func decode(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("log line %q: %v", line, err)
		}
		out = append(out, m)
	}
	return out
}

// Level labels are forma2's pino names in lower case, so a filter on level == "audit" (the audit logging guide's) or level == "error" written for forma2 keeps matching.
func TestJSONLevelLabels(t *testing.T) {
	var buf bytes.Buffer
	log := New(config.Config{LogLevel: config.LogLevel(slog.LevelDebug), LogFormat: "json"}, &buf)
	ctx := context.Background()
	log.DebugContext(ctx, "d")
	log.InfoContext(ctx, "i")
	log.WarnContext(ctx, "w")
	log.ErrorContext(ctx, "e")
	log.Log(ctx, config.LevelFatal, "f")
	log.LogAttrs(ctx, LevelAudit, "audit", slog.String("action", "created"))
	log.Log(ctx, slog.LevelInfo+2, "between")

	got := decode(t, &buf)
	want := []string{"debug", "info", "warn", "error", "fatal", "audit", "info+2"}
	if len(got) != len(want) {
		t.Fatalf("got %d lines, want %d: %v", len(got), len(want), got)
	}
	for i, level := range want {
		if got[i]["level"] != level {
			t.Errorf("line %d level = %v, want %q", i, got[i]["level"], level)
		}
		if _, ok := got[i]["time"].(string); !ok {
			t.Errorf("line %d has no time: %v", i, got[i])
		}
	}
	if got[5]["msg"] != "audit" || got[5]["action"] != "created" {
		t.Errorf("audit line = %v", got[5])
	}
}

func TestTextLevelLabels(t *testing.T) {
	var buf bytes.Buffer
	log := New(config.Config{LogLevel: config.LogLevel(slog.LevelInfo), LogFormat: "text"}, &buf)
	log.Info("started")
	log.Log(context.Background(), LevelAudit, "audit")
	out := buf.String()
	if !strings.Contains(out, "level=info msg=started") || !strings.Contains(out, "level=audit msg=audit") {
		t.Fatalf("text output %q, want lowercase level labels", out)
	}
}

// LOG_LEVEL=fatal keeps only what is above error and LOG_LEVEL=audit only audit events, as forma2's levels did.
func TestLevelThresholds(t *testing.T) {
	for _, tc := range []struct {
		level config.LogLevel
		want  []string
	}{
		{config.LogLevel(slog.LevelWarn), []string{"warn", "error", "audit"}},
		{config.LogLevel(config.LevelFatal), []string{"audit"}},
		{config.LogLevel(config.LevelAudit), []string{"audit"}},
	} {
		var buf bytes.Buffer
		log := New(config.Config{LogLevel: tc.level, LogFormat: "json"}, &buf)
		log.Info("i")
		log.Warn("w")
		log.Error("e")
		log.Log(context.Background(), LevelAudit, "audit")
		var levels []string
		for _, line := range decode(t, &buf) {
			levels = append(levels, line["level"].(string))
		}
		if strings.Join(levels, ",") != strings.Join(tc.want, ",") {
			t.Errorf("LOG_LEVEL %v wrote %v, want %v", tc.level.Level(), levels, tc.want)
		}
	}
}

// Deprecations found while reading the configuration are reported once, when the logger is built.
func TestNewWritesConfigurationWarnings(t *testing.T) {
	var buf bytes.Buffer
	New(config.Config{LogLevel: config.LogLevel(slog.LevelInfo), LogFormat: "json", Warnings: []string{"BULLMQ_WORKER_CONCURRENCY and BULLMQ_WORKER_COUNT are deprecated; set WORKER_CONCURRENCY=6 instead"}}, &buf)
	got := decode(t, &buf)
	if len(got) != 1 || got[0]["level"] != "warn" || !strings.Contains(got[0]["msg"].(string), "WORKER_CONCURRENCY=6") {
		t.Fatalf("logged %v, want the one warning", got)
	}
}
