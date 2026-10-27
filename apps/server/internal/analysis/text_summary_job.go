package analysis

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/jobs"
	"github.com/yldm-tech/forma/apps/server/internal/response"
)

// TextSummaryArgs is the generation of one AI text summary, queued by TextSummaries.Generate: the input it was asked with, whether it refreshes a current summary, and when it was asked for. Key, the cache key of the summary, is its identity: a generation of the same summary queued or running is not queued again.
type TextSummaryArgs struct {
	Key            string             `json:"key" river:"unique"`
	SurveyID       uuid.UUID          `json:"surveyId"`
	ElementID      string             `json:"elementId"`
	Target         string             `json:"target"`
	Language       string             `json:"language"`
	FilterCriteria *response.Criteria `json:"filterCriteria,omitempty"`
	// Criteria is FilterCriteria encoded as it was asked with, which Key was made from. FilterCriteria comes back from River's jsonb with its escapes decoded and its objects' keys reordered, so it cannot make Key again.
	Criteria    string    `json:"criteria"`
	Refresh     bool      `json:"refresh,omitempty"`
	RequestedAt time.Time `json:"requestedAt"`
}

func (TextSummaryArgs) Kind() string { return "analysis.text-summary" }

// textSummaryUniqueStates are the states in which a generation keeps another of the same summary from being queued: River's required states and retryable, but not the finished ones, so the summary can be asked for again once its answers change.
var textSummaryUniqueStates = []rivertype.JobState{
	rivertype.JobStateAvailable,
	rivertype.JobStatePending,
	rivertype.JobStateRetryable,
	rivertype.JobStateRunning,
	rivertype.JobStateScheduled,
}

// InsertOpts puts a generation on the ai queue and makes it a single attempt: a retry would pay the provider again for a summary the member can ask for again.
func (TextSummaryArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: jobs.QueueAI, MaxAttempts: 1, UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: textSummaryUniqueStates}}
}

// textSummaryWorker runs TextSummaryArgs jobs. A failure is recorded as the summary's status for the card to show; an expected one (AI switched off since, no answers left, the provider rate limiting the instance) ends the job without an error.
type textSummaryWorker struct {
	river.WorkerDefaults[TextSummaryArgs]
	texts *TextSummaries
}

func (w *textSummaryWorker) Timeout(*river.Job[TextSummaryArgs]) time.Duration { return textJobTimeout }

func (w *textSummaryWorker) Work(ctx context.Context, job *river.Job[TextSummaryArgs]) error {
	err := w.texts.run(ctx, job.Args)
	if err == nil {
		return nil
	}
	w.texts.fail(ctx, job.Args.Key, err)
	var expected *apperr.Error
	if errors.As(err, &expected) {
		w.texts.log.InfoContext(ctx, "text summary not generated", "survey", job.Args.SurveyID, "code", expected.Code)
		return nil
	}
	return err
}

// RegisterJobs registers the AI text summary generation with `forma worker`.
func RegisterJobs(reg *jobs.Registry, texts *TextSummaries) {
	jobs.Add(reg, &textSummaryWorker{texts: texts})
}
