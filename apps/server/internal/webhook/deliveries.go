package webhook

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/audit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/jsvalue"
	"github.com/yldm-tech/forma/apps/server/internal/platform/ratelimit"
)

// A delivery's state after its latest attempt, the status column of webhook_deliveries.
const (
	// DeliverySucceeded is a delivery an attempt delivered. It stays succeeded when a later attempt, a resend, fails.
	DeliverySucceeded = "succeeded"
	// DeliveryRetrying is a failed attempt whose job will try again.
	DeliveryRetrying = "retrying"
	// DeliveryFailed is a delivery whose retries ran out, or whose resend failed after they had.
	DeliveryFailed = "failed"
)

// A webhook's health on the webhooks and integrations pages, read from its recent deliveries.
const (
	// HealthHealthy is a webhook whose latest attempt succeeded and none of whose kept deliveries was lost.
	HealthHealthy = "healthy"
	// HealthRetrying is a webhook whose latest attempt failed while none of its kept deliveries was lost and one is still being retried.
	HealthRetrying = "retrying"
	// HealthFailing is a webhook whose latest attempt failed while a kept delivery was lost (its retries ran out) or nothing is being retried.
	HealthFailing = "failing"
	// HealthLost is a webhook whose latest attempt succeeded, its endpoint working again, while kept deliveries it lost earlier wait to be resent.
	HealthLost = "lost"
)

// deliveriesKept is how many deliveries of each webhook are kept, the newest; the delivery log shows them all.
const deliveriesKept = 100

// CodeThirdPartySecret is the refusal to regenerate the secret of a webhook an automation platform subscribed, which received the secret when it subscribed and has no way to learn a new one.
const CodeThirdPartySecret = "webhook_secret_third_party"

// DeliveryView is one delivery as the delivery log shows it: never its payload. Failure and FailureMessage say why the latest attempt failed, and are null once one succeeded.
type DeliveryView struct {
	ID         uuid.UUID `json:"id"`
	CreatedAt  string    `json:"createdAt"`
	Event      string    `json:"event"`
	ResponseID uuid.UUID `json:"responseId"`
	SurveyID   uuid.UUID `json:"surveyId"`
	SurveyName *string   `json:"surveyName"`
	// MessageID is the webhook-id header every attempt carries, which receivers deduplicate on.
	MessageID      string  `json:"messageId"`
	Status         string  `json:"status"`
	Attempts       int     `json:"attempts"`
	LastAttemptAt  string  `json:"lastAttemptAt"`
	NextAttemptAt  *string `json:"nextAttemptAt"`
	StatusCode     *int    `json:"statusCode"`
	Failure        *string `json:"failure"`
	FailureMessage *string `json:"failureMessage"`
	LatencyMs      int     `json:"latencyMs"`
}

// Health is a webhook's delivery health, from its kept deliveries; a webhook nothing was sent to yet has none. Status is HealthHealthy, HealthRetrying, HealthFailing or HealthLost. The latest attempt's outcome is StatusCode, Failure and FailureMessage, and NextAttemptAt is when its delivery is tried again. LastSuccessAt is the latest attempt that delivered an event. FailingSince is, while the latest attempt failed, the first failed attempt after the last success. FailedCount is the kept deliveries that were lost and not resent since, however long ago, so a later success does not hide them.
type Health struct {
	Status         string  `json:"status"`
	LastAttemptAt  string  `json:"lastAttemptAt"`
	LastSuccessAt  *string `json:"lastSuccessAt"`
	FailingSince   *string `json:"failingSince"`
	NextAttemptAt  *string `json:"nextAttemptAt"`
	StatusCode     *int    `json:"statusCode"`
	Failure        *string `json:"failure"`
	FailureMessage *string `json:"failureMessage"`
	FailedCount    int     `json:"failedCount"`
}

// SecretView is the answer to a regenerated secret, the only time it is shown.
type SecretView struct {
	Secret string `json:"secret"`
}

// deliveryRecord is one attempt of a delivery as the worker records it.
type deliveryRecord struct {
	WebhookID  uuid.UUID
	ResponseID uuid.UUID
	SurveyID   uuid.UUID
	Event      string
	MessageID  string
	Payload    string
	Status     string
	At         time.Time
	// NextAttemptAt is set exactly when Status is DeliveryRetrying.
	NextAttemptAt *time.Time
	Attempt       Attempt
}

