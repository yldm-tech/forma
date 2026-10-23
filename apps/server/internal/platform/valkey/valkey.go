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

// KeyPrefix starts every key this server owns alone: one whose meaning or value differs from what forma2 stores under the same name. forma2 and this server share one Valkey during the cutover and for as long as a rollback to forma2 stays possible, and forma2 never writes a key under this prefix: its cache keys all start with "fb:" (packages/cache, makeCacheKey), its job queue with "{forma:jobs}", and Better Auth's secondary storage uses its own unprefixed names. A key that keeps forma2's "fb:" name must keep forma2's meaning and value format with it, as the rate-limit counters (internal/platform/ratelimit), the sign-in failure throttle (internal/auth) and the usage report's lock and last-sent time (internal/usagereport) do; those are shared on purpose, so both servers count against one budget and report once. Keys built from this server's UUIDs ("fb:env:<workspace uuid>:state" and the like) keep forma2's names too: forma2's ids are cuids, so the two never build the same key.
const KeyPrefix = "forma:"

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
