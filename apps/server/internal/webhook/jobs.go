package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"

	"github.com/yldm-tech/forma/apps/server/internal/platform/jobs"
)

// DeliverArgs is the webhook.deliver job: one response pipeline event delivered to one webhook. Each webhook has a job of its own, so a failing endpoint is retried on its own schedule without delivering again to the others or holding back the pipeline's other side effects. The job is unique by webhook and pipeline job key: a pipeline job that runs again (an interrupted attempt, a retry for a later step) queues no second delivery while the first is queued, running, or completed and not yet cleaned out.
type DeliverArgs struct {
	WebhookID uuid.UUID `json:"webhookId" river:"unique"`
	// JobKey is the pipeline job's key, which names the event and the response version; the Standard Webhooks message id derives from it, so every attempt carries the same webhook-id.
	JobKey      string    `json:"jobKey" river:"unique"`
	Event       string    `json:"event"`
	WorkspaceID uuid.UUID `json:"workspaceId"`
	SurveyID    uuid.UUID `json:"surveyId"`
	// ResponseID is the response the event is about. It is not part of the uniqueness, which JobKey already carries; every attempt checks the response still exists, so a response deleted during the retries, or its survey, stops the delivery of its data.
	ResponseID uuid.UUID `json:"responseId"`
	// Data is the event's data as the pipeline encoded it, kept as JSON text: River stores job args as jsonb, which would reorder the keys of an embedded object, and the delivered body is frozen byte for byte.
	Data string `json:"data"`
}

func (DeliverArgs) Kind() string { return "webhook.deliver" }

// retrySchedule is the wait before each retry of a failed delivery, the Standard Webhooks reference schedule (Svix's): a quick retry for a blip, then hours, so an endpoint that is down for a day still gets the event. A delivery that fails all of them is discarded about 28 hours after its first attempt.
var retrySchedule = []time.Duration{5 * time.Second, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 5 * time.Hour, 10 * time.Hour, 10 * time.Hour}

// deliveryAttempts is the first attempt and every retry of retrySchedule.
var deliveryAttempts = len(retrySchedule) + 1

func (DeliverArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: deliveryAttempts, UniqueOpts: river.UniqueOpts{ByArgs: true}}
}

// Enqueue queues the delivery of event to each of hooks inside tx. jobKey is the pipeline job's key, responseID the response the event is about and data the event's encoded data.
func Enqueue(ctx context.Context, tx *jobs.Tx, hooks []*Webhook, event, jobKey string, workspaceID, surveyID, responseID uuid.UUID, data json.RawMessage) error {
	for _, h := range hooks {
		args := DeliverArgs{WebhookID: h.ID, JobKey: jobKey, Event: event, WorkspaceID: workspaceID, SurveyID: surveyID, ResponseID: responseID, Data: string(data)}
		if err := tx.Enqueue(ctx, args, nil); err != nil {
			return fmt.Errorf("queue delivery to webhook %s: %w", h.ID, err)
		}
	}
	return nil
}

type deliverWorker struct {
	river.WorkerDefaults[DeliverArgs]
	deliverer *Deliverer
	// breaker caps the process's deliveries to each webhook and holds back those to a webhook whose endpoint keeps failing.
	breaker *breaker
	log     *slog.Logger
}

func newDeliverWorker(deliverer *Deliverer, log *slog.Logger) *deliverWorker {
	return &deliverWorker{deliverer: deliverer, breaker: newBreaker(), log: log}
}

// RegisterJobs adds the webhook delivery worker to `forma worker`.
func RegisterJobs(reg *jobs.Registry, deliverer *Deliverer, log *slog.Logger) {
	jobs.Add(reg, newDeliverWorker(deliverer, log))
}

// NextRetry follows retrySchedule instead of the queue's default backoff of seconds.
func (w *deliverWorker) NextRetry(job *river.Job[DeliverArgs]) time.Time {
	return time.Now().UTC().Add(retryDelay(job.Attempt))
}

// retryDelay is the wait after the attempt-th failed attempt.
func retryDelay(attempt int) time.Duration {
	return retrySchedule[min(max(attempt, 1), len(retrySchedule))-1]
}