// attemptColumns are an attempt's outcome as the columns store it: no status code without an answer, no failure for a success, and a latency in whole milliseconds.
func attemptColumns(a Attempt) (statusCode *int, failure, message *string, latencyMs int64) {
	if a.StatusCode != 0 {
		statusCode = &a.StatusCode
	}
	if !a.OK() {
		failure, message = &a.Failure, &a.Message
	}
	return statusCode, failure, message, a.Latency.Milliseconds()
}

// recordAttempt stores an attempt of the worker's: the delivery's first, which adds its row and trims the webhook's log to deliveriesKept, or a retry, which updates it. A retry that a resend overtook, one sent after the job found the delivery still undelivered while the resend delivered it, leaves the delivered row alone: it is neither retried nor failed.
func (r *Repository) recordAttempt(ctx context.Context, rec deliveryRecord) error {
	code, failure, message, latency := attemptColumns(rec.Attempt)
	var firstFailedAt, deliveredAt *time.Time
	if rec.Attempt.OK() {
		deliveredAt = &rec.At
	} else {
		firstFailedAt = &rec.At
	}
	var inserted bool
	err := r.db.WithContext(ctx).Raw(`INSERT INTO webhook_deliveries AS d (webhook_id, response_id, survey_id, event, message_id, payload, status, attempts,
			last_attempted_at, first_failed_at, next_attempt_at, delivered_at, status_code, failure, failure_message, latency_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (webhook_id, message_id) DO UPDATE SET status = EXCLUDED.status, attempts = d.attempts + 1, last_attempted_at = EXCLUDED.last_attempted_at,
			first_failed_at = COALESCE(d.first_failed_at, EXCLUDED.first_failed_at), next_attempt_at = EXCLUDED.next_attempt_at, delivered_at = EXCLUDED.delivered_at,
			status_code = EXCLUDED.status_code, failure = EXCLUDED.failure, failure_message = EXCLUDED.failure_message, latency_ms = EXCLUDED.latency_ms
		WHERE d.status <> 'succeeded'
		RETURNING xmax = 0`,
		rec.WebhookID, rec.ResponseID, rec.SurveyID, rec.Event, rec.MessageID, rec.Payload, rec.Status,
		rec.At, firstFailedAt, rec.NextAttemptAt, deliveredAt, code, failure, message, latency).Row().Scan(&inserted)
	if errors.Is(err, sql.ErrNoRows) {
		// The delivery was delivered already, by a resend.
		return nil
	}
	if err != nil {
		return fmt.Errorf("record webhook delivery: %w", err)
	}
	if !inserted {
		return nil
	}
	if err := r.db.WithContext(ctx).Exec(`DELETE FROM webhook_deliveries WHERE id IN (
			SELECT id FROM webhook_deliveries WHERE webhook_id = ? ORDER BY created_at DESC, id DESC OFFSET ?)`, rec.WebhookID, deliveriesKept).Error; err != nil {
		return fmt.Errorf("trim webhook deliveries: %w", err)
	}
	return nil
}

// deliveredAlready reports whether the delivery of message messageID to the webhook has succeeded: a resend from the webhooks page delivered it while its job was waiting to retry.
func (r *Repository) deliveredAlready(ctx context.Context, webhookID uuid.UUID, messageID string) (bool, error) {
	var done bool
	if err := r.db.WithContext(ctx).Raw(`SELECT EXISTS (SELECT 1 FROM webhook_deliveries WHERE webhook_id = ? AND message_id = ? AND status = 'succeeded')`,
		webhookID, messageID).Row().Scan(&done); err != nil {
		return false, fmt.Errorf("check webhook delivery: %w", err)
	}
	return done, nil
}

// abandon marks a delivery that was being retried as failed when its job stops retrying without an attempt: the webhook no longer subscribes to the event or the survey.
func (r *Repository) abandon(ctx context.Context, webhookID uuid.UUID, messageID string) error {
	if err := r.db.WithContext(ctx).Exec(`UPDATE webhook_deliveries SET status = 'failed', next_attempt_at = NULL WHERE webhook_id = ? AND message_id = ? AND status = 'retrying'`,
		webhookID, messageID).Error; err != nil {
		return fmt.Errorf("abandon webhook delivery: %w", err)
	}
	return nil
}

