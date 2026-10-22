package app

import (
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yldm-tech/forma/server/internal/auth"
	"github.com/yldm-tech/forma/server/internal/authz"
	"github.com/yldm-tech/forma/server/internal/platform/jobs"
)

// NewJobRegistry is every job kind and schedule `forma worker` runs: the platform's own, then each feature module's.
func NewJobRegistry(pool *pgxpool.Pool, log *slog.Logger, delivery *authz.Delivery, audit *authz.Audit) *jobs.Registry {
	reg := jobs.NewRegistry(log)
	authz.RegisterJobs(reg, delivery, audit)
	auth.RegisterJobs(reg, pool, log)
	return reg
}
