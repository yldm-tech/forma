package pipeline

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/response"
)

// Event is one response pipeline event as its consumers see it: the job's payload, the key that identifies the job across its attempts, and what the pipeline loaded for it.
type Event struct {
	// Name is responseCreated, responseUpdated or responseFinished.
	Name string
	// JobKey is forma2's deterministic job id, response-pipeline-<event>-<response id>-<updatedAt ms>: the same on every attempt of the job and for an event enqueued twice for the same write.
	JobKey      string
	WorkspaceID uuid.UUID
	// Response is the response as the write that caused the event left it.
	Response response.Response
	// Locale is the respondent's resolved locale; empty when none was resolved.
	Locale         string
	OrganizationID uuid.UUID
	// DisplayTimeZone is the organization's IANA zone for human-facing timestamps, "UTC" when it has none.
	DisplayTimeZone  string
	StripeCustomerID *string
	Survey           *Survey
}

// IntegrationHandler delivers a finished response to the workspace's integrations (Google Sheets, Slack, Airtable, Notion), forma2's handleIntegrations. The pipeline calls it once per responseFinished event when one is registered; it loads the workspace's integrations itself. A returned error is logged and never retried: integration writes append rows, so a retry would duplicate them.
type IntegrationHandler interface {
	HandleResponse(ctx context.Context, ev *Event) error
}

// WorkflowEnqueuer starts the response.completed runs of the workspace's enabled workflows for a finished response, forma2's enqueueResponseCompletedWorkflowRuns. The pipeline calls it first among the side effects of a responseFinished event, only for a finished response, because it is the one step built to be replayed: runs are keyed by (workflow, response id). Return an error wrapping ErrRetry to have the whole job retried (forma2 retried on database pool exhaustion); any other error is logged and the remaining side effects still run.
type WorkflowEnqueuer interface {
	EnqueueResponseCompleted(ctx context.Context, ev *Event) error
}

// UsageMeter records billable usage, forma2's Stripe metering: one meter event per created response, which the pipeline reports after a responseCreated event's webhooks, and one per new workflow run that is not a dry run, which the workflow runner reports. It never fails its caller; a failure is logged where it happens. stripeCustomerID is the organization's, nil when it has none.
type UsageMeter interface {
	ResponseCreated(ctx context.Context, stripeCustomerID *string, responseID uuid.UUID, createdAt time.Time)
	WorkflowRunCreated(ctx context.Context, stripeCustomerID *string, runID uuid.UUID, createdAt time.Time)
}

// UsageReporter sends the instance's daily anonymous usage update when its operator opted in, forma2's sendTelemetryEvents. The pipeline calls it after every responseCreated event, so an instance in use reports without waiting for the daily job; it decides itself whether a report is due and never fails its caller.
type UsageReporter interface {
	Send(ctx context.Context)
}

// ErrRetry marks a hand-off failure that should retry the pipeline job.
var ErrRetry = errors.New("retry the response pipeline job")

// Handoffs are the consumers later modules register; a nil one is skipped.
type Handoffs struct {
	Integrations IntegrationHandler
	Workflows    WorkflowEnqueuer
	Meter        UsageMeter
	UsageReport  UsageReporter
}
