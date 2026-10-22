package workflow

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/server/internal/platform/jobs"
)

// The reconciler's tuning, forma2's reconcile-constants.
const (
	// orphanMinAge is the grace window: a younger queued run may still be on its way to the queue.
	orphanMinAge = 2 * time.Minute
	// orphanMaxAge is the ceiling past which a queued run is failed instead of dispatched again.
	orphanMaxAge = 24 * time.Hour
	// stuckRunningMaxAge is how long a running run may go without a write before it counts as abandoned.
	stuckRunningMaxAge = time.Hour
	// reconcileBatch is how many runs one sweep handles; a backlog drains over the next sweeps.
	reconcileBatch = 250
)

// The errors the reconciler records, forma2's.
const (
	agedOutDispatched   = "Workflow run was dispatched but never executed and exceeded the reconcile age ceiling"
	agedOutUndispatched = "Workflow run was never dispatched and exceeded the reconcile age ceiling"
	abandonedRun        = "Workflow run was abandoned mid-execution (stale running) and recovered by the reconciler"
	abandonedStep       = "Step outcome unknown: run abandoned mid-execution; not retried (at-most-once)"
)

// Reconciler is forma2's workflow-run.reconcile: it dispatches again the queued runs whose job never ran, fails those past the age ceiling, and fails the running runs abandoned mid-execution, marking their in-flight steps skipped so they are never sent twice.
type Reconciler struct {
	db   *gorm.DB
	jobs *jobs.Enqueuer
	log  *slog.Logger
}

func NewReconciler(db *gorm.DB, enqueuer *jobs.Enqueuer, log *slog.Logger) *Reconciler {
	return &Reconciler{db: db, jobs: enqueuer, log: log}
}

// OrphanResult is what the queued sweep did.
type OrphanResult struct {
	Scanned         int `json:"scanned"`
	Redispatched    int `json:"redispatched"`
	AgedOutFailed   int `json:"agedOutFailed"`
	NeverDispatched int `json:"neverDispatched"`
}

// StuckResult is what the running sweep did.
type StuckResult struct {
	Scanned      int `json:"scanned"`
	Recovered    int `json:"recovered"`
	StepsSkipped int `json:"stepsSkipped"`
}

// Run is one sweep: both halves concurrently against one clock. A failed scan fails the job; the schedule is the backstop.
func (r *Reconciler) Run(ctx context.Context) error {
	now := time.Now()
	var (
		wg                sync.WaitGroup
		queued            OrphanResult
		stuck             StuckResult
		queuedErr, runErr error
	)
	wg.Go(func() { queued, queuedErr = r.Orphans(ctx, now) })
	wg.Go(func() { stuck, runErr = r.Stuck(ctx, now) })
	wg.Wait()
	if err := errors.Join(queuedErr, runErr); err != nil {
		return err
	}
	if queued.Redispatched > 0 || queued.AgedOutFailed > 0 || stuck.Recovered > 0 {
		r.log.InfoContext(ctx, "Workflow run reconciliation acted", "queued", queued, "stuck_running", stuck)
	} else {
		r.log.DebugContext(ctx, "Workflow run reconciliation found nothing to reconcile", "queued", queued, "stuck_running", stuck)
	}
	return nil
}

type orphan struct {
	ID           uuid.UUID
	WorkflowID   uuid.UUID
	WorkspaceID  uuid.UUID
	CreatedAt    time.Time
	DispatchedAt *time.Time
}

