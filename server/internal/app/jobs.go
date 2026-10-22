package app

import (
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/server/internal/auth"
	"github.com/yldm-tech/forma/server/internal/authz"
	"github.com/yldm-tech/forma/server/internal/platform/audit"
	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/platform/jobs"
	"github.com/yldm-tech/forma/server/internal/response"
	"github.com/yldm-tech/forma/server/internal/survey"
)

// NewJobRegistry is every job kind and schedule `forma worker` runs: the platform's own, then each feature module's.
func NewJobRegistry(cfg config.Config, pool *pgxpool.Pool, db *gorm.DB, log *slog.Logger, delivery *authz.Delivery, authzAudit *authz.Audit, emitter *audit.Emitter) *jobs.Registry {
	reg := jobs.NewRegistry(log)
	authz.RegisterJobs(reg, delivery, authzAudit)
	auth.RegisterJobs(reg, pool, log)
	survey.RegisterJobs(reg, cfg, db, emitter, log)
	response.RegisterJobs(reg, log)
	return reg
}
