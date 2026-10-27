// Package retention is forma2's retention sweep (apps/web/lib/jobs/retention): once a day, each organization's own retention windows are applied to its terminal workflow runs and to the survey impressions (displays) that never became a response, except an app survey's impressions of identified users, which are its targeting history. A window is configuration on the organization, null (keep forever) until its owner sets one (the Data retention card), so an upgrade deletes nothing by itself. The instance's RETENTION_SWEEP_DRY_RUN, on by default, makes every sweep count instead of delete; each organization's last sweep is kept for its owner to read (organization_retention_sweeps).
package retention

import (
	"context"
	"errors"
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
	// recordTimeout bounds recording a sweep that failed, which outlives the job's context.
	recordTimeout = 10 * time.Second
)

// Schedule is when the sweep runs: every day at midnight UTC, as forma2's every-24-hours repeat aligned to the epoch did.
var Schedule = jobs.DailyUTC{}

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

type surveyRow struct {
	ID   uuid.UUID
	Type string
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

// Sweep applies every organization's windows as of now. One organization's failure is logged and the others are still swept; a canceled ctx ends the sweep with its error. The columns' CHECK keeps a window a positive number of days, so every non-null window is usable.
func (s *Sweeper) Sweep(ctx context.Context, now time.Time, dryRun bool) (Result, error) {
	var orgs []organizationWindows
	err := s.db.WithContext(ctx).Raw(`SELECT id, workflow_run_retention_days, display_retention_days FROM organizations
		WHERE workflow_run_retention_days IS NOT NULL OR display_retention_days IS NOT NULL ORDER BY id`).Scan(&orgs).Error
	if err != nil {
		return Result{}, fmt.Errorf("list organizations with retention windows: %w", err)
	}
	result := Result{DryRun: dryRun}
	for _, org := range orgs {
		// A job that is shutting down stops between organizations, so the ones it never reached keep their last report rather than an empty stopped-early one.
		if err := ctx.Err(); err != nil {
			return result, err
		}
		result.Organizations++
		if err := s.sweepOrganization(ctx, org, now, dryRun, &result); err != nil {
			s.log.ErrorContext(ctx, "Retention sweep failed for an organization", "organizationId", org.ID.String(), "err", err)
		}
	}
	return result, nil
}

// sweepOrganization applies one organization's windows, adds what it counted or deleted to result, and records it as the organization's last sweep. A pass that fails does not stop the other one, and what the passes did before the failure is still recorded, marked as stopped early: on an instance that deletes, those rows are gone for good, and the owner's card has to say so.
func (s *Sweeper) sweepOrganization(ctx context.Context, org organizationWindows, now time.Time, dryRun bool, result *Result) error {
	var runs, displays PassResult
	var runsErr, displaysErr error
	if org.WorkflowRunRetentionDays != nil {
		runs, runsErr = s.sweepWorkflowRuns(ctx, org.ID, Cutoff(now, *org.WorkflowRunRetentionDays), dryRun)
		result.WorkflowRuns.add(runs)
	}
	if org.DisplayRetentionDays != nil {
		displays, displaysErr = s.sweepDisplays(ctx, org.ID, Cutoff(now, *org.DisplayRetentionDays), dryRun)
		result.Displays.add(displays)
	}
	failed := errors.Join(runsErr, displaysErr)
	report := Report{SweptAt: now, DryRun: dryRun, StoppedEarly: failed != nil, WorkflowRunRetentionDays: org.WorkflowRunRetentionDays, DisplayRetentionDays: org.DisplayRetentionDays, WorkflowRuns: runs, Displays: displays}
	recordCtx := ctx
	if failed != nil {
		// The failure may be the job's context ending; what was already deleted is recorded all the same.
		var cancel context.CancelFunc
		recordCtx, cancel = context.WithTimeout(context.WithoutCancel(ctx), recordTimeout)
		defer cancel()
	}
	return errors.Join(failed, s.record(recordCtx, report, org.ID))
}

// record replaces the organization's last sweep. An organization deleted while it was being swept has nothing to record, and its row is not inserted.
func (s *Sweeper) record(ctx context.Context, r Report, organizationID uuid.UUID) error {
	err := s.db.WithContext(ctx).Exec(`
		INSERT INTO organization_retention_sweeps (organization_id, swept_at, dry_run, stopped_early, workflow_run_retention_days, display_retention_days,
			workflow_runs_matched, workflow_runs_deleted, workflow_runs_capped_units, displays_matched, displays_deleted, displays_capped_units)
		SELECT id, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ? FROM organizations WHERE id = ?
		ON CONFLICT (organization_id) DO UPDATE SET swept_at = excluded.swept_at, dry_run = excluded.dry_run, stopped_early = excluded.stopped_early,
			workflow_run_retention_days = excluded.workflow_run_retention_days, display_retention_days = excluded.display_retention_days,
			workflow_runs_matched = excluded.workflow_runs_matched, workflow_runs_deleted = excluded.workflow_runs_deleted, workflow_runs_capped_units = excluded.workflow_runs_capped_units,
			displays_matched = excluded.displays_matched, displays_deleted = excluded.displays_deleted, displays_capped_units = excluded.displays_capped_units`,
		r.SweptAt, r.DryRun, r.StoppedEarly, r.WorkflowRunRetentionDays, r.DisplayRetentionDays,
		r.WorkflowRuns.Matched, r.WorkflowRuns.Deleted, r.WorkflowRuns.CappedUnits, r.Displays.Matched, r.Displays.Deleted, r.Displays.CappedUnits, organizationID).Error
	if err != nil {
		return fmt.Errorf("record the retention sweep: %w", err)
	}
	return nil
}

// Report is an organization's last retention sweep.
type Report struct {
	SweptAt time.Time `json:"sweptAt"`
	// DryRun is whether the sweep only counted (the PassResults' Matched) or deleted (their Deleted).
	DryRun bool `json:"dryRun"`
	// StoppedEarly is a sweep where a pass failed: the counts are what it got through before the failure, and the rest waits for the next sweep.
	StoppedEarly bool `json:"stoppedEarly"`
	// The windows the sweep applied; nil is a pass it did not run.
	WorkflowRunRetentionDays *int       `json:"workflowRunRetentionDays"`
	DisplayRetentionDays     *int       `json:"displayRetentionDays"`
	WorkflowRuns             PassResult `json:"workflowRuns"`
	Displays                 PassResult `json:"displays"`
}

// LastReport reads the organization's last sweep, nil when no sweep has visited it. db is the caller's connection, a transaction included.
func LastReport(ctx context.Context, db *gorm.DB, organizationID uuid.UUID) (*Report, error) {
	var rows []struct {
		SweptAt                  time.Time
		DryRun                   bool
		StoppedEarly             bool
		WorkflowRunRetentionDays *int
		DisplayRetentionDays     *int
		WorkflowRunsMatched      int64
		WorkflowRunsDeleted      int64
		WorkflowRunsCappedUnits  int64
		DisplaysMatched          int64
		DisplaysDeleted          int64
		DisplaysCappedUnits      int64
	}
	if err := db.WithContext(ctx).Raw(`SELECT swept_at, dry_run, stopped_early, workflow_run_retention_days, display_retention_days, workflow_runs_matched, workflow_runs_deleted,
		workflow_runs_capped_units, displays_matched, displays_deleted, displays_capped_units FROM organization_retention_sweeps WHERE organization_id = ?`, organizationID).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load the last retention sweep: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	r := rows[0]
	return &Report{SweptAt: r.SweptAt, DryRun: r.DryRun, StoppedEarly: r.StoppedEarly, WorkflowRunRetentionDays: r.WorkflowRunRetentionDays, DisplayRetentionDays: r.DisplayRetentionDays,
		WorkflowRuns: PassResult{Matched: r.WorkflowRunsMatched, Deleted: r.WorkflowRunsDeleted, CappedUnits: r.WorkflowRunsCappedUnits},
		Displays:     PassResult{Matched: r.DisplaysMatched, Deleted: r.DisplaysDeleted, CappedUnits: r.DisplaysCappedUnits}}, nil
}

// Preview is what a deleting sweep run now would delete under a set of windows; nil is a window that keeps that data forever, which deletes nothing and is not counted.
type Preview struct {
	WorkflowRuns *int64 `json:"workflowRuns"`
	Displays     *int64 `json:"displays"`
}

// PreviewWindows counts, as of now, what the organization's sweep would delete under the given windows, with the queries a dry run counts with, so an owner sees what windows the organization does not have yet would delete before saving them. db is the caller's connection.
func PreviewWindows(ctx context.Context, db *gorm.DB, organizationID uuid.UUID, now time.Time, workflowRunDays, displayDays *int) (Preview, error) {
	s := &Sweeper{db: db}
	var out Preview
	if workflowRunDays != nil {
		runs, err := s.sweepWorkflowRuns(ctx, organizationID, Cutoff(now, *workflowRunDays), true)
		if err != nil {
			return Preview{}, err
		}
		out.WorkflowRuns = &runs.Matched
	}
	if displayDays != nil {
		displays, err := s.sweepDisplays(ctx, organizationID, Cutoff(now, *displayDays), true)
		if err != nil {
			return Preview{}, err
		}
		out.Displays = &displays.Matched
	}
	return out, nil
}

// sweepWorkflowRuns goes one workspace and one status at a time, so each statement is an equality on the status, and by created_at, because a run canceled before it was dispatched never finished. On an error it returns what it counted or deleted before it, as sweepDisplays does.
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
					return result, fmt.Errorf("count expired workflow runs: %w", err)
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
			// The batches drain committed before a failure stay deleted, so they count.
			result.Deleted += deleted
			if err != nil {
				return result, fmt.Errorf("delete expired workflow runs: %w", err)
			}
			if capped {
				result.CappedUnits++
			}
		}
	}
	return result, nil
}

