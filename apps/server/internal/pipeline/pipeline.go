// Package pipeline is forma2's response pipeline (apps/web/modules/response-pipeline): the River worker of the response-pipeline.process jobs the response module enqueues with every write, and what it does after a response, in forma2's order. Every event is delivered to the webhooks subscribed to it; a responseCreated event is then metered for Stripe billing, and a responseFinished event starts the workspace's workflow runs, delivers to its integrations, sends the survey's follow-ups and the members' notification emails, and completes a survey that reached its response limit.
//
// A webhook that fails is retried by retrying the whole job, which delivers again to every subscribed webhook with the same webhook-id, so a receiver can drop the duplicate; no other side effect runs on an attempt that will be retried. The last attempt logs the failure and carries on with the side effects. The integrations, the workflow runs, the Stripe meter and the usage report are reached through Handoffs, which app/jobs.go newPipelineHandoffs wires.
//
// An attempt whose context ends (River cancels it SHUTDOWN_TIMEOUT after the worker is asked to stop, and at jobTimeout) fails as soon as the pipeline sees it, so the side effects it cut short run on the retry instead of being lost. The side effects that had completed run again on that retry, as they did when forma2's BullMQ worker stopped mid-job.
package pipeline

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/server/internal/followup"
	"github.com/yldm-tech/forma/server/internal/notification"
	"github.com/yldm-tech/forma/server/internal/platform/analytics"
	"github.com/yldm-tech/forma/server/internal/platform/audit"
	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/platform/jobs"
	"github.com/yldm-tech/forma/server/internal/response"
	"github.com/yldm-tech/forma/server/internal/responsemail"
	"github.com/yldm-tech/forma/server/internal/webhook"
)

// jobTimeout bounds one attempt. River's default minute is too short for a survey whose members' notification emails go out five at a time, each with SMTP timeouts of their own.
const jobTimeout = 15 * time.Minute

// Survey is what the pipeline reads of the survey a response belongs to.
type Survey struct {
	*responsemail.Survey
	Type         string
	Status       string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	AutoComplete *int
	HasFollowUps bool
}

// organization is what the pipeline reads of the survey's organization.
type organization struct {
	ID               uuid.UUID
	DisplayTimeZone  *string
	LogoURL          *string
	StripeCustomerID *string
}

// Worker works response-pipeline.process jobs.
type Worker struct {
	river.WorkerDefaults[response.PipelineArgs]
	cfg       config.Config
	db        *gorm.DB
	webhooks  *webhook.Deliverer
	followUps *followup.Sender
	notifier  *notification.Notifier
	audit     *audit.Emitter
	handoffs  Handoffs
	log       *slog.Logger
}

func NewWorker(cfg config.Config, db *gorm.DB, webhooks *webhook.Deliverer, followUps *followup.Sender, notifier *notification.Notifier, emitter *audit.Emitter, handoffs Handoffs, log *slog.Logger) *Worker {
	return &Worker{cfg: cfg, db: db, webhooks: webhooks, followUps: followUps, notifier: notifier, audit: emitter, handoffs: handoffs, log: log}
}

// RegisterJobs adds the pipeline's worker to `forma worker`.
func RegisterJobs(reg *jobs.Registry, w *Worker) {
	jobs.Add(reg, w)
}

func (w *Worker) Timeout(*river.Job[response.PipelineArgs]) time.Duration { return jobTimeout }

// JobKey is forma2's deterministic job id for an event: response-pipeline-<event>-<response id>-<updatedAt in ms>. Webhook message ids derive from it, so a retry is recognizable as the same message.
func JobKey(args response.PipelineArgs) string {
	return "response-pipeline-" + args.Event + "-" + args.Response.ID.String() + "-" + strconv.FormatInt(args.Response.UpdatedAt.UnixMilli(), 10)
}

func (w *Worker) Work(ctx context.Context, job *river.Job[response.PipelineArgs]) error {
	args := job.Args
	log := w.log.With("attempt", job.Attempt, "max_attempts", job.MaxAttempts, "job_id", job.ID, "event", args.Event,
		"workspace_id", args.WorkspaceID, "survey_id", args.SurveyID, "response_id", args.Response.ID)
	err := w.work(ctx, job, log)
	switch {
	case err == nil:
	case isCancel(err):
		log.ErrorContext(ctx, "Response pipeline job failed permanently", "err", err)
	default:
		log.ErrorContext(ctx, "Response pipeline job failed", "err", err)
	}
	return err
}

