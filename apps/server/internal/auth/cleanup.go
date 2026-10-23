package auth

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/yldm-tech/forma/server/internal/platform/jobs"
)

// cleanupInterval is how often expired sessions and tokens are deleted. Nothing depends on the deletion: every read checks expiry itself, so this only keeps the tables small.
const cleanupInterval = time.Hour

// CleanupArgs is the periodic job that deletes expired sessions and single-use tokens. forma2 kept both in Redis, where they expired by TTL.
type CleanupArgs struct{}

func (CleanupArgs) Kind() string { return "auth.cleanup-expired" }

type CleanupWorker struct {
	river.WorkerDefaults[CleanupArgs]
	pool *pgxpool.Pool
	log  *slog.Logger
}

func (w *CleanupWorker) Work(ctx context.Context, _ *river.Job[CleanupArgs]) error {
	sessions, err := w.pool.Exec(ctx, `DELETE FROM sessions WHERE expires_at < now()`)
	if err != nil {
		return fmt.Errorf("delete expired sessions: %w", err)
	}
	tokens, err := w.pool.Exec(ctx, `DELETE FROM verifications WHERE expires_at < now()`)
	if err != nil {
		return fmt.Errorf("delete expired tokens: %w", err)
	}
	w.log.InfoContext(ctx, "Deleted expired sessions and tokens", "sessions", sessions.RowsAffected(), "tokens", tokens.RowsAffected())
	return nil
}

// RegisterJobs adds the module's background work to `forma worker`.
func RegisterJobs(reg *jobs.Registry, pool *pgxpool.Pool, log *slog.Logger) {
	jobs.Add(reg, &CleanupWorker{pool: pool, log: log})
	reg.AddPeriodic(river.NewPeriodicJob(river.PeriodicInterval(cleanupInterval), func() (river.JobArgs, *river.InsertOpts) {
		return CleanupArgs{}, nil
	}, &river.PeriodicJobOpts{RunOnStart: true}))
}
