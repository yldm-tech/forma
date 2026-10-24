package survey

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/platform/audit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/jobs"
	"github.com/yldm-tech/forma/apps/server/internal/survey/scheduling"
)

// Archive retention, forma2's ARCHIVED_SURVEY_RETENTION_DAYS and purge batching.
const (
	archiveRetention     = 30 * 24 * time.Hour
	purgeBatchSize       = 100
	purgeMaxBatches      = 10000
	purgeHour, purgeMins = 1, 30
)

// dailyAt is a River schedule that fires once a day at a local time of the instance's scheduling zone, forma2's "<minute> <hour> * * *" cron in SURVEY_SCHEDULING_TIME_ZONE.
type dailyAt struct {
	schedule     scheduling.Schedule
	hour, minute int
}

func (d dailyAt) Next(current time.Time) time.Time {
	return d.schedule.NextAt(current, d.hour, d.minute)
}

// SchedulingArgs is forma2's survey-scheduling.reconcile job.
type SchedulingArgs struct {
	// Slot is the slot the run belongs to (jobs.Daily), which makes a caught-up slot run once.
	Slot time.Time `json:"slot"`
}

func (SchedulingArgs) Kind() string { return "survey-scheduling.reconcile" }

type schedulingWorker struct {
	river.WorkerDefaults[SchedulingArgs]
	scheduler *Scheduler
}

func (w *schedulingWorker) Work(ctx context.Context, _ *river.Job[SchedulingArgs]) error {
	_, _, err := w.scheduler.Reconcile(ctx, time.Now(), nil)
	return err
}

// ArchivePurgeArgs is forma2's survey-archive-purge.process job.
type ArchivePurgeArgs struct {
	// Slot is the daily slot the run belongs to (jobs.Daily), which makes a caught-up slot run once.
	Slot time.Time `json:"slot"`
}

func (ArchivePurgeArgs) Kind() string { return "survey-archive-purge.process" }

// Purger deletes surveys archived longer than the retention, forma2's processSurveyArchivePurgeJob.
type Purger struct {
	repo  *Repository
	audit *audit.Emitter
	log   *slog.Logger
}

func NewPurger(repo *Repository, emitter *audit.Emitter, log *slog.Logger) *Purger {
	return &Purger{repo: repo, audit: emitter, log: log}
}

type purgeCandidate struct {
	ID             uuid.UUID
	OrganizationID uuid.UUID
}

// PurgeResult counts what one purge run did.
type PurgeResult struct {
	Deleted, Skipped, Failed int
}

// Purge deletes, oldest archive first and in batches, every survey archived before now minus the retention. Each delete re-checks the archive stamp under a row lock, so a survey restored in the meantime is skipped; a survey that fails is counted and not retried in the same run.
func (p *Purger) Purge(ctx context.Context, now time.Time) (PurgeResult, error) {
	cutoff := now.Add(-archiveRetention)
	var result PurgeResult
	failed := []uuid.UUID{}
	for batch := 0; batch < purgeMaxBatches; batch++ {
		var candidates []purgeCandidate
		err := p.repo.db.WithContext(ctx).Raw(`SELECT s.id, w.organization_id FROM surveys s JOIN workspaces w ON w.id = s.workspace_id
			WHERE s.archived_at IS NOT NULL AND s.archived_at < ? AND NOT (s.id = ANY(?::uuid[])) ORDER BY s.archived_at ASC, s.id ASC LIMIT ?`,
			cutoff, uuidArray(failed), purgeBatchSize).Scan(&candidates).Error
		if err != nil {
			return result, fmt.Errorf("load archived surveys: %w", err)
		}
		for _, c := range candidates {
			// A run stopped from outside (the worker shutting down) ends here rather than failing every remaining survey.
			if err := ctx.Err(); err != nil {
				return result, err
			}
			deleted, err := p.repo.deleteSurvey(ctx, c.ID, &cutoff)
			switch {
			case err != nil:
				result.Failed++
				failed = append(failed, c.ID)
				p.log.ErrorContext(ctx, "Archived survey purge failed", "surveyId", c.ID.String(), "err", err)
			case deleted == nil:
				result.Skipped++
			default:
				result.Deleted++
				p.audit.Log(ctx, audit.Event{
					Action: audit.ActionDeleted, TargetType: audit.TargetSurvey, TargetID: c.ID.String(), OrganizationID: c.OrganizationID.String(),
					ActorType: audit.ActorSystem, ActorID: "system", Status: audit.StatusSuccess, OldObject: deleted,
				})
			}
		}
		if len(candidates) < purgeBatchSize {
			break
		}
	}
	p.log.InfoContext(ctx, "Archived survey purge completed", "deleted", result.Deleted, "skipped", result.Skipped, "failed", result.Failed)
	return result, nil
}

type purgeWorker struct {
	river.WorkerDefaults[ArchivePurgeArgs]
	purger *Purger
}

// Timeout lifts River's per-job limit (a minute unless the client sets one): one survey with many responses can take longer than that to delete, and a run cut short starts again at that same survey, so the purge would never get past it. A run is bounded by purgeMaxBatches instead.
func (w *purgeWorker) Timeout(*river.Job[ArchivePurgeArgs]) time.Duration { return -1 }

func (w *purgeWorker) Work(ctx context.Context, _ *river.Job[ArchivePurgeArgs]) error {
	_, err := w.purger.Purge(ctx, time.Now())
	return err
}

// RegisterJobs adds the survey module's background work to `forma worker`: the publish and close run every quarter hour at the configured local minute, the instants at which the configured local time arrives in any organization's zone (scheduling.Runs), and the daily archive purge at 01:30 in the instance's scheduling time zone; a slot missed while no worker ran is caught up at the next start.
func RegisterJobs(reg *jobs.Registry, cfg config.Config, db *gorm.DB, emitter *audit.Emitter, log *slog.Logger) {
	repo := NewRepository(db)
	schedule := scheduling.Instance(cfg)
	jobs.Add(reg, &schedulingWorker{scheduler: NewScheduler(repo, emitter, log)})
	jobs.Add(reg, &purgeWorker{purger: NewPurger(repo, emitter, log)})
	reg.AddPeriodic(jobs.Daily("survey-scheduling", scheduling.RunsFor(schedule), func(slot time.Time) river.JobArgs {
		return SchedulingArgs{Slot: slot}
	}))
	reg.AddPeriodic(jobs.Daily("survey-archive-purge", dailyAt{schedule, purgeHour, purgeMins}, func(slot time.Time) river.JobArgs {
		return ArchivePurgeArgs{Slot: slot}
	}))
}
