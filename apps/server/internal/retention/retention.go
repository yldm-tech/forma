// Package retention is forma2's retention sweep (apps/web/lib/jobs/retention): once a day, each organization's own retention windows are applied to its terminal workflow runs and to the survey impressions (displays) that never became a response. A window is configuration on the organization, null (keep forever) everywhere unless someone sets it, so an upgrade deletes nothing by itself.
package retention

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/platform/jobs"
)

// forma2's sweep limits.
const (
	// deleteBatchSize is how many rows one statement retires; a workflow run cascades to its logs, so a batch touches far more than its own width.
	deleteBatchSize = 1000
	// maxDeleteBatches bounds the statements per unit (a workspace and status for runs, a survey for displays); the remainder waits for the next day.
	maxDeleteBatches = 50
	// surveyPageSize is how many survey ids are read per round trip while walking an organization's displays.
	surveyPageSize = 200
	// DryRun is forma2's RETENTION_SWEEP_DRY_RUN: this release only reports what each window would remove. Deleting product data is irreversible, so an operator gets to see the volume in the logs before any row goes; turning it off is a separate, deliberate change.
	DryRun = true
)

// terminalRunStatuses are the statuses a run never leaves. A queued or running run is live work and is never swept, however old.
var terminalRunStatuses = []string{"completed", "failed", "canceled"}

// PassResult counts one pass over every organization.
type PassResult struct {
	// Matched is what a dry run found eligible; Deleted what a real run removed.
	Matched int64 `json:"matched"`
	Deleted int64 `json:"deleted"`
	// CappedUnits are units that reached maxDeleteBatches and left the rest for the next run.
	CappedUnits int64 `json:"cappedUnits"`
}

func (p *PassResult) add(o PassResult) {
	p.Matched += o.Matched
	p.Deleted += o.Deleted
	p.CappedUnits += o.CappedUnits
}

// Result is one sweep.
type Result struct {
	DryRun bool `json:"dryRun"`
	// Organizations had at least one window set.
	Organizations int        `json:"organizations"`
	WorkflowRuns  PassResult `json:"workflowRuns"`
	Displays      PassResult `json:"displays"`
}

// Sweeper applies the windows.
type Sweeper struct {
	db  *gorm.DB
	log *slog.Logger
}

func NewSweeper(db *gorm.DB, log *slog.Logger) *Sweeper {
	return &Sweeper{db: db, log: log}
}

type idRow struct {
	ID uuid.UUID
}

type organizationWindows struct {
	ID                       uuid.UUID
	WorkflowRunRetentionDays *int
	DisplayRetentionDays     *int
}

// Cutoff is the instant rows must have been created strictly before to be past a window of days.
func Cutoff(now time.Time, days int) time.Time {
	return now.Add(-time.Duration(days) * 24 * time.Hour)
}

// Sweep applies every organization's windows as of now. One organization's failure is logged and the others are still swept. The columns' CHECK keeps a window a positive number of days, so every non-null window is usable.
func (s *Sweeper) Sweep(ctx context.Context, now time.Time, dryRun bool) (Result, error) {
	var orgs []organizationWindows
	err := s.db.WithContext(ctx).Raw(`SELECT id, workflow_run_retention_days, display_retention_days FROM organizations
		WHERE workflow_run_retention_days IS NOT NULL OR display_retention_days IS NOT NULL ORDER BY id`).Scan(&orgs).Error
	if err != nil {
		return Result{}, fmt.Errorf("list organizations with retention windows: %w", err)
	}
	result := Result{DryRun: dryRun}
	for _, org := range orgs {
		result.Organizations++
		if err := s.sweepOrganization(ctx, org, now, dryRun, &result); err != nil {
			s.log.ErrorContext(ctx, "Retention sweep failed for an organization", "organizationId", org.ID.String(), "err", err)
		}
	}
	return result, nil
}

func (s *Sweeper) sweepOrganization(ctx context.Context, org organizationWindows, now time.Time, dryRun bool, result *Result) error {
	if org.WorkflowRunRetentionDays != nil {
		runs, err := s.sweepWorkflowRuns(ctx, org.ID, Cutoff(now, *org.WorkflowRunRetentionDays), dryRun)
		if err != nil {
			return err
		}
		result.WorkflowRuns.add(runs)
	}
	if org.DisplayRetentionDays != nil {
		displays, err := s.sweepDisplays(ctx, org.ID, Cutoff(now, *org.DisplayRetentionDays), dryRun)
		if err != nil {
			return err
		}
		result.Displays.add(displays)
	}
	return nil
}

// sweepWorkflowRuns goes one workspace and one status at a time, so each statement is an equality on the status, and by created_at, because a run canceled before it was dispatched never finished.
func (s *Sweeper) sweepWorkflowRuns(ctx context.Context, organizationID uuid.UUID, cutoff time.Time, dryRun bool) (PassResult, error) {
	var workspaces []idRow
	if err := s.db.WithContext(ctx).Raw(`SELECT id FROM workspaces WHERE organization_id = ? ORDER BY id`, organizationID).Scan(&workspaces).Error; err != nil {
		return PassResult{}, fmt.Errorf("list workspaces: %w", err)
	}
	var result PassResult
	for _, w := range workspaces {
		workspaceID := w.ID
		for _, status := range terminalRunStatuses {
			if dryRun {
				var n int64
				err := s.db.WithContext(ctx).Raw(`SELECT count(*) FROM workflow_runs WHERE workspace_id = ? AND status = ? AND created_at < ?`, workspaceID, status, cutoff).Scan(&n).Error
				if err != nil {
					return PassResult{}, fmt.Errorf("count expired workflow runs: %w", err)
				}
				result.Matched += n
				continue
			}
			deleted, capped, err := drain(func() (int64, error) {
				res := s.db.WithContext(ctx).Exec(`
					WITH expired AS (
						SELECT id FROM workflow_runs WHERE workspace_id = ? AND status = ? AND created_at < ? ORDER BY created_at LIMIT ?
					)
					DELETE FROM workflow_runs AS run USING expired WHERE run.id = expired.id`, workspaceID, status, cutoff, deleteBatchSize)
				return res.RowsAffected, res.Error
			})
			if err != nil {
				return PassResult{}, fmt.Errorf("delete expired workflow runs: %w", err)
			}
			result.Deleted += deleted
			if capped {
				result.CappedUnits++
			}
		}
	}
	return result, nil
}