// isCancel reports whether err asks River not to retry.
func isCancel(err error) bool {
	var cancel *river.JobCancelError
	return errors.As(err, &cancel)
}

func (w *Worker) work(ctx context.Context, job *river.Job[response.PipelineArgs], log *slog.Logger) error {
	args := job.Args
	// A partial respondent causes one responseUpdated per page, and webhooks are all that event feeds: without a subscriber there is nothing to load the survey and organization for.
	var hooks []*webhook.Webhook
	if args.Event == response.EventResponseUpdated {
		var err error
		if hooks, err = w.webhooks.Matching(ctx, args.WorkspaceID, args.Event, args.SurveyID); err != nil {
			return err
		}
		if len(hooks) == 0 {
			log.DebugContext(ctx, "Response pipeline job skipped: no webhooks subscribed to responseUpdated")
			return nil
		}
	}
	sv, err := w.loadSurvey(ctx, args.SurveyID)
	if err != nil {
		return err
	}
	if sv == nil {
		return river.JobCancel(fmt.Errorf("survey %s not found", args.SurveyID))
	}
	org, err := w.loadOrganization(ctx, args.WorkspaceID)
	if err != nil {
		return err
	}
	if org == nil {
		return river.JobCancel(fmt.Errorf("organization not found for workspace %s", args.WorkspaceID))
	}
	if sv.WorkspaceID != args.WorkspaceID {
		return river.JobCancel(fmt.Errorf("survey %s does not belong to workspace %s", args.SurveyID, args.WorkspaceID))
	}
	if hooks == nil {
		if hooks, err = w.webhooks.Matching(ctx, args.WorkspaceID, args.Event, args.SurveyID); err != nil {
			return err
		}
	}
	tz := "UTC"
	if org.DisplayTimeZone != nil && *org.DisplayTimeZone != "" {
		tz = *org.DisplayTimeZone
	}
	ev := &Event{
		Name: args.Event, JobKey: JobKey(args), WorkspaceID: args.WorkspaceID, Response: args.Response, Locale: args.Locale,
		OrganizationID: org.ID, DisplayTimeZone: tz, StripeCustomerID: org.StripeCustomerID, Survey: sv,
	}
	if err := w.deliverWebhooks(ctx, job, ev, hooks, log); err != nil {
		return err
	}
	// forma2's runResponseCreatedSideEffects: the response is billable from its creation, counts toward the survey's analytics milestones, and may make the usage update due.
	if args.Event == response.EventResponseCreated {
		if w.handoffs.Meter != nil {
			w.handoffs.Meter.ResponseCreated(ctx, ev.StripeCustomerID, ev.Response.ID, ev.Response.CreatedAt)
		}
		w.captureResponseMilestone(ctx, ev, log)
		if w.handoffs.UsageReport != nil {
			w.handoffs.UsageReport.Send(ctx)
		}
	}
	if args.Event == response.EventResponseFinished {
		return w.responseFinished(ctx, ev, org, log)
	}
	return nil
}

// deliverWebhooks is forma2's deliverWebhooks: every subscribed webhook concurrently. A failure fails the attempt while attempts remain, so the job is retried; on the last attempt it is logged and the job goes on.
func (w *Worker) deliverWebhooks(ctx context.Context, job *river.Job[response.PipelineArgs], ev *Event, hooks []*webhook.Webhook, log *slog.Logger) error {
	if len(hooks) == 0 {
		return nil
	}
	data, err := webhookPayloadData(w.cfg, ev.Response, ev.Survey)
	if err != nil {
		return err
	}
	errs := make([]error, len(hooks))
	var wg sync.WaitGroup
	for i, h := range hooks {
		wg.Go(func() {
			if errs[i] = w.webhooks.Deliver(ctx, h, ev.Name, ev.JobKey, data); errs[i] != nil {
				log.ErrorContext(ctx, "Response pipeline webhook delivery failed", "err", errs[i], "webhook_id", h.ID)
			}
		})
	}
	wg.Wait()
	failed := 0
	var first error
	for _, e := range errs {
		if e != nil {
			failed++
			if first == nil {
				first = e
			}
		}
	}
	if failed == 0 {
		return nil
	}
	if job.Attempt < job.MaxAttempts {
		return first
	}
	log.ErrorContext(ctx, "Response pipeline webhook delivery exhausted retries; continuing with remaining side effects", "failed_webhook_count", failed)
	return nil
}

