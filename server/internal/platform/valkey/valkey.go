// Package valkey opens the Valkey (Redis protocol) client that holds rate-limit counters.
package valkey

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/yldm-tech/forma/server/internal/platform/config"
)

// Open parses REDIS_URL and pings once. A malformed URL fails the start; an unreachable server only logs a warning, because nothing that depends on Valkey is allowed to fail closed: the rate limiter lets requests through while it is down, and readiness deliberately ignores it (see internal/health). The returned cleanup closes the client.
func Open(ctx context.Context, cfg config.Config, log *slog.Logger) (*redis.Client, func(), error) {
	opts, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		return nil, nil, fmt.Errorf("parse REDIS_URL: %w", err)
	}
	client := redis.NewClient(opts)

	if err := Ping(ctx, client); err != nil {
		log.WarnContext(ctx, "valkey is not reachable; rate limits fail open until it is", "addr", opts.Addr, "err", err)
	}

	cleanup := func() {
		if err := client.Close(); err != nil {
			log.Error("close valkey client", "err", err)
		}
	}
	return client, cleanup, nil
}

// Ping checks the connection with a short timeout. It is a diagnostic, not a readiness gate.
func Ping(ctx context.Context, client *redis.Client) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return client.Ping(ctx).Err()
}
