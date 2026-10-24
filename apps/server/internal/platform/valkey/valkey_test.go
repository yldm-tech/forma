package valkey_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/valkey"
	"github.com/yldm-tech/forma/apps/server/internal/platform/valkey/valkeytest"
)

func TestOpen(t *testing.T) {
	var logs bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logs, nil))

	client, cleanup, err := valkey.Open(context.Background(), config.Config{RedisURL: valkeytest.URL(t)}, log)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer cleanup()
	if err := valkey.Ping(context.Background(), client); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if logs.Len() != 0 {
		t.Fatalf("a reachable server logged: %s", logs.String())
	}
}

// Valkey being down must not stop the process: rate limits fail open until it is back.
func TestOpenToleratesAnUnreachableServer(t *testing.T) {
	var logs bytes.Buffer
	_, cleanup, err := valkey.Open(context.Background(), config.Config{RedisURL: "redis://127.0.0.1:1/0?dial_timeout=200ms"}, slog.New(slog.NewTextHandler(&logs, nil)))
	if err != nil {
		t.Fatalf("Open failed for an unreachable server: %v", err)
	}
	cleanup()
	if !strings.Contains(logs.String(), "valkey is not reachable") {
		t.Fatalf("no warning logged: %s", logs.String())
	}
}

func TestOpenRejectsAMalformedURL(t *testing.T) {
	if _, _, err := valkey.Open(context.Background(), config.Config{RedisURL: "http://localhost:6379"}, slog.New(slog.DiscardHandler)); err == nil {
		t.Fatal("Open accepted a non-redis URL")
	}
}
