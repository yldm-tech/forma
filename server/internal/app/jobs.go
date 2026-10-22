package app

import (
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/server/internal/auth"
	"github.com/yldm-tech/forma/server/internal/authz"
	"github.com/yldm-tech/forma/server/internal/integration"
	"github.com/yldm-tech/forma/server/internal/pipeline"
	"github.com/yldm-tech/forma/server/internal/platform/audit"
	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/platform/jobs"
	"github.com/yldm-tech/forma/server/internal/storage"
	"github.com/yldm-tech/forma/server/internal/survey"
	"github.com/yldm-tech/forma/server/internal/workflow"
	"github.com/yldm-tech/forma/server/internal/workspace"
)

// NewJobRegistry is every job kind and schedule `forma worker` runs: the platform's own, then each feature module's.
func NewJobRegistry(cfg config.Config, pool *pgxpool.Pool, db *gorm.DB, log *slog.Logger, delivery *authz.Delivery, authzAudit *authz.Audit, emitter *audit.Emitter, files *storage.Service, workspaces *workspace.Repository, responsePipeline *pipeline.Worker, workflowRunner *workflow.Runner, workflowReconciler *workflow.Reconciler) *jobs.Registry {
	reg := jobs.NewRegistry(log)
	authz.RegisterJobs(reg, delivery, authzAudit)
	auth.RegisterJobs(reg, pool, log)
	survey.RegisterJobs(reg, cfg, db, emitter, log)
	pipeline.RegisterJobs(reg, responsePipeline)
	workflow.RegisterJobs(reg, workflowRunner, workflowReconciler)
	storage.RegisterJobs(reg, files, workspaces, log)
	return reg
}

// newPipelineHandoffs is the consumers the response pipeline hands a finished response to after its webhooks: the integrations and the workflow runs.
func newPipelineHandoffs(integrations *integration.Deliverer, workflows *workflow.Enqueuer) pipeline.Handoffs {
	return pipeline.Handoffs{Integrations: integrations, Workflows: workflows}
}