// autoCompleteTimeout bounds the survey auto-complete, which runs even when the attempt's context has ended: it is one idempotent update, and retrying the job for it would send every notification email again.
const autoCompleteTimeout = 10 * time.Second

// responseFinished is forma2's runResponseFinishedSideEffects. None of its steps fails the job, except a workflow hand-off that asks for a retry, which runs first because it is the only step safe to replay, and the end of the attempt's context before the notification emails are out.
func (w *Worker) responseFinished(ctx context.Context, ev *Event, org *organization, log *slog.Logger) error {
	recipients, err := w.notifier.Recipients(ctx, ev.WorkspaceID, ev.Survey.ID)
	if err != nil {
		if err := interrupted(ctx, "the notification recipient lookup"); err != nil {
			return err
		}
		log.ErrorContext(ctx, "Response pipeline notification recipient lookup failed", "err", err)
		recipients = nil
	}

	if w.handoffs.Workflows != nil && ev.Response.Finished {
		if err := w.handoffs.Workflows.EnqueueResponseCompleted(ctx, ev); err != nil {
			if errors.Is(err, ErrRetry) {
				return err
			}
			if err := interrupted(ctx, "the workflow hand-off"); err != nil {
				return err
			}
			log.ErrorContext(ctx, "Response pipeline workflow run enqueue failed", "err", err)
		}
	}

	if w.handoffs.Integrations != nil {
		if err := w.handoffs.Integrations.HandleResponse(ctx, ev); err != nil {
			log.ErrorContext(ctx, "Response pipeline integration handling failed", "err", err)
		}
		if err := interrupted(ctx, "the integrations"); err != nil {
			return err
		}
	}

	if ev.Survey.HasFollowUps {
		if _, err := w.followUps.Send(ctx, ev.Response.ID, ev.Locale); err != nil {
			log.ErrorContext(ctx, "Response pipeline follow-up delivery failed", "err", err, "rate_limited", followup.IsRateLimited(err))
		}
		if err := interrupted(ctx, "the follow-ups"); err != nil {
			return err
		}
	}

	if len(recipients) > 0 {
		w.notify(ctx, ev, org, recipients, log)
		if err := interrupted(ctx, "the notification emails"); err != nil {
			return err
		}
	}

	actx, cancel := context.WithTimeout(context.WithoutCancel(ctx), autoCompleteTimeout)
	defer cancel()
	w.autoComplete(actx, ev, log)
	return nil
}

// interrupted is the error that fails the attempt when its context ended by the end of step: the step may have been cut short, the ones after it cannot run, and only a retry runs them.
func interrupted(ctx context.Context, step string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("response pipeline interrupted during %s: %w", step, err)
	}
	return nil
}

// notify sends the response-finished email to every recipient, with the count of every response the survey has. Without the count no email is sent.
func (w *Worker) notify(ctx context.Context, ev *Event, org *organization, recipients []notification.Recipient, log *slog.Logger) {
	var count int64
	if err := w.db.WithContext(ctx).Raw(`SELECT count(*) FROM responses WHERE survey_id = ?`, ev.Survey.ID).Row().Scan(&count); err != nil {
		log.ErrorContext(ctx, "Response pipeline notification emails skipped because the response count could not be loaded", "err", err, "notification_recipient_count", len(recipients))
		return
	}
	answers, err := responsemail.Decode(&ev.Response)
	if err != nil {
		log.ErrorContext(ctx, "Response pipeline notification emails skipped", "err", err)
		return
	}
	o := notification.Organization{ID: org.ID}
	if org.LogoURL != nil {
		o.LogoURL = *org.LogoURL
	}
	if err := w.notifier.Send(ctx, recipients, notification.ResponseFinished{
		WorkspaceID: ev.WorkspaceID, Survey: ev.Survey.Survey, Answers: answers, Organization: o, ResponseCount: int(count),
	}); err != nil {
		log.ErrorContext(ctx, "Response pipeline notification emails failed", "err", err)
	}
}

