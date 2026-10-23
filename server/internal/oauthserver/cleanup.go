package oauthserver

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/yldm-tech/forma/server/internal/platform/jobs"
)

// cleanupInterval is how often expired authorization requests and tokens are deleted. Every read checks expiry itself, so this only keeps the tables small. A rotated refresh token is kept until it expires, so presenting it again is still recognised as a replay.
const cleanupInterval = time.Hour

// CleanupArgs is the periodic job that deletes the authorization server's expired rows.
type CleanupArgs struct{}

func (CleanupArgs) Kind() string { return "oauth.cleanup-expired" }

type CleanupWorker struct {
	river.WorkerDefaults[CleanupArgs]
	pool *pgxpool.Pool
	log  *slog.Logger
}

func (w *CleanupWorker) Work(ctx context.Context, _ *river.Job[CleanupArgs]) error {
	counts := map[string]int64{}
	for _, table := range []string{"oauth_authorization_requests", "oauth_access_tokens", "oauth_refresh_tokens"} {
		tag, err := w.pool.Exec(ctx, `DELETE FROM `+table+` WHERE expires_at < now()`)
		if err != nil {
			return fmt.Errorf("delete expired %s: %w", table, err)
		}
		counts[table] = tag.RowsAffected()
	}
	w.log.InfoContext(ctx, "Deleted expired OAuth authorization requests and tokens",
		"authorization_requests", counts["oauth_authorization_requests"], "access_tokens", counts["oauth_access_tokens"], "refresh_tokens", counts["oauth_refresh_tokens"])
	return nil
}

// RegisterJobs adds the module's background work to `forma worker`.
func RegisterJobs(reg *jobs.Registry, pool *pgxpool.Pool, log *slog.Logger) {
	jobs.Add(reg, &CleanupWorker{pool: pool, log: log})
	reg.AddPeriodic(river.NewPeriodicJob(river.PeriodicInterval(cleanupInterval), func() (river.JobArgs, *river.InsertOpts) {
		return CleanupArgs{}, nil
	}, &river.PeriodicJobOpts{RunOnStart: true}))
}
