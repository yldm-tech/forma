package pipeline

import (
	"context"
	"errors"

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

// ErrRetry marks a hand-off failure that should retry the pipeline job.
var ErrRetry = errors.New("retry the response pipeline job")

// Handoffs are the consumers later modules register; a nil one is skipped.
type Handoffs struct {
	Integrations IntegrationHandler
	Workflows    WorkflowEnqueuer
}