// storedDelivery is what a resend needs of a delivery.
type storedDelivery struct {
	ID        uuid.UUID
	Event     string
	MessageID string
	Payload   string
}

// storedDelivery is the webhook's delivery with id, or nil.
func (r *Repository) storedDelivery(ctx context.Context, webhookID, id uuid.UUID) (*storedDelivery, error) {
	var found []storedDelivery
	if err := r.db.WithContext(ctx).Raw(`SELECT id, event, message_id, payload FROM webhook_deliveries WHERE id = ? AND webhook_id = ?`, id, webhookID).Scan(&found).Error; err != nil {
		return nil, fmt.Errorf("load webhook delivery: %w", err)
	}
	if len(found) == 0 {
		return nil, nil
	}
	return &found[0], nil
}

// recordResend stores a resend's attempt. A success settles the delivery, and its job, if it is still waiting to retry, then finds it delivered and stops. A failure leaves a delivery that is still being retried to its job, keeps one that was delivered already delivered, with the failed attempt as its latest, and marks any other one failed.
func (r *Repository) recordResend(ctx context.Context, id uuid.UUID, at time.Time, a Attempt) error {
	code, failure, message, latency := attemptColumns(a)
	if err := r.db.WithContext(ctx).Exec(`UPDATE webhook_deliveries SET attempts = attempts + 1, last_attempted_at = ?,
			status = CASE WHEN ? THEN 'succeeded' WHEN status IN ('succeeded', 'retrying') THEN status ELSE 'failed' END,
			next_attempt_at = CASE WHEN ? THEN NULL ELSE next_attempt_at END,
			delivered_at = CASE WHEN ? THEN ? ELSE delivered_at END,
			first_failed_at = CASE WHEN ? THEN first_failed_at ELSE COALESCE(first_failed_at, ?) END,
			status_code = ?, failure = ?, failure_message = ?, latency_ms = ?
		WHERE id = ?`, at, a.OK(), a.OK(), a.OK(), at, a.OK(), at, code, failure, message, latency, id).Error; err != nil {
		return fmt.Errorf("record webhook resend: %w", err)
	}
	return nil
}

type deliveryRow struct {
	ID              uuid.UUID
	CreatedAt       time.Time
	Event           string
	ResponseID      uuid.UUID
	SurveyID        uuid.UUID
	SurveyName      *string
	MessageID       string
	Status          string
	Attempts        int
	LastAttemptedAt time.Time
	NextAttemptAt   *time.Time
	StatusCode      *int
	Failure         *string
	FailureMessage  *string
	LatencyMs       int
}

func (d deliveryRow) view() DeliveryView {
	return DeliveryView{
		ID: d.ID, CreatedAt: jsvalue.ISOString(d.CreatedAt), Event: d.Event, ResponseID: d.ResponseID, SurveyID: d.SurveyID, SurveyName: d.SurveyName,
		MessageID: d.MessageID, Status: d.Status, Attempts: d.Attempts, LastAttemptAt: jsvalue.ISOString(d.LastAttemptedAt), NextAttemptAt: isoString(d.NextAttemptAt),
		StatusCode: d.StatusCode, Failure: d.Failure, FailureMessage: d.FailureMessage, LatencyMs: d.LatencyMs,
	}
}

func isoString(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := jsvalue.ISOString(*t)
	return &s
}

const selectDelivery = `SELECT d.id, d.created_at, d.event, d.response_id, d.survey_id, s.name AS survey_name, d.message_id, d.status, d.attempts,
		d.last_attempted_at, d.next_attempt_at, d.status_code, d.failure, d.failure_message, d.latency_ms
	FROM webhook_deliveries d LEFT JOIN surveys s ON s.id = d.survey_id`

// deliveries is the webhook's kept deliveries, newest first.
func (r *Repository) deliveries(ctx context.Context, webhookID uuid.UUID) ([]DeliveryView, error) {
	var rows []deliveryRow
	if err := r.db.WithContext(ctx).Raw(selectDelivery+` WHERE d.webhook_id = ? ORDER BY d.created_at DESC, d.id DESC LIMIT ?`, webhookID, deliveriesKept).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load webhook deliveries: %w", err)
	}
	out := make([]DeliveryView, len(rows))
	for i, d := range rows {
		out[i] = d.view()
	}
	return out, nil
}