// Work delivers the event to the webhook as it is now: its current URL and secret, and only while it still exists and subscribes to the event for the survey, so a webhook deleted or narrowed during the retries stops receiving it. A response deleted during the retries, or its survey, stops it too, so its data is not sent on after an erasure, and so does a resend from the webhooks page that delivered it in the meantime. Every attempt is recorded in the webhook's delivery log; a failure to record it is logged and does not fail the job, which would deliver the event again.
//
// The breaker admits the delivery first. One it holds back, because the webhook already has its share of the process's deliveries running or its endpoint keeps failing, is snoozed before anything is read: the snooze frees the slot at once, is not counted as an attempt, and leaves the delivery's retries for when the endpoint recovers. A delivery queued longer ago than giveUpAfter is sent even through an open circuit, as its last attempt, and is discarded when it fails.
func (w *deliverWorker) Work(ctx context.Context, job *river.Job[DeliverArgs]) error {
	a := job.Args
	log := w.log.With("job_id", job.ID, "attempt", job.Attempt, "max_attempts", job.MaxAttempts, "webhook_id", a.WebhookID, "event", a.Event,
		"workspace_id", a.WorkspaceID, "survey_id", a.SurveyID)
	pastDeadline := w.deliverer.now().Sub(job.CreatedAt) >= giveUpAfter
	adm := w.breaker.enter(a.WebhookID, pastDeadline)
	if adm.Wait > 0 {
		log.DebugContext(ctx, "Webhook delivery held back", "circuit_open", adm.Open, "snooze", adm.Wait.String())
		return river.JobSnooze(adm.Wait)
	}
	result := outcomeSkipped
	defer func() {
		switch w.breaker.done(a.WebhookID, adm, result) {
		case transitionOpened:
			log.WarnContext(ctx, "Webhook circuit opened: its endpoint keeps timing out or failing, so its deliveries wait", "threshold", breakerThreshold, "open_for", breakerOpenFor.String())
		case transitionReopened:
			log.InfoContext(ctx, "Webhook circuit stays open: the probe failed", "open_for", breakerOpenFor.String())
		case transitionClosed:
			log.InfoContext(ctx, "Webhook circuit closed: its endpoint answers again")
		case transitionNone:
		}
	}()
	exists, err := w.deliverer.repo.responseExists(ctx, a.WorkspaceID, a.SurveyID, a.ResponseID)
	if err != nil {
		return err
	}
	if !exists {
		log.InfoContext(ctx, "Webhook delivery skipped: the response or its survey was deleted", "response_id", a.ResponseID)
		return nil
	}
	hooks, err := w.deliverer.Matching(ctx, a.WorkspaceID, a.Event, a.SurveyID)
	if err != nil {
		return err
	}
	messageID := MessageID(a.JobKey, a.WebhookID, a.Event)
	i := slices.IndexFunc(hooks, func(h *Webhook) bool { return h.ID == a.WebhookID })
	if i < 0 {
		log.InfoContext(ctx, "Webhook delivery skipped: the webhook was deleted or no longer subscribes to the event")
		if err := w.deliverer.repo.abandon(ctx, a.WebhookID, messageID); err != nil {
			log.ErrorContext(ctx, "Failed to record the end of a webhook delivery's retries", "err", err)
		}
		return nil
	}
	delivered, err := w.deliverer.repo.deliveredAlready(ctx, a.WebhookID, messageID)
	if err != nil {
		return err
	}
	if delivered {
		log.InfoContext(ctx, "Webhook delivery skipped: a resend delivered it")
		return nil
	}
	hook := hooks[i]
	attempt, err := w.deliverer.attempt(ctx, hook, a.Event, messageID, json.RawMessage(a.Data))
	if err != nil {
		return err
	}
	result = outcomeOf(attempt)
	rec := deliveryRecord{
		WebhookID: a.WebhookID, ResponseID: a.ResponseID, SurveyID: a.SurveyID, Event: a.Event, MessageID: messageID, Payload: a.Data,
		Status: DeliverySucceeded, At: w.deliverer.now().UTC(), Attempt: attempt,
	}
	final := job.Attempt >= job.MaxAttempts || pastDeadline
	switch {
	case attempt.OK():
	case final:
		rec.Status = DeliveryFailed
	default:
		next := rec.At.Add(retryDelay(job.Attempt))
		rec.Status, rec.NextAttemptAt = DeliveryRetrying, &next
	}
	if err := w.deliverer.repo.recordAttempt(ctx, rec); err != nil {
		log.ErrorContext(ctx, "Failed to record a webhook delivery attempt", "err", err)
	}
	err = attempt.err(hook)
	switch {
	case err == nil:
		return nil
	case final && job.Attempt < job.MaxAttempts:
		// Past its deadline with attempts left: River would retry it, so it is cancelled instead.
		log.ErrorContext(ctx, "Webhook delivery failed; it waited past its retry schedule", "err", err)
		return river.JobCancel(err)
	case final:
		log.ErrorContext(ctx, "Webhook delivery failed; retries exhausted", "err", err)
	default:
		log.WarnContext(ctx, "Webhook delivery failed; it will be retried", "err", err, "retry_in", retryDelay(job.Attempt).String())
	}
	return err
}
