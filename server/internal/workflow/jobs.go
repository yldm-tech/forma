package workflow

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/yldm-tech/forma/server/internal/platform/jobs"
)

// RunArgs is forma2's workflow-run.process job (TWorkflowRunJobData): one queued run to execute. Its uniqueness by run id is BullMQ's deterministic jobId, so dispatching a run twice queues it once.
type RunArgs struct {
	WorkflowRunID uuid.UUID `json:"workflowRunId" river:"unique"`
	WorkflowID    uuid.UUID `json:"workflowId"`
	WorkspaceID   uuid.UUID `json:"workspaceId"`
}

func (RunArgs) Kind() string { return "workflow-run.process" }

// ReconcileArgs is forma2's workflow-run.reconcile job, the recurring sweep of runs that were never executed or were abandoned mid-execution.
type ReconcileArgs struct {
	Scope string `json:"scope"`
}

func (ReconcileArgs) Kind() string { return "workflow-run.reconcile" }

// reconcileInterval is WORKFLOW_RUN_RECONCILE_INTERVAL_MS.
const reconcileInterval = 3 * time.Minute

// runTimeout bounds one attempt of a run: every step sends one email, each bounded by the SMTP timeouts.
const runTimeout = 5 * time.Minute

var dispatchOpts = &river.InsertOpts{UniqueOpts: river.UniqueOpts{ByArgs: true}}

// singleton keeps one pending or running sweep, so a slow one is not stacked behind more of itself.
var singleton = &river.InsertOpts{
	MaxAttempts: 1,
	UniqueOpts: river.UniqueOpts{ByState: []rivertype.JobState{
		rivertype.JobStateAvailable, rivertype.JobStatePending, rivertype.JobStateRunning, rivertype.JobStateRetryable, rivertype.JobStateScheduled,
	}},
}

// dispatch hands a run to the queue inside tx, forma2's dispatchWorkflowRunViaJobs.
func dispatch(ctx context.Context, tx *jobs.Tx, run, workflowID, workspaceID uuid.UUID) error {
	return tx.Enqueue(ctx, RunArgs{WorkflowRunID: run, WorkflowID: workflowID, WorkspaceID: workspaceID}, dispatchOpts)
}

type runWorker struct {
	river.WorkerDefaults[RunArgs]
	runner *Runner
}

func (w *runWorker) Timeout(*river.Job[RunArgs]) time.Duration { return runTimeout }

func (w *runWorker) Work(ctx context.Context, job *river.Job[RunArgs]) error {
	return w.runner.Work(ctx, job)
}

type reconcileWorker struct {
	river.WorkerDefaults[ReconcileArgs]
	reconciler *Reconciler
}

func (w *reconcileWorker) Work(ctx context.Context, _ *river.Job[ReconcileArgs]) error {
	return w.reconciler.Run(ctx)
}

// RegisterJobs adds the run executor and the reconcile sweep, every three minutes, to `forma worker`.
func RegisterJobs(reg *jobs.Registry, runner *Runner, reconciler *Reconciler) {
	jobs.Add(reg, &runWorker{runner: runner})
	jobs.Add(reg, &reconcileWorker{reconciler: reconciler})
	reg.AddPeriodic(river.NewPeriodicJob(river.PeriodicInterval(reconcileInterval), func() (river.JobArgs, *river.InsertOpts) {
		return ReconcileArgs{Scope: "global"}, singleton
	}, &river.PeriodicJobOpts{ID: "workflow-run-reconcile"}))
}

// transient reports a database failure worth retrying the whole response pipeline job for, forma2's isDatabasePoolExhaustionError: the server out of connections or resources, a broken connection, or a timeout.
func transient(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return strings.HasPrefix(pgErr.Code, "53") || strings.HasPrefix(pgErr.Code, "08") || pgErr.Code == "57P03"
	}
	return pgconn.SafeToRetry(err) || pgconn.Timeout(err) || errors.Is(err, driver.ErrBadConn)
}
