package survey

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/audit"
	"github.com/yldm-tech/forma/apps/server/internal/survey/scheduling"
)

// schedulingBatchSize is forma2's SURVEY_SCHEDULING_RECONCILIATION_BATCH_SIZE.
const schedulingBatchSize = 250

// normalizeDates is forma2's normalizeSurveyScheduling: both dates are snapped to the schedule of the survey's organization; a manual status change (current nil or different) to inProgress drops the publish date, to paused without a publish date drops the close date, and to completed drops both. A close date not after the publish date is refused.
func normalizeDates(s scheduling.Schedule, current *string, status string, publishOn, closeOn *time.Time) (*time.Time, *time.Time, error) {
	closeOn, publishOn = s.Snap(closeOn), s.Snap(publishOn)
	manual := current == nil || *current != status
	if manual && status == StatusInProgress {
		publishOn = nil
	}
	if manual && status == StatusPaused && publishOn == nil {
		closeOn = nil
	}
	if manual && status == StatusCompleted {
		closeOn, publishOn = nil, nil
	}
	if publishOn != nil && closeOn != nil && !closeOn.After(*publishOn) {
		return nil, nil, apperr.InvalidInput(apperr.CodeValidation, "Close date must be after publish date")
	}
	return publishOn, closeOn, nil
}

// scheduleOf is the schedule of a workspace's organization: its own time zone, or the instance's.
func (s *Service) scheduleOf(ctx context.Context, repo *Repository, workspaceID uuid.UUID) (scheduling.Schedule, error) {
	zone, err := repo.schedulingTimeZone(ctx, workspaceID)
	if err != nil {
		return scheduling.Schedule{}, err
	}
	return s.schedule.ForOrganization(zone), nil
}

// schedulingTimeZone is the scheduling time zone of a workspace's organization, nil when it has none (or the workspace does not exist).
func (r *Repository) schedulingTimeZone(ctx context.Context, workspaceID uuid.UUID) (*string, error) {
	var rows []struct{ SurveySchedulingTimeZone *string }
	if err := r.db.WithContext(ctx).Raw(`SELECT o.survey_scheduling_time_zone FROM workspaces w JOIN organizations o ON o.id = w.organization_id WHERE w.id = ?`, workspaceID).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load organization scheduling time zone: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0].SurveySchedulingTimeZone, nil
}

func isDue(t *time.Time, now time.Time) bool { return t != nil && !t.After(now) }

// isSchedulingDue is forma2's isSurveySchedulingDue.
func isSchedulingDue(s *Survey, now time.Time) bool {
	return isDue(s.PublishOn, now) || isDue(s.CloseOn, now)
}

// transition is one of the two scheduled status changes.
type transition struct {
	name                string
	dueColumn, from, to string
}

var (
	publishTransition = transition{name: "publish", dueColumn: "publish_on", from: StatusPaused, to: StatusInProgress}
	closeTransition   = transition{name: "close", dueColumn: "close_on", from: StatusInProgress, to: StatusCompleted}
)

type schedulingCandidate struct {
	ID             uuid.UUID
	WorkspaceID    uuid.UUID
	OrganizationID uuid.UUID
	Status         string
	PublishOn      *time.Time
	CloseOn        *time.Time
}

// dueCandidates loads the surveys a transition is due for: in its from status, not archived, due date passed; ordered by due date.
func (r *Repository) dueCandidates(ctx context.Context, t transition, now time.Time, surveyID *uuid.UUID, take int) ([]schedulingCandidate, error) {
	query := `SELECT s.id, s.workspace_id, w.organization_id, s.status, s.publish_on, s.close_on FROM surveys s JOIN workspaces w ON w.id = s.workspace_id
		WHERE s.` + t.dueColumn + ` IS NOT NULL AND s.` + t.dueColumn + ` <= ? AND s.status = ? AND s.archived_at IS NULL`
	args := []any{now, t.from}
	if surveyID != nil {
		query += ` AND s.id = ?`
		args = append(args, *surveyID)
	}
	query += ` ORDER BY s.` + t.dueColumn + ` ASC, s.id ASC LIMIT ?`
	args = append(args, take)
	var rows []schedulingCandidate
	if err := r.db.WithContext(ctx).Raw(query, args...).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load due %s candidates: %w", t.name, err)
	}
	return rows, nil
}