// delivery is the delivery with id.
func (r *Repository) delivery(ctx context.Context, id uuid.UUID) (*DeliveryView, error) {
	var rows []deliveryRow
	if err := r.db.WithContext(ctx).Raw(selectDelivery+` WHERE d.id = ?`, id).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load webhook delivery: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	v := rows[0].view()
	return &v, nil
}

type healthRow struct {
	WebhookID       uuid.UUID
	LastAttemptedAt time.Time
	NextAttemptAt   *time.Time
	StatusCode      *int
	Failure         *string
	FailureMessage  *string
	LastSuccessAt   *time.Time
	FailingSince    *time.Time
	FailedCount     int
	Retrying        bool
}

// health is the delivery health of each of the workspace's webhooks that was sent anything: the latest attempt, the last success, the first failure since, and the kept deliveries that were lost, however long ago.
func (r *Repository) health(ctx context.Context, workspaceID uuid.UUID) (map[uuid.UUID]*Health, error) {
	var rows []healthRow
	if err := r.db.WithContext(ctx).Raw(`SELECT w.id AS webhook_id, latest.last_attempted_at, latest.next_attempt_at, latest.status_code, latest.failure, latest.failure_message,
			ok.at AS last_success_at, bad.since AS failing_since, kept.failed AS failed_count, kept.retrying
		FROM webhooks w
		JOIN LATERAL (SELECT d.last_attempted_at, d.next_attempt_at, d.status_code, d.failure, d.failure_message FROM webhook_deliveries d
			WHERE d.webhook_id = w.id ORDER BY d.last_attempted_at DESC, d.id DESC LIMIT 1) latest ON true
		LEFT JOIN LATERAL (SELECT max(d.delivered_at) AS at FROM webhook_deliveries d WHERE d.webhook_id = w.id) ok ON true
		LEFT JOIN LATERAL (SELECT min(GREATEST(d.first_failed_at, ok.at)) AS since FROM webhook_deliveries d
			WHERE d.webhook_id = w.id AND d.status <> 'succeeded' AND (ok.at IS NULL OR d.last_attempted_at > ok.at)) bad ON true
		LEFT JOIN LATERAL (SELECT count(*) FILTER (WHERE d.status = 'failed') AS failed, bool_or(d.status = 'retrying') AS retrying FROM webhook_deliveries d
			WHERE d.webhook_id = w.id) kept ON true
		WHERE w.workspace_id = ?`, workspaceID).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load webhook health: %w", err)
	}
	out := make(map[uuid.UUID]*Health, len(rows))
	for _, x := range rows {
		h := &Health{
			LastAttemptAt: jsvalue.ISOString(x.LastAttemptedAt), LastSuccessAt: isoString(x.LastSuccessAt), FailingSince: isoString(x.FailingSince),
			NextAttemptAt: isoString(x.NextAttemptAt), StatusCode: x.StatusCode, Failure: x.Failure, FailureMessage: x.FailureMessage, FailedCount: x.FailedCount,
		}
		latestFailed := x.Failure != nil
		switch {
		case latestFailed && x.FailedCount == 0 && x.Retrying:
			h.Status = HealthRetrying
		case latestFailed:
			h.Status = HealthFailing
		case x.FailedCount > 0:
			h.Status, h.FailingSince = HealthLost, nil
		default:
			h.Status, h.FailingSince = HealthHealthy, nil
		}
		out[x.WebhookID] = h
	}
	return out, nil
}

// setSecret replaces the webhook's signing secret.
func (r *Repository) setSecret(ctx context.Context, id uuid.UUID, secret string) error {
	if err := r.db.WithContext(ctx).Exec(`UPDATE webhooks SET secret = ?, updated_at = now() WHERE id = ?`, secret, id).Error; err != nil {
		return fmt.Errorf("set webhook secret: %w", err)
	}
	return nil
}

// Deliveries is the webhook's delivery log, its kept deliveries newest first: workspace.read on its workspace, as the webhooks page is.
func (s *Service) Deliveries(ctx context.Context, userID, webhookID uuid.UUID) ([]DeliveryView, error) {
	if _, err := s.owned(ctx, userID, webhookID, authz.WorkspaceRead); err != nil {
		return nil, err
	}
	return s.repo.deliveries(ctx, webhookID)
}

