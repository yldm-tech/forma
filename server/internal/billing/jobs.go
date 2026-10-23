package billing

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/platform/jobs"
)

// SetupArgs is the billing.setup-organization job: forma2's ensureCloudStripeSetupForOrganization, which it ran fire-and-forget after every organization it created. As a job it is retried when Stripe fails.
type SetupArgs struct {
	OrganizationID uuid.UUID `json:"organizationId"`
}

func (SetupArgs) Kind() string { return "billing.setup-organization" }

// CleanupArgs is the billing.cleanup-customer job: forma2's cleanupStripeCustomer for a deleted organization's customer.
type CleanupArgs struct {
	StripeCustomerID string `json:"stripeCustomerId"`
}

func (CleanupArgs) Kind() string { return "billing.cleanup-customer" }

// jobTimeout bounds one attempt, a handful of Stripe round trips.
const jobTimeout = 2 * time.Minute

type setupWorker struct {
	river.WorkerDefaults[SetupArgs]
	engine *Engine
}

func (w *setupWorker) Timeout(*river.Job[SetupArgs]) time.Duration { return jobTimeout }

func (w *setupWorker) Work(ctx context.Context, job *river.Job[SetupArgs]) error {
	return w.engine.EnsureSetup(ctx, job.Args.OrganizationID)
}

type cleanupWorker struct {
	river.WorkerDefaults[CleanupArgs]
	engine *Engine
}

func (w *cleanupWorker) Timeout(*river.Job[CleanupArgs]) time.Duration { return jobTimeout }

func (w *cleanupWorker) Work(ctx context.Context, job *river.Job[CleanupArgs]) error {
	return w.engine.CleanupCustomer(ctx, job.Args.StripeCustomerID)
}

// RegisterJobs adds billing's job kinds to `forma worker`.
func RegisterJobs(reg *jobs.Registry, engine *Engine) {
	jobs.Add(reg, &setupWorker{engine: engine})
	jobs.Add(reg, &cleanupWorker{engine: engine})
}

// Lifecycle is what the organization's lifecycle tells billing: a new organization gets its Stripe customer and Hobby subscription, a deleted one has its subscriptions cancelled. Both run as jobs on `forma worker`, enqueued after the change commits, as forma2 ran them after its writes; both do nothing unless billing is enabled. An enqueue that fails is logged: the organization exists either way, and the billing page offers to retry the setup.
type Lifecycle struct {
	enabled bool
	db      *gorm.DB
	jobs    *jobs.Enqueuer
	log     *slog.Logger
}

func NewLifecycle(cfg config.Config, s *Stripe, db *gorm.DB, enqueuer *jobs.Enqueuer, log *slog.Logger) *Lifecycle {
	return &Lifecycle{enabled: cfg.IsFormaCloud && s.Configured(), db: db, jobs: enqueuer, log: log}
}

// OrganizationCreated starts the billing setup of a new organization.
func (l *Lifecycle) OrganizationCreated(ctx context.Context, organizationID uuid.UUID) {
	if !l.enabled {
		return
	}
	l.enqueue(ctx, SetupArgs{OrganizationID: organizationID}, "organizationId", organizationID.String())
}

// OrganizationDeleted cancels the subscriptions of a deleted organization's Stripe customer; nil when it had none.
func (l *Lifecycle) OrganizationDeleted(ctx context.Context, stripeCustomerID *string) {
	if !l.enabled || stripeCustomerID == nil || *stripeCustomerID == "" {
		return
	}
	l.enqueue(ctx, CleanupArgs{StripeCustomerID: *stripeCustomerID})
}

func (l *Lifecycle) enqueue(ctx context.Context, args river.JobArgs, attrs ...any) {
	ctx = context.WithoutCancel(ctx)
	err := l.jobs.Transaction(ctx, l.db, func(tx *jobs.Tx) error {
		return tx.Enqueue(ctx, args, nil)
	})
	if err != nil {
		l.log.ErrorContext(ctx, "Failed to enqueue billing job", append([]any{"err", err, "kind", args.Kind()}, attrs...)...)
	}
}
