package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/server/internal/pipeline"
	"github.com/yldm-tech/forma/server/internal/platform/jobs"
	"github.com/yldm-tech/forma/server/internal/platform/jsvalue"
	"github.com/yldm-tech/forma/server/internal/platform/legacyid"
	"github.com/yldm-tech/forma/server/internal/survey/surveydoc"
)

// Enqueuer is the producer half of the runner, forma2's enqueueResponseCompletedWorkflowRuns: the pipeline's workflow hand-off.
type Enqueuer struct {
	db   *gorm.DB
	jobs *jobs.Enqueuer
	log  *slog.Logger
}

func NewEnqueuer(db *gorm.DB, enqueuer *jobs.Enqueuer, log *slog.Logger) *Enqueuer {
	return &Enqueuer{db: db, jobs: enqueuer, log: log}
}

var _ pipeline.WorkflowEnqueuer = (*Enqueuer)(nil)

// candidate is an enabled workflow with its published version.
type candidate struct {
	WorkflowID uuid.UUID
	VersionID  *uuid.UUID
	Definition json.RawMessage
}

// triggerConfig is a published trigger's configuration, read without trusting the stored document.
type triggerConfig struct {
	SurveyID      string
	EndingCardIDs []string
}

// readTriggerConfig is forma2's readResponseCompletedTriggerConfig: total over malformed documents, nil for anything but a response.completed trigger with a survey id and a list of ending ids.
func readTriggerConfig(definition json.RawMessage) *triggerConfig {
	var d struct {
		Trigger *struct {
			TriggerType any `json:"triggerType"`
			Config      *struct {
				SurveyID      any `json:"surveyId"`
				EndingCardIDs any `json:"endingCardIds"`
			} `json:"config"`
		} `json:"trigger"`
	}
	if json.Unmarshal(definition, &d) != nil || d.Trigger == nil || d.Trigger.TriggerType != TriggerResponseCompleted || d.Trigger.Config == nil {
		return nil
	}
	surveyID, ok := d.Trigger.Config.SurveyID.(string)
	if !ok {
		return nil
	}
	list, ok := d.Trigger.Config.EndingCardIDs.([]any)
	if !ok {
		return nil
	}
	cfg := &triggerConfig{SurveyID: surveyID, EndingCardIDs: make([]string, len(list))}
	for i, item := range list {
		s, ok := item.(string)
		if !ok {
			return nil
		}
		cfg.EndingCardIDs[i] = s
	}
	return cfg
}

// matches is forma2's matchWorkflowsForResponse for one candidate: the trigger targets the response's survey (a survey id stored before the migration matches by its forma2 id), and either names no endings or names the one the response reached.
func (c *triggerConfig) matches(surveyID uuid.UUID, endingID *string) bool {
	if !legacyid.Matches(c.SurveyID, surveyID) {
		return false
	}
	if len(c.EndingCardIDs) == 0 {
		return true
	}
	return endingID != nil && slices.Contains(c.EndingCardIDs, *endingID)
}

// TriggerPayload is the response.completed payload a run stores, forma2's TWorkflowTriggerRunPayload.
type TriggerPayload struct {
	Type         string          `json:"type"`
	WorkspaceID  string          `json:"workspaceId"`
	SurveyID     string          `json:"surveyId"`
	ResponseID   string          `json:"responseId"`
	EndingCardID *string         `json:"endingCardId,omitempty"`
	Data         json.RawMessage `json:"data,omitempty"`
	TriggeredAt  string          `json:"triggeredAt"`
}

// EnqueueResponseCompleted persists one queued run per enabled workflow whose published trigger the finished response meets, keyed by (workflow, response id) so a replayed event creates nothing new, and dispatches each in the transaction that writes it. A database failure worth retrying asks the pipeline to retry; any other failure is isolated to its workflow.
func (e *Enqueuer) EnqueueResponseCompleted(ctx context.Context, ev *pipeline.Event) error {
	r := ev.Response
	if !r.Finished {
		return nil
	}
	log := e.log.With("job_key", ev.JobKey, "workspace_id", ev.WorkspaceID, "survey_id", r.SurveyID, "response_id", r.ID)
	candidates, err := e.candidates(ctx, ev.WorkspaceID, r.SurveyID, log)
	if err != nil {
		return retryable(err)
	}
	var matched []candidate
	for _, c := range candidates {
		if readTriggerConfig(c.Definition).matches(r.SurveyID, r.EndingID) {
			matched = append(matched, c)
		}
	}
	if len(matched) == 0 {
		return nil
	}
	payload := mustJSON(e.payload(ev, log))
	for _, c := range matched {
		if err := e.createAndDispatch(ctx, ev, c, payload); err != nil {
			if transient(err) {
				return retryable(err)
			}
			log.ErrorContext(ctx, "Failed to persist workflow run", "err", err, "workflow_id", c.WorkflowID)
		}
	}
	return nil
}