// applyTransition is the guarded status change: the same predicate as the candidate query, so a concurrent change makes it a no-op.
func (r *Repository) applyTransition(ctx context.Context, t transition, id uuid.UUID, now time.Time) (bool, error) {
	n, err := r.exec(ctx, `UPDATE surveys SET status = ?, `+t.dueColumn+` = NULL, updated_at = now()
		WHERE id = ? AND `+t.dueColumn+` IS NOT NULL AND `+t.dueColumn+` <= ? AND status = ? AND archived_at IS NULL`, t.to, id, now, t.from)
	if err != nil {
		return false, fmt.Errorf("apply survey %s transition: %w", t.name, err)
	}
	return n > 0, nil
}

// Scheduler applies due publish and close dates, forma2's reconcileDueSurveySchedules.
type Scheduler struct {
	repo  *Repository
	audit *audit.Emitter
	log   *slog.Logger
}

func NewScheduler(repo *Repository, emitter *audit.Emitter, log *slog.Logger) *Scheduler {
	return &Scheduler{repo: repo, audit: emitter, log: log}
}

// Reconcile publishes every paused survey whose publish date has passed, then completes every running survey whose close date has passed, so both can happen in one run. With surveyID it looks at that survey only. A failed update is returned so the job retries; transitions already applied stay applied.
func (s *Scheduler) Reconcile(ctx context.Context, now time.Time, surveyID *uuid.UUID) (published, closed int, err error) {
	if published, err = s.reconcile(ctx, publishTransition, now, surveyID); err != nil {
		return published, 0, err
	}
	if closed, err = s.reconcile(ctx, closeTransition, now, surveyID); err != nil {
		return published, closed, err
	}
	s.log.InfoContext(ctx, "Survey scheduling reconciliation completed", "publishedCount", published, "closedCount", closed)
	return published, closed, nil
}

func (s *Scheduler) reconcile(ctx context.Context, t transition, now time.Time, surveyID *uuid.UUID) (int, error) {
	batch := schedulingBatchSize
	if surveyID != nil {
		batch = 1
	}
	count := 0
	for {
		candidates, err := s.repo.dueCandidates(ctx, t, now, surveyID, batch)
		if err != nil {
			return count, err
		}
		for _, c := range candidates {
			applied, err := s.repo.applyTransition(ctx, t, c.ID, now)
			if err != nil {
				s.log.ErrorContext(ctx, "Survey scheduling transition failed", "surveyId", c.ID.String(), "transition", t.name, "err", err)
				return count, err
			}
			if !applied {
				continue
			}
			count++
			s.recordTransition(ctx, t, c)
		}
		if surveyID != nil || len(candidates) < batch {
			return count, nil
		}
	}
}

func (s *Scheduler) recordTransition(ctx context.Context, t transition, c schedulingCandidate) {
	type state struct {
		Status    string     `json:"status"`
		PublishOn *time.Time `json:"publishOn"`
		CloseOn   *time.Time `json:"closeOn"`
	}
	oldState := state{c.Status, c.PublishOn, c.CloseOn}
	newState := state{t.to, c.PublishOn, nil}
	if t == publishTransition {
		newState = state{t.to, nil, c.CloseOn}
	}
	s.audit.Log(ctx, audit.Event{
		Action: audit.ActionUpdated, TargetType: audit.TargetSurvey, TargetID: c.ID.String(), OrganizationID: c.OrganizationID.String(),
		ActorType: audit.ActorSystem, ActorID: "system", Status: audit.StatusSuccess, OldObject: oldState, NewObject: newState,
	})
}

// reconcileIfDue is the synchronous reconcile a save runs when it leaves a date already due; it reports whether the survey changed.
func (s *Scheduler) reconcileIfDue(ctx context.Context, sv *Survey, now time.Time) (bool, error) {
	if !isSchedulingDue(sv, now) {
		return false, nil
	}
	published, closed, err := s.Reconcile(ctx, now, &sv.ID)
	return published+closed > 0, err
}