// sweepDisplays walks the organization's surveys in id order, a page at a time, so each statement stays on the (survey_id, created_at) index. An impression that became a response is kept: the response would otherwise silently lose its display (ON DELETE SET NULL).
func (s *Sweeper) sweepDisplays(ctx context.Context, organizationID uuid.UUID, cutoff time.Time, dryRun bool) (PassResult, error) {
	var result PassResult
	after := uuid.Nil
	for {
		var surveys []idRow
		err := s.db.WithContext(ctx).Raw(`SELECT s.id FROM surveys s JOIN workspaces w ON w.id = s.workspace_id
			WHERE w.organization_id = ? AND s.id > ? ORDER BY s.id LIMIT ?`, organizationID, after, surveyPageSize).Scan(&surveys).Error
		if err != nil {
			return PassResult{}, fmt.Errorf("list surveys: %w", err)
		}
		for _, row := range surveys {
			surveyID := row.ID
			if dryRun {
				var n int64
				err := s.db.WithContext(ctx).Raw(`SELECT count(*) FROM displays d WHERE d.survey_id = ? AND d.created_at < ?
					AND NOT EXISTS (SELECT 1 FROM responses r WHERE r.display_id = d.id)`, surveyID, cutoff).Scan(&n).Error
				if err != nil {
					return PassResult{}, fmt.Errorf("count expired displays: %w", err)
				}
				result.Matched += n
				continue
			}
			deleted, capped, err := drain(func() (int64, error) {
				res := s.db.WithContext(ctx).Exec(`
					WITH expired AS (
						SELECT d.id FROM displays d WHERE d.survey_id = ? AND d.created_at < ?
							AND NOT EXISTS (SELECT 1 FROM responses r WHERE r.display_id = d.id)
						ORDER BY d.created_at LIMIT ?
					)
					DELETE FROM displays AS target USING expired WHERE target.id = expired.id`, surveyID, cutoff, deleteBatchSize)
				return res.RowsAffected, res.Error
			})
			if err != nil {
				return PassResult{}, fmt.Errorf("delete expired displays: %w", err)
			}
			result.Deleted += deleted
			if capped {
				result.CappedUnits++
			}
		}
		if len(surveys) < surveyPageSize {
			return result, nil
		}
		after = surveys[len(surveys)-1].ID
	}
}

// drain runs deleteBatch until a batch comes back short or maxDeleteBatches have run; capped reports the latter.
func drain(deleteBatch func() (int64, error)) (deleted int64, capped bool, err error) {
	for range maxDeleteBatches {
		n, err := deleteBatch()
		if err != nil {
			return deleted, false, err
		}
		deleted += n
		if n < deleteBatchSize {
			return deleted, false, nil
		}
	}
	return deleted, true, nil
}

// SweepArgs is forma2's retention-sweep.process job.
type SweepArgs struct {
	// Slot is the daily slot the run belongs to (jobs.Daily), which makes a caught-up slot run once.
	Slot time.Time `json:"slot"`
}

func (SweepArgs) Kind() string { return "retention-sweep.process" }

type sweepWorker struct {
	river.WorkerDefaults[SweepArgs]
	sweeper *Sweeper
	log     *slog.Logger
}

// Timeout lifts River's one-minute limit: a first sweep over years of history is bounded by maxDeleteBatches per unit, not by the clock.
func (w *sweepWorker) Timeout(*river.Job[SweepArgs]) time.Duration { return -1 }

func (w *sweepWorker) Work(ctx context.Context, job *river.Job[SweepArgs]) error {
	logContext := []any{"attempt", job.Attempt, "jobId", job.ID, "jobName", job.Kind, "maxAttempts", job.MaxAttempts, "queueName", job.Queue}
	w.log.InfoContext(ctx, "Retention sweep job started", logContext...)
	result, err := w.sweeper.Sweep(ctx, time.Now(), DryRun)
	if err != nil {
		return err
	}
	message := "Retention sweep job completed"
	if result.DryRun {
		message = "Retention sweep job completed (dry run, nothing deleted)"
	}
	// invalidWindows is always 0: the columns' CHECK refuses a window that is not a positive number of days. It stays in the line so existing log queries keep working.
	w.log.InfoContext(ctx, message, append(logContext, "dryRun", result.DryRun, "organizations", result.Organizations, "invalidWindows", 0, "workflowRuns", result.WorkflowRuns, "displays", result.Displays)...)
	return nil
}

// RegisterJobs adds the daily sweep. forma2 ran it every 24 hours aligned to the epoch, which is midnight UTC.
func RegisterJobs(reg *jobs.Registry, db *gorm.DB, log *slog.Logger) {
	jobs.Add(reg, &sweepWorker{sweeper: NewSweeper(db, log), log: log})
	reg.AddPeriodic(jobs.Daily("retention-sweep", jobs.DailyUTC{}, func(slot time.Time) river.JobArgs { return SweepArgs{Slot: slot} }))
}
