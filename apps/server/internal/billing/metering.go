package billing

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v84"

	"github.com/yldm-tech/forma/server/internal/platform/config"
)

// Meter events, forma2's metering: Stripe aggregates them per customer and billing period against the plan's meters and bills what exceeds the included volume. There is no cap; nothing is refused for being over a limit.
const (
	meterResponseCreated    = "response_created"
	meterWorkflowRunCreated = "workflow_run_created"
)

// Meter records usage in Stripe on Forma Cloud. The response pipeline calls it for every created response, the workflow runner for every new run that is not a dry run.
type Meter struct {
	enabled bool
	stripe  *Stripe
	log     *slog.Logger
}

func NewMeter(cfg config.Config, s *Stripe, log *slog.Logger) *Meter {
	return &Meter{enabled: cfg.IsFormaCloud && s.Configured(), stripe: s, log: log}
}

// ResponseCreated is forma2's recordResponseCreatedMeterEvent.
func (m *Meter) ResponseCreated(ctx context.Context, stripeCustomerID *string, responseID uuid.UUID, createdAt time.Time) {
	m.record(ctx, meterResponseCreated, stripeCustomerID, responseID, createdAt)
}

// WorkflowRunCreated is forma2's recordWorkflowRunCreatedMeterEvent.
func (m *Meter) WorkflowRunCreated(ctx context.Context, stripeCustomerID *string, runID uuid.UUID, createdAt time.Time) {
	m.record(ctx, meterWorkflowRunCreated, stripeCustomerID, runID, createdAt)
}

// record sends one meter event. The identifier makes Stripe drop a repeat of the same event, and the timestamp puts the usage in the period it happened in. A failure is logged and never reaches the caller: metering must not block a response or a run.
func (m *Meter) record(ctx context.Context, eventName string, stripeCustomerID *string, id uuid.UUID, createdAt time.Time) {
	if !m.enabled || stripeCustomerID == nil || *stripeCustomerID == "" {
		return
	}
	params := &stripe.BillingMeterEventCreateParams{
		EventName:  stripe.String(eventName),
		Identifier: stripe.String(eventName + ":" + id.String()),
		Payload:    map[string]string{"stripe_customer_id": *stripeCustomerID, "value": "1"},
	}
	if !createdAt.IsZero() {
		params.Timestamp = stripe.Int64(createdAt.Unix())
	}
	if _, err := m.stripe.api.V1BillingMeterEvents.Create(ctx, params); err != nil {
		m.log.WarnContext(ctx, "Failed to record Stripe meter event for "+eventName, "err", err, "id", id.String())
	}
}