// autoComplete is forma2's handleSurveyAutoCompleteSafely: a survey with a response limit is completed once its finished responses reach it, unless its status changed since this job read it, and the change is audited as the system's.
func (w *Worker) autoComplete(ctx context.Context, ev *Event, log *slog.Logger) {
	sv := ev.Survey
	if sv.AutoComplete == nil || *sv.AutoComplete == 0 || sv.Status == "completed" {
		return
	}
	threshold := *sv.AutoComplete
	var finished int64
	if err := w.db.WithContext(ctx).Raw(`SELECT count(*) FROM responses WHERE survey_id = ? AND finished`, sv.ID).Row().Scan(&finished); err != nil {
		log.ErrorContext(ctx, "Response pipeline survey auto-complete skipped because the finished response count could not be loaded", "err", err, "auto_complete_threshold", threshold)
		return
	}
	if finished < int64(threshold) {
		return
	}
	e := audit.Event{Action: audit.ActionUpdated, TargetType: audit.TargetSurvey, TargetID: sv.ID.String(), ActorType: audit.ActorSystem, ActorID: audit.Unknown,
		OrganizationID: ev.OrganizationID.String(), Status: audit.StatusSuccess, NewObject: map[string]any{"status": "completed"}}
	res := w.db.WithContext(ctx).Exec(`UPDATE surveys SET status = 'completed', updated_at = now() WHERE id = ? AND workspace_id = ? AND status = ?`, sv.ID, sv.WorkspaceID, sv.Status)
	switch {
	case res.Error != nil:
		log.ErrorContext(ctx, "Response pipeline survey auto-complete update failed", "err", res.Error)
		e.Status, e.NewObject = audit.StatusFailure, nil
	case res.RowsAffected == 0:
		log.InfoContext(ctx, "Survey status changed since the pipeline snapshot; skipping auto-complete", "snapshot_status", sv.Status)
		return
	}
	w.audit.Log(ctx, e)
}

// captureResponseMilestone is forma2's captureSurveyResponsePostHogEvent: survey_response_received at the survey's first response, every tenth up to a hundred, then every hundredth.
func (w *Worker) captureResponseMilestone(ctx context.Context, ev *Event, log *slog.Logger) {
	if !analytics.Enabled() {
		return
	}
	var count int64
	if err := w.db.WithContext(ctx).Raw(`SELECT count(*) FROM responses WHERE survey_id = ?`, ev.Survey.ID).Scan(&count).Error; err != nil {
		log.ErrorContext(ctx, "Response pipeline PostHog capture failed", "err", err)
		return
	}
	milestone := ""
	switch {
	case count == 1:
		milestone = "first"
	case count <= 100 && count%10 == 0, count > 100 && count%100 == 0:
		milestone = strconv.FormatInt(count, 10)
	default:
		return
	}
	org, workspace := ev.OrganizationID.String(), ev.WorkspaceID.String()
	analytics.Capture(org, "survey_response_received", analytics.Properties{
		"survey_id":         ev.Survey.ID.String(),
		"survey_type":       ev.Survey.Type,
		"organization_id":   org,
		"workspace_id":      workspace,
		"response_count":    count,
		"is_first_response": count == 1,
		"milestone":         milestone,
	}, analytics.Groups{OrganizationID: org, WorkspaceID: workspace})
}

func (w *Worker) loadSurvey(ctx context.Context, id uuid.UUID) (*Survey, error) {
	base, err := responsemail.LoadSurvey(ctx, w.db, id)
	if err != nil || base == nil {
		return nil, err
	}
	var rows []struct {
		Type         string
		Status       string
		CreatedAt    time.Time
		UpdatedAt    time.Time
		AutoComplete *int
		HasFollowUps bool
	}
	if err := w.db.WithContext(ctx).Raw(`SELECT s.type, s.status, s.created_at, s.updated_at, s.auto_complete,
		EXISTS (SELECT 1 FROM survey_follow_ups f WHERE f.survey_id = s.id) AS has_follow_ups
		FROM surveys s WHERE s.id = ?`, id).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load pipeline survey: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	r := rows[0]
	return &Survey{Survey: base, Type: r.Type, Status: r.Status, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, AutoComplete: r.AutoComplete, HasFollowUps: r.HasFollowUps}, nil
}

func (w *Worker) loadOrganization(ctx context.Context, workspaceID uuid.UUID) (*organization, error) {
	var rows []organization
	if err := w.db.WithContext(ctx).Raw(`SELECT o.id, o.display_time_zone, o.whitelabel ->> 'logoUrl' AS logo_url, b.stripe_customer_id
		FROM organizations o JOIN workspaces w ON w.organization_id = o.id LEFT JOIN organization_billing b ON b.organization_id = o.id
		WHERE w.id = ?`, workspaceID).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load pipeline organization: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}
