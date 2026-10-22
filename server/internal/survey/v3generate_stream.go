package survey

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/ai"
	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/platform/v3api"
)

// This file is forma2's POST /api/internal/surveys/generate/stream: the create-with-AI dialog's generation, streamed as NDJSON so questions appear while the model writes them. Every guard runs before the body opens, because once a 200 has begun there is no way back to an error response; only failures during the generation become in-band error events.

// GenerationStreamContentType is forma2's SURVEY_GENERATION_STREAM_CONTENT_TYPE.
const GenerationStreamContentType = "application/x-ndjson; charset=utf-8"

// snapshotThrottle is forma2's SURVEY_GENERATION_SNAPSHOT_THROTTLE_MS: about ten snapshots a second reads as live without a frame per token.
const snapshotThrottle = 100 * time.Millisecond

// Stream error codes, forma2's SURVEY_GENERATION_STREAM_ERROR_CODES, and their fixed details: an in-band error is shown to the user and never repeats the provider's message, which can echo the prompt.
const (
	streamQuotaExceeded    = "ai_quota_exceeded"
	streamOutputTooLong    = "ai_output_too_long"
	streamPayloadInvalid   = "ai_generated_payload_invalid"
	streamGenerationFailed = "ai_generation_failed"
)

var streamErrorDetails = map[string]string{
	streamQuotaExceeded:    "The AI provider is temporarily rate-limited. Try again shortly.",
	streamOutputTooLong:    "The generated survey exceeded the AI output token limit. Simplify the prompt or split it into smaller surveys.",
	streamPayloadInvalid:   "The generated survey draft could not be validated.",
	streamGenerationFailed: "The AI provider could not finish the survey draft. Try again or add more detail.",
}

type streamStartEvent struct {
	Type      string `json:"type"`
	RequestID string `json:"requestId"`
}

type streamPartialEvent struct {
	Type  string          `json:"type"`
	Seq   int             `json:"seq"`
	Draft json.RawMessage `json:"draft"`
}

type streamTranslatingEvent struct {
	Type         string `json:"type"`
	LanguageCode string `json:"languageCode"`
	Index        int    `json:"index"`
	Total        int    `json:"total"`
}

type streamDoneEvent struct {
	Type string `json:"type"`
	GenerateResult
}

type streamErrorEvent struct {
	Type          string               `json:"type"`
	Code          string               `json:"code"`
	Detail        string               `json:"detail"`
	InvalidParams []v3api.InvalidParam `json:"invalid_params,omitempty"`
	RetryAfter    *int                 `json:"retryAfter,omitempty"`
}

// streamErrorEvent is forma2's toStreamErrorEvent.
func toStreamErrorEvent(err error) streamErrorEvent {
	code := streamGenerationFailed
	event := streamErrorEvent{Type: "error"}
	var payloadErr *generatedPayloadError
	var tokenErr *ai.OutputTokenLimitError
	if e, ok := apperr.As(err); ok && e.Status == http.StatusTooManyRequests {
		code = streamQuotaExceeded
		if e.RetryAfter > 0 {
			retry := e.RetryAfter
			event.RetryAfter = &retry
		}
	} else if errors.As(err, &tokenErr) {
		code = streamOutputTooLong
	} else if errors.As(err, &payloadErr) {
		code = streamPayloadInvalid
		event.InvalidParams = payloadErr.params
	}
	event.Code, event.Detail = code, streamErrorDetails[code]
	return event
}

// shouldEmitSnapshot is forma2's: a snapshot identical to the last one never goes out, the first always does, and the rest at most once per throttle window.
func shouldEmitSnapshot(now time.Time, lastEmittedAt *time.Time, serialized, lastSerialized string) bool {
	if serialized == lastSerialized {
		return false
	}
	if lastEmittedAt == nil {
		return true
	}
	return now.Sub(*lastEmittedAt) >= snapshotThrottle
}

// GenerationStream is a survey generation whose guards have all passed: the caller may write to the workspace, the prompt has enough detail, and AI is available to the organization.
type GenerationStream struct {
	svc            *Service
	in             *GenerateInput
	organizationID uuid.UUID
}

// StartGenerationStream is the part of forma2's streamV3SurveyGeneration that runs before the body opens: workspace.write, the prompt's detail, then the organization's AI. Its errors are the ones forma2 answered with a problem response.
func (s *Service) StartGenerationStream(ctx context.Context, caller Caller, in *GenerateInput) (*GenerationStream, error) {
	organizationID, err := s.generationOrganization(ctx, caller, in)
	if err != nil {
		return nil, err
	}
	if err := promptDetailProblem(in.Prompt); err != nil {
		return nil, err
	}
	if _, err := s.ai.Assert(ctx, organizationID); err != nil {
		return nil, s.generationProblem(ctx, err, in.WorkspaceID, organizationID)
	}
	return &GenerationStream{svc: s, in: in, organizationID: organizationID}, nil
}

// Run streams the generation through emit: start, the draft's snapshots, the completed draft as the final snapshot, one translating event per further language, then done; or an error event in place of whatever could not be finished. A cancelled context is the client hanging up: nothing more is emitted and the provider call stops with it.
func (g *GenerationStream) Run(ctx context.Context, requestID string, emit func(event any)) {
	s := g.svc
	emit(streamStartEvent{Type: "start", RequestID: requestID})
	seq := 0
	var lastEmittedAt *time.Time
	lastSerialized := ""
	partial := func(serialized string) {
		seq++
		emit(streamPartialEvent{Type: "partial", Seq: seq, Draft: json.RawMessage(serialized)})
		lastSerialized = serialized
	}
	out, err := s.ai.StreamObject(ctx, g.organizationID, ai.FeatureSurveyGeneration, generationRequest(g.in), func(snapshot any) {
		serialized := string(encodeValue(snapshot))
		now := s.now()
		if !shouldEmitSnapshot(now, lastEmittedAt, serialized, lastSerialized) {
			return
		}
		lastEmittedAt = &now
		partial(serialized)
	})
	if err == nil {
		// The completed draft always lands as the last snapshot: the last streamed one can lack fields the finished object has, and a provider may stream none at all.
		var final any
		if json.Unmarshal(out.Object, &final) == nil {
			if serialized := string(encodeValue(final)); serialized != lastSerialized {
				partial(serialized)
			}
		}
		var res *GenerateResult
		res, err = s.finishGeneration(ctx, g.organizationID, g.in, out.Object, func(languageCode string, index, total int) {
			emit(streamTranslatingEvent{Type: "translating", LanguageCode: languageCode, Index: index, Total: total})
		})
		if err == nil {
			emit(streamDoneEvent{Type: "done", GenerateResult: *res})
			return
		}
	}
	if ai.IsAbort(ctx, err) {
		s.log.InfoContext(ctx, "AI survey generation aborted by the client", "workspaceId", g.in.WorkspaceID.String(), "organizationId", g.organizationID.String())
		return
	}
	s.log.ErrorContext(ctx, "AI survey generation stream failed", "workspaceId", g.in.WorkspaceID.String(), "organizationId", g.organizationID.String(), "err", err)
	emit(toStreamErrorEvent(err))
}