// sweepDisplays walks the organization's surveys in id order, a page at a time, so each statement stays on the (survey_id, created_at) index. An impression that became a response is kept: the response would otherwise silently lose its display (ON DELETE SET NULL). An app survey's impression of an identified user (one linked to a contact) is kept as well, whatever its age: it is what the SDK decides show once, display limits and waiting periods by (clientapi userState), so deleting it would show the survey to that user again. It goes with its contact or its survey. A link survey's impressions, and an app survey's anonymous ones, are swept.
func (s *Sweeper) sweepDisplays(ctx context.Context, organizationID uuid.UUID, cutoff time.Time, dryRun bool) (PassResult, error) {
	var result PassResult
	after := uuid.Nil
	for {
		var surveys []surveyRow
		err := s.db.WithContext(ctx).Raw(`SELECT s.id, s.type FROM surveys s JOIN workspaces w ON w.id = s.workspace_id
			WHERE w.organization_id = ? AND s.id > ? ORDER BY s.id LIMIT ?`, organizationID, after, surveyPageSize).Scan(&surveys).Error
		if err != nil {
			return result, fmt.Errorf("list surveys: %w", err)
		}
		for _, row := range surveys {
			surveyID := row.ID
			// keepIdentified excludes the survey's impressions linked to a contact; the type is the survey's current one, the one userState reads.
			keepIdentified := row.Type == "app"
			if dryRun {
				var n int64
				err := s.db.WithContext(ctx).Raw(`SELECT count(*) FROM displays d WHERE d.survey_id = ? AND d.created_at < ?
					AND NOT (? AND d.contact_id IS NOT NULL)
					AND NOT EXISTS (SELECT 1 FROM responses r WHERE r.display_id = d.id)`, surveyID, cutoff, keepIdentified).Scan(&n).Error
				if err != nil {
					return result, fmt.Errorf("count expired displays: %w", err)
				}
				result.Matched += n
				continue
			}
			deleted, capped, err := drain(func() (int64, error) {
				res := s.db.WithContext(ctx).Exec(`
					WITH expired AS (
						SELECT d.id FROM displays d WHERE d.survey_id = ? AND d.created_at < ?
							AND NOT (? AND d.contact_id IS NOT NULL)
							AND NOT EXISTS (SELECT 1 FROM responses r WHERE r.display_id = d.id)
						ORDER BY d.created_at LIMIT ?
					)
					DELETE FROM displays AS target USING expired WHERE target.id = expired.id`, surveyID, cutoff, keepIdentified, deleteBatchSize)
				return res.RowsAffected, res.Error
			})
			// The batches drain committed before a failure stay deleted, so they count.
			result.Deleted += deleted
			if err != nil {
				return result, fmt.Errorf("delete expired displays: %w", err)
			}
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
	// dryRun is the instance's RETENTION_SWEEP_DRY_RUN.
	dryRun bool
	log    *slog.Logger
}

// Timeout lifts River's one-minute limit: a first sweep over years of history is bounded by maxDeleteBatches per unit, not by the clock.
func (w *sweepWorker) Timeout(*river.Job[SweepArgs]) time.Duration { return -1 }

func (w *sweepWorker) Work(ctx context.Context, job *river.Job[SweepArgs]) error {
	logContext := []any{"attempt", job.Attempt, "jobId", job.ID, "jobName", job.Kind, "maxAttempts", job.MaxAttempts, "queueName", job.Queue}
	w.log.InfoContext(ctx, "Retention sweep job started", logContext...)
	result, err := w.sweeper.Sweep(ctx, time.Now(), w.dryRun)
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

// RegisterJobs adds the daily sweep on Schedule; dryRun is the instance's RETENTION_SWEEP_DRY_RUN.
func RegisterJobs(reg *jobs.Registry, dryRun bool, db *gorm.DB, log *slog.Logger) {
	jobs.Add(reg, &sweepWorker{sweeper: NewSweeper(db, log), dryRun: dryRun, log: log})
	reg.AddPeriodic(jobs.Daily("retention-sweep", Schedule, func(slot time.Time) river.JobArgs { return SweepArgs{Slot: slot} }))
}