// Orphans is forma2's reconcileOrphanedWorkflowRuns over real runs still queued after the grace window, oldest first. Dispatching again is a no-op while the run's job exists, so every such run is dispatched; one never dispatched before is counted and stamped. Each run is handled on its own, so one failure does not stop the sweep.
func (r *Reconciler) Orphans(ctx context.Context, now time.Time) (OrphanResult, error) {
	var rows []orphan
	if err := r.db.WithContext(ctx).Raw(`SELECT id, workflow_id, workspace_id, created_at, dispatched_at FROM workflow_runs
		WHERE status = 'queued' AND NOT is_dry_run AND created_at < ? ORDER BY created_at LIMIT ?`, now.Add(-orphanMinAge), reconcileBatch).Scan(&rows).Error; err != nil {
		return OrphanResult{}, fmt.Errorf("scan queued workflow runs: %w", err)
	}
	res := OrphanResult{Scanned: len(rows)}
	for _, o := range rows {
		log := r.log.With("workflow_run_id", o.ID, "workflow_id", o.WorkflowID, "workspace_id", o.WorkspaceID)
		if o.CreatedAt.Before(now.Add(-orphanMaxAge)) {
			msg := agedOutUndispatched
			if o.DispatchedAt != nil {
				msg = agedOutDispatched
			}
			failed := r.db.WithContext(ctx).Exec(`UPDATE workflow_runs SET status = 'failed', error = ?, last_error_at = ?, finished_at = ?, updated_at = now()
				WHERE id = ? AND workspace_id = ? AND status = 'queued'`, msg, now, now, o.ID, o.WorkspaceID)
			switch {
			case failed.Error != nil:
				log.ErrorContext(ctx, "Failed to reconcile orphaned workflow run", "err", failed.Error)
			case failed.RowsAffected > 0:
				res.AgedOutFailed++
				log.WarnContext(ctx, "Orphaned workflow run exceeded reconcile age ceiling; marked failed", "created_at", o.CreatedAt)
			}
			continue
		}
		err := r.jobs.Transaction(ctx, r.db, func(tx *jobs.Tx) error {
			if err := dispatch(ctx, tx, o.ID, o.WorkflowID, o.WorkspaceID); err != nil {
				return err
			}
			if o.DispatchedAt != nil {
				return nil
			}
			return tx.Exec(`UPDATE workflow_runs SET dispatched_at = ?, updated_at = now() WHERE id = ? AND workspace_id = ?`, now, o.ID, o.WorkspaceID).Error
		})
		if err != nil {
			log.ErrorContext(ctx, "Failed to reconcile orphaned workflow run", "err", err)
			continue
		}
		res.Redispatched++
		if o.DispatchedAt == nil {
			res.NeverDispatched++
		}
	}
	return res, nil
}

type stuckRun struct {
	ID          uuid.UUID
	WorkflowID  uuid.UUID
	WorkspaceID uuid.UUID
	StartedAt   *time.Time
	UpdatedAt   time.Time
}

// Stuck is forma2's reconcileStuckRunningWorkflowRuns over real runs running without a write for an hour: each is failed, if it is still running, and only then are its in-flight steps marked skipped, never failed, which the runner would claim and send again.
func (r *Reconciler) Stuck(ctx context.Context, now time.Time) (StuckResult, error) {
	var rows []stuckRun
	if err := r.db.WithContext(ctx).Raw(`SELECT id, workflow_id, workspace_id, started_at, updated_at FROM workflow_runs
		WHERE status = 'running' AND NOT is_dry_run AND updated_at < ? ORDER BY updated_at LIMIT ?`, now.Add(-stuckRunningMaxAge), reconcileBatch).Scan(&rows).Error; err != nil {
		return StuckResult{}, fmt.Errorf("scan running workflow runs: %w", err)
	}
	res := StuckResult{Scanned: len(rows)}
	for _, s := range rows {
		log := r.log.With("workflow_run_id", s.ID, "workflow_id", s.WorkflowID, "workspace_id", s.WorkspaceID)
		failed := r.db.WithContext(ctx).Exec(`UPDATE workflow_runs SET status = 'failed', error = ?, last_error_at = ?, finished_at = ?, updated_at = now()
			WHERE id = ? AND workspace_id = ? AND status = 'running'`, abandonedRun, now, now, s.ID, s.WorkspaceID)
		if failed.Error != nil {
			log.ErrorContext(ctx, "Failed to recover stuck running workflow run", "err", failed.Error)
			continue
		}
		if failed.RowsAffected == 0 {
			continue
		}
		res.Recovered++
		skipped := r.db.WithContext(ctx).Exec(`UPDATE workflow_run_logs SET status = 'skipped', error = ?, finished_at = ? WHERE run_id = ? AND status = 'running'`, abandonedStep, now, s.ID)
		if skipped.Error != nil {
			log.ErrorContext(ctx, "Failed to recover stuck running workflow run", "err", skipped.Error)
			continue
		}
		res.StepsSkipped += int(skipped.RowsAffected)
		log.WarnContext(ctx, "Stuck running workflow run recovered; marked failed", "started_at", s.StartedAt, "updated_at", s.UpdatedAt, "steps_skipped", skipped.RowsAffected)
	}
	return res, nil
}
