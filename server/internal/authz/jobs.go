package authz

import (
	"context"
	"log/slog"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/server/internal/platform/jobs"
)

// Schedules, forma2's recurring registrations.
const (
	deliveryInterval = 5 * time.Second
	auditInterval    = 6 * time.Hour
	// auditTimeout bounds one audit: a dry run, possibly an apply and a confirming dry run over the whole graph.
	auditTimeout = time.Hour
)

// DeliverArgs is forma2's authzed-projection.deliver job: drain the projection outbox into SpiceDB.
type DeliverArgs struct{}

func (DeliverArgs) Kind() string { return "authzed-projection.deliver" }

// AuditArgs is forma2's authzed-reconciliation.audit job: the six-hourly full reconciliation.
type AuditArgs struct{}

func (AuditArgs) Kind() string { return "authzed-reconciliation.audit" }

type deliverWorker struct {
	river.WorkerDefaults[DeliverArgs]
	delivery *Delivery
}

// Work drains at most ten batches. A failure is not retried: the next tick, five seconds later, is the retry.
func (w *deliverWorker) Work(ctx context.Context, _ *river.Job[DeliverArgs]) error {
	if w.delivery == nil {
		return nil
	}
	_, err := w.delivery.Drain(ctx, deliveryJobBatches)
	return err
}

type auditWorker struct {
	river.WorkerDefaults[AuditArgs]
	audit *Audit
}

func (w *auditWorker) Timeout(*river.Job[AuditArgs]) time.Duration { return auditTimeout }

func (w *auditWorker) Work(ctx context.Context, _ *river.Job[AuditArgs]) error {
	if w.audit == nil {
		return nil
	}
	return w.audit.Run(ctx)
}

// singleton keeps one pending or running instance of a periodic job, so a slow run is not stacked behind more of itself.
var singleton = &river.InsertOpts{
	MaxAttempts: 1,
	UniqueOpts: river.UniqueOpts{ByState: []rivertype.JobState{
		rivertype.JobStateAvailable, rivertype.JobStatePending, rivertype.JobStateRunning, rivertype.JobStateRetryable, rivertype.JobStateScheduled,
	}},
}

// RegisterJobs adds the delivery and audit workers and their schedules. Both are no-ops while AuthZed is disabled (nil arguments).
func RegisterJobs(reg *jobs.Registry, delivery *Delivery, audit *Audit) {
	jobs.Add(reg, &deliverWorker{delivery: delivery})
	jobs.Add(reg, &auditWorker{audit: audit})
	reg.AddPeriodic(river.NewPeriodicJob(river.PeriodicInterval(deliveryInterval), func() (river.JobArgs, *river.InsertOpts) {
		return DeliverArgs{}, singleton
	}, &river.PeriodicJobOpts{ID: "authzed-projection-delivery", RunOnStart: true}))
	reg.AddPeriodic(river.NewPeriodicJob(river.PeriodicInterval(auditInterval), func() (river.JobArgs, *river.InsertOpts) {
		return AuditArgs{}, singleton
	}, &river.PeriodicJobOpts{ID: "authzed-reconciliation-audit"}))
}

// Audit is the scheduled reconciliation: a dry run over everything, and when it finds drift an apply and a confirming dry run. It repairs what PostgreSQL attributes and never prunes. It then retires delivered outbox history, and after a clean audit hands every dead letter back to delivery: SpiceDB already agrees with PostgreSQL everywhere, so whatever a dead letter was trying to say has been said, and leaving it would keep the freshness guard failing every check until someone replayed it by hand.
type Audit struct {
	db       *gorm.DB
	backfill *Backfill
	log      *slog.Logger
}

// NewAudit returns nil when AuthZed is disabled.
func NewAudit(db *gorm.DB, client *Client, log *slog.Logger) *Audit {
	if client == nil {
		return nil
	}
	return &Audit{db: db, backfill: NewBackfill(db, client, log), log: log}
}

func (a *Audit) pass(ctx context.Context, name string, apply bool) BackfillResult {
	started := time.Now()
	res := a.backfill.Run(ctx, BackfillRequest{Apply: apply, MaxPrune: MaxPrune, Scope: BackfillScope{Kind: ScopeAll}})
	codes := make([]Code, 0, len(res.Failures))
	for _, f := range res.Failures {
		codes = append(codes, f.Code)
	}
	a.log.InfoContext(ctx, "AuthZed reconciliation pass finished", "component", "authz", "operation", "scheduled_reconciliation", "pass", name, "status", res.Status, "duration_ms", time.Since(started).Milliseconds(), "failed", res.Counters.Failed, "failure_codes", codes)
	return res
}

func (a *Audit) Run(ctx context.Context) error {
	observed := a.pass(ctx, "dry_run", false)
	result := observed
	if observed.Status == StatusDrifted {
		applied := a.pass(ctx, "apply", true)
		a.log.InfoContext(ctx, "AuthZed reconciliation repaired drift", "component", "authz", "operation", "scheduled_reconciliation", "repaired", applied.Counters.Reconciled, "failed", applied.Counters.Failed)
		result = a.pass(ctx, "confirm", false)
	}
	if result.Status != StatusReconciled {
		a.log.WarnContext(ctx, "Scheduled AuthZed relationship reconciliation did not finish cleanly", "component", "authz", "operation", "scheduled_reconciliation", "drift", result.Counters.Missing+result.Counters.MismatchedPermissions, "failures", result.Counters.Failed, "status", result.Status)
	}
	if _, err := pruneOutboxHistory(ctx, a.db); err != nil {
		return err
	}
	if result.Status == StatusReconciled {
		replayed, err := replayDeadLetters(ctx, a.db)
		if err != nil {
			return err
		}
		if replayed > 0 {
			a.log.InfoContext(ctx, "AuthZed dead letters replayed after a clean reconciliation", "component", "authz", "operation", "scheduled_reconciliation", "count", replayed)
		}
	}
	return nil
}
