package app

import (
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yldm-tech/forma/server/internal/auth"
	"github.com/yldm-tech/forma/server/internal/platform/jobs"
)

// NewJobRegistry is the set of jobs `forma worker` runs: the platform's own plus every feature module's.
func NewJobRegistry(pool *pgxpool.Pool, log *slog.Logger) *jobs.Registry {
	reg := jobs.NewRegistry(log)
	auth.RegisterJobs(reg, pool, log)
	return reg
}
