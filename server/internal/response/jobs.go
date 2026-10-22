package response

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
	"github.com/riverqueue/river"

	"github.com/yldm-tech/forma/server/internal/platform/jobs"
)

// Response pipeline events, forma2's ZResponsePipelineEvent.
const (
	EventResponseCreated  = "responseCreated"
	EventResponseUpdated  = "responseUpdated"
	EventResponseFinished = "responseFinished"
)

// PipelineArgs is the response pipeline job, forma2's response-pipeline.process (TResponsePipelineJobData): what runs after a response is written (webhooks, integrations, follow-up emails, workflows, notifications). It is enqueued in the transaction that writes the response, so it commits with the response and is never lost; forma2 enqueued after commit and kept an outbox for the events its queue refused.
//
// A create enqueues responseCreated and, when the submitted response is finished, responseFinished; an update enqueues responseUpdated and, when the updated response is finished, responseFinished. Response is the response as that write left it, before a quota's endSurvey action finished it: as in forma2, a quota-forced finish fires no responseFinished and the payload shows finished false. Locale is the respondent's resolved locale, for the follow-up email chrome; empty when none was resolved.
type PipelineArgs struct {
	Event       string    `json:"event"`
	WorkspaceID uuid.UUID `json:"workspaceId"`
	SurveyID    uuid.UUID `json:"surveyId"`
	Response    Response  `json:"response"`
	Locale      string    `json:"locale,omitempty"`
}

func (PipelineArgs) Kind() string { return "response-pipeline.process" }

// pipelineWorker accepts pipeline jobs until the pipeline itself is ported (P4), which replaces it.
type pipelineWorker struct {
	river.WorkerDefaults[PipelineArgs]
	log *slog.Logger
}

func (w *pipelineWorker) Work(ctx context.Context, job *river.Job[PipelineArgs]) error {
	w.log.DebugContext(ctx, "response pipeline event has no consumer yet", "event", job.Args.Event, "response_id", job.Args.Response.ID, "survey_id", job.Args.SurveyID)
	return nil
}

// DeleteFilesArgs is the cleanup of a deleted response's uploads: the storage URLs its file upload answers held, to delete from the survey's workspace, forma2's deleteResponseFileUrls. The storage module works it; a URL whose storage id does not resolve to WorkspaceID must be left alone, since response data is written by respondents.
type DeleteFilesArgs struct {
	WorkspaceID uuid.UUID `json:"workspaceId"`
	FileURLs    []string  `json:"fileUrls"`
}

func (DeleteFilesArgs) Kind() string { return "response.delete-files" }

// RegisterJobs adds the module's background work to `forma worker`.
func RegisterJobs(reg *jobs.Registry, log *slog.Logger) {
	jobs.Add(reg, &pipelineWorker{log: log})
}