// Resend sends a delivery again, now, to the webhook as it is: its current URL and secret, with the body and the webhook-id of the original, so a receiver that already has it can tell. It needs workspace.write on the webhook's workspace, as the endpoint test does, and counts against the state-mutation limit. The attempt is recorded like a retry, and the answer is the delivery after it, whether the endpoint accepted it or not.
func (s *Service) Resend(ctx context.Context, userID, webhookID, deliveryID uuid.UUID) (*DeliveryView, error) {
	w, err := s.owned(ctx, userID, webhookID, authz.WorkspaceWrite)
	if err != nil {
		return nil, err
	}
	if err := s.limiter.Apply(ctx, ratelimit.ActionStateMutation, w.WorkspaceID.String()); err != nil {
		return nil, err
	}
	d, err := s.repo.storedDelivery(ctx, webhookID, deliveryID)
	if err != nil {
		return nil, err
	}
	if d == nil {
		return nil, apperr.NotFound("webhook delivery", deliveryID.String())
	}
	a, err := s.deliverer().attempt(ctx, w, d.Event, d.MessageID, json.RawMessage(d.Payload))
	if err != nil {
		return nil, err
	}
	if err := s.repo.recordResend(ctx, d.ID, s.now(), a); err != nil {
		return nil, err
	}
	log := s.log.With("webhook_id", w.ID, "delivery_id", d.ID, "event", d.Event, "workspace_id", w.WorkspaceID)
	if a.OK() {
		log.InfoContext(ctx, "Webhook delivery resent")
	} else {
		log.WarnContext(ctx, "Webhook delivery resend failed", "err", a.err(w))
	}
	v, err := s.repo.delivery(ctx, d.ID)
	if err != nil {
		return nil, err
	}
	if v == nil {
		// The response was deleted while the request was out, and the delivery with it.
		return nil, apperr.NotFound("webhook delivery", deliveryID.String())
	}
	return v, nil
}

// deliverer sends the service's resends, through the same SSRF guard and clock as the service.
func (s *Service) deliverer() *Deliverer {
	return &Deliverer{repo: s.repo, guard: s.guard, now: s.now}
}

// RegenerateSecret replaces a webhook's signing secret with a new one and returns it, the only time it is shown: workspace.write on the webhook's workspace and the state-mutation limit. Every delivery from then on is signed with it, the retries of earlier events included. A webhook an automation platform subscribed keeps its secret, which the platform received when it subscribed.
func (s *Service) RegenerateSecret(ctx context.Context, userID, id uuid.UUID) (*SecretView, error) {
	var before, after *Webhook
	secret := GenerateSecret()
	err := func() error {
		var err error
		if before, err = s.owned(ctx, userID, id, authz.WorkspaceWrite); err != nil {
			return err
		}
		if err := s.limiter.Apply(ctx, ratelimit.ActionStateMutation, before.WorkspaceID.String()); err != nil {
			return err
		}
		if before.Source != SourceUser {
			return apperr.InvalidInput(CodeThirdPartySecret, "The secret of a webhook an automation platform created belongs to that platform")
		}
		if err := s.repo.setSecret(ctx, id, secret); err != nil {
			return err
		}
		after, err = s.repo.ByID(ctx, id)
		return err
	}()
	s.auditSecret(ctx, id, before, after, err)
	if err != nil {
		return nil, err
	}
	return &SecretView{Secret: secret}, nil
}

// auditSecret records a regenerated secret as an update of the webhook. The emitter masks the secret on both sides, so secretRegenerated says what changed.
func (s *Service) auditSecret(ctx context.Context, id uuid.UUID, before, after *Webhook, err error) {
	e := audit.Event{Action: audit.ActionUpdated, TargetType: audit.TargetWebhook, TargetID: id.String(), ActorType: audit.ActorUser, Status: audit.StatusSuccess}
	if before != nil {
		if org, orgErr := s.repo.organizationOf(ctx, before.WorkspaceID); orgErr == nil && org != nil {
			e.OrganizationID = org.String()
		}
	}
	if err == nil && before != nil && after != nil {
		oldObject, newObject := before.auditObject(), after.auditObject()
		oldObject["secretRegenerated"], newObject["secretRegenerated"] = false, true
		e.OldObject, e.NewObject = oldObject, newObject
	} else {
		e.Status = audit.StatusFailure
	}
	s.audit.Log(ctx, audit.FromRequest(ctx, e))
}