func retryable(err error) error {
	if transient(err) {
		return fmt.Errorf("%w: %w", pipeline.ErrRetry, err)
	}
	return err
}

// candidates are the workspace's enabled workflows that may target the survey, each with its newest version. A null trigger_survey_id matches every survey, so the narrowing never hides a workflow the matcher would fire. A workflow without a version, or whose version has no usable trigger, is logged and skipped.
func (e *Enqueuer) candidates(ctx context.Context, workspaceID, surveyID uuid.UUID, log *slog.Logger) ([]candidate, error) {
	var rows []candidate
	if err := e.db.WithContext(ctx).Raw(`SELECT DISTINCT ON (w.id) w.id AS workflow_id, v.id AS version_id, v.definition
		FROM workflows w LEFT JOIN workflow_versions v ON v.workflow_id = w.id
		WHERE w.workspace_id = ? AND w.status = 'enabled' AND (w.trigger_survey_id = ? OR w.trigger_survey_id IS NULL)
		ORDER BY w.id, v.version DESC NULLS LAST`, workspaceID, surveyID).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load enabled workflows: %w", err)
	}
	out := rows[:0]
	for _, c := range rows {
		switch {
		case c.VersionID == nil:
			log.WarnContext(ctx, "Enabled workflow has no published version; skipping workflow run enqueue", "workflow_id", c.WorkflowID)
		case readTriggerConfig(c.Definition) == nil:
			log.WarnContext(ctx, "Published workflow version has no usable response.completed trigger; skipping workflow run enqueue", "workflow_id", c.WorkflowID, "workflow_version_id", *c.VersionID)
		default:
			out = append(out, c)
		}
	}
	return out, nil
}

// payload is forma2's buildTriggerPayload, the same for every matched workflow and, since triggeredAt is the response's updatedAt, for every replay. An ending id that is not one a survey could have is left out, but the runs still start.
func (e *Enqueuer) payload(ev *pipeline.Event, log *slog.Logger) TriggerPayload {
	r := ev.Response
	p := TriggerPayload{
		Type: TriggerResponseCompleted, WorkspaceID: ev.WorkspaceID.String(), SurveyID: r.SurveyID.String(), ResponseID: r.ID.String(),
		Data: jsonObject(r.Data), TriggeredAt: jsvalue.ISOString(r.UpdatedAt),
	}
	if r.EndingID != nil && *r.EndingID != "" {
		if surveydoc.IsCUID2(*r.EndingID) {
			p.EndingCardID = r.EndingID
		} else {
			ending := *r.EndingID
			if len(ending) > 64 {
				ending = ending[:64]
			}
			log.Warn("Response ending id is not a valid ending card id; enqueueing workflow runs without it", "ending_id", ending)
		}
	}
	return p
}

// createAndDispatch writes the queued run bound to the published version, or finds the run an earlier delivery of the event wrote, and queues its job in the same transaction, so no run is left without one.
func (e *Enqueuer) createAndDispatch(ctx context.Context, ev *pipeline.Event, c candidate, payload json.RawMessage) error {
	r := ev.Response
	return e.jobs.Transaction(ctx, e.db, func(tx *jobs.Tx) error {
		var id uuid.UUID
		if err := tx.Raw(`INSERT INTO workflow_runs (workflow_id, workspace_id, workflow_version_id, status, trigger_type, survey_id, response_id, is_dry_run, attempt, idempotency_key, trigger_payload, dispatched_at)
			VALUES (?, ?, ?, 'queued', 'response.completed', ?, ?, false, 0, ?, ?::jsonb, now())
			ON CONFLICT (workflow_id, idempotency_key) DO UPDATE SET dispatched_at = now(), updated_at = now()
			RETURNING id`, c.WorkflowID, ev.WorkspaceID, c.VersionID, r.SurveyID, r.ID, r.ID.String(), string(payload)).Row().Scan(&id); err != nil {
			return fmt.Errorf("persist workflow run: %w", err)
		}
		return dispatch(ctx, tx, id, c.WorkflowID, ev.WorkspaceID)
	})
}
