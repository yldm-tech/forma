package app

import (
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/server/internal/auth"
	"github.com/yldm-tech/forma/server/internal/authz"
	"github.com/yldm-tech/forma/server/internal/billing"
	"github.com/yldm-tech/forma/server/internal/brevo"
	"github.com/yldm-tech/forma/server/internal/integration"
	"github.com/yldm-tech/forma/server/internal/oauthserver"
	"github.com/yldm-tech/forma/server/internal/pipeline"
	"github.com/yldm-tech/forma/server/internal/platform/audit"
	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/platform/jobs"
	"github.com/yldm-tech/forma/server/internal/retention"
	"github.com/yldm-tech/forma/server/internal/storage"
	"github.com/yldm-tech/forma/server/internal/survey"
	"github.com/yldm-tech/forma/server/internal/usagereport"
	"github.com/yldm-tech/forma/server/internal/workflow"
	"github.com/yldm-tech/forma/server/internal/workspace"
)

// NewJobRegistry is every job kind and schedule `forma worker` runs: the platform's own, then each feature module's.
func NewJobRegistry(cfg config.Config, pool *pgxpool.Pool, db *gorm.DB, log *slog.Logger, delivery *authz.Delivery, authzAudit *authz.Audit, emitter *audit.Emitter, files *storage.Service, workspaces *workspace.Repository, responsePipeline *pipeline.Worker, workflowRunner *workflow.Runner, workflowReconciler *workflow.Reconciler, billingEngine *billing.Engine, usageReporter *usagereport.Reporter, brevoClient *brevo.Client) *jobs.Registry {
	reg := jobs.NewRegistry(log)
	authz.RegisterJobs(reg, delivery, authzAudit)
	auth.RegisterJobs(reg, pool, log)
	oauthserver.RegisterJobs(reg, pool, log)
	survey.RegisterJobs(reg, cfg, db, emitter, log)
	pipeline.RegisterJobs(reg, responsePipeline)
	workflow.RegisterJobs(reg, workflowRunner, workflowReconciler)
	storage.RegisterJobs(reg, files, workspaces, log)
	billing.RegisterJobs(reg, billingEngine)
	retention.RegisterJobs(reg, db, log)
	usagereport.RegisterJobs(reg, usageReporter, log)
	workflow.RegisterUsageSnapshot(reg, workflow.NewUsageSnapshot(cfg, db, log), log)
	brevo.RegisterJobs(reg, brevoClient)
	return reg
}

// newPipelineHandoffs is the consumers the response pipeline hands a response to after its webhooks: the integrations and the workflow runs of a finished one, and the Stripe meter and the usage update of a created one.
func newPipelineHandoffs(integrations *integration.Deliverer, workflows *workflow.Enqueuer, meter pipeline.UsageMeter, usageReporter *usagereport.Reporter) pipeline.Handoffs {
	return pipeline.Handoffs{Integrations: integrations, Workflows: workflows, Meter: meter, UsageReport: usageReporter}
}
