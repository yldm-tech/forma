package response

import (
	"github.com/google/uuid"
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

// Kind is forma2's job name; the pipeline module works it.
func (PipelineArgs) Kind() string { return "response-pipeline.process" }

// DeleteFilesArgs is the cleanup of a deleted response's uploads: the storage URLs its file upload answers held, to delete from the survey's workspace, forma2's deleteResponseFileUrls. The storage module works it; a URL whose storage id does not resolve to WorkspaceID must be left alone, since response data is written by respondents.
type DeleteFilesArgs struct {
	WorkspaceID uuid.UUID `json:"workspaceId"`
	FileURLs    []string  `json:"fileUrls"`
}

func (DeleteFilesArgs) Kind() string { return "response.delete-files" }
