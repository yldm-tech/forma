// Package response is response collection: displays and responses as the survey runtime creates and updates them through the client API, with every rule forma2 applied on the way in (the Embedded Data ingest contract, meta capture and anonymization, the contact snapshot, single-use links, display ownership, quotas and the response pipeline), and the operations the response views need (list, filter, read, delete, tag).
//
// The client API handlers own the request (body limits, input schemas, rate limits, CORS) and the checks of other modules (PIN, verified email, reCAPTCHA, file uploads, answer validation); they call this package's steps in forma2's order. Errors meant for a client API caller are *apperr.Error values with forma2's client codes, messages and details.
package response

import (
	"encoding/json"
	"net/http"
	"regexp"
	"time"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/platform/legacyid"
	"github.com/yldm-tech/forma/server/internal/tag"
)

// Contact is forma2's TResponseContact: the contact a response belongs to and its userId attribute.
type Contact struct {
	ID     uuid.UUID `json:"id"`
	UserID *string   `json:"userId,omitempty"`
}

// Response is forma2's TResponse. The JSON columns are passed through as stored.
type Response struct {
	ID                uuid.UUID       `json:"id"`
	CreatedAt         time.Time       `json:"createdAt"`
	UpdatedAt         time.Time       `json:"updatedAt"`
	SurveyID          uuid.UUID       `json:"surveyId"`
	DisplayID         *uuid.UUID      `json:"displayId"`
	Contact           *Contact        `json:"contact"`
	ContactAttributes json.RawMessage `json:"contactAttributes"`
	Finished          bool            `json:"finished"`
	EndingID          *string         `json:"endingId"`
	Data              json.RawMessage `json:"data"`
	Variables         json.RawMessage `json:"variables"`
	TTC               json.RawMessage `json:"ttc"`
	Tags              []tag.Tag       `json:"tags"`
	Meta              json.RawMessage `json:"meta"`
	SingleUseID       *string         `json:"singleUseId"`
	Language          *string         `json:"language"`
}

// QuotaRef is a quota a response is screened into, as the response views list it.
type QuotaRef struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// WithQuotas is forma2's TResponseWithQuotas: a response with the quotas it is screened into.
type WithQuotas struct {
	Response
	Quotas []QuotaRef `json:"quotas"`
}

// QuotaFull is forma2's createQuotaFullObject: what the client is told about a full quota.
type QuotaFull struct {
	QuotaFull bool       `json:"quotaFull"`
	Quota     *FullQuota `json:"quota,omitempty"`
}

// FullQuota is the quota that screened a response out; EndingCardID is left out when it is empty.
type FullQuota struct {
	ID           uuid.UUID `json:"id"`
	Action       string    `json:"action"`
	EndingCardID string    `json:"endingCardId,omitempty"`
}

// Result is the client API's answer to a create or an update: `{id, quotaFull, quota?}`.
type Result struct {
	ID uuid.UUID `json:"id"`
	QuotaFull
}

// Client API error codes and the messages forma2's client routes answered with.
const (
	codeBadRequest = apperr.CodeBadRequest
	codeForbidden  = apperr.CodeForbidden
	codeNotFound   = apperr.CodeNotFound
	codeConflict   = apperr.CodeConflict

	// CodeResponseAlreadyFinished is forma2's RESPONSE_ALREADY_FINISHED_ERROR_CODE, the details.code the runtime keys off to stop retrying.
	CodeResponseAlreadyFinished = "response_already_finished"
)

func details(kv ...any) map[string]any {
	d := map[string]any{}
	for i := 0; i < len(kv); i += 2 {
		d[kv[i].(string)] = kv[i+1]
	}
	return d
}

// badRequest is forma2's responses.badRequestResponse.
func badRequest(message string, d map[string]any) *apperr.Error {
	if d == nil {
		d = map[string]any{}
	}
	return &apperr.Error{Status: http.StatusBadRequest, Code: codeBadRequest, Message: message, Details: d}
}

// forbidden is forma2's responses.forbiddenResponse.
func forbidden(message string, d map[string]any) *apperr.Error {
	if d == nil {
		d = map[string]any{}
	}
	return &apperr.Error{Status: http.StatusForbidden, Code: codeForbidden, Message: message, Details: d}
}

// NotFound is forma2's responses.notFoundResponse: "<Type> not found" with the resource the caller named. A nil id is reported as null.
func NotFound(resourceType string, id *string) *apperr.Error {
	var rid any
	if id != nil {
		rid = *id
	}
	return &apperr.Error{Status: http.StatusNotFound, Code: codeNotFound, Message: resourceType + " not found", Details: map[string]any{"resource_id": rid, "resource_type": resourceType}}
}

func conflict(message string) *apperr.Error {
	return &apperr.Error{Status: http.StatusConflict, Code: codeConflict, Message: message, Details: map[string]any{}}
}

// cuid2 is zod's cuid2 shape, every forma2 id.
var cuid2 = regexp.MustCompile(`^[0-9a-z]+$`)

// ResolveID reads an id a client sent: a UUID as it is, a forma2 cuid through the legacy id function. Anything else is forma2's validateInputs failure on a ZId, "Validation failed: Invalid cuid2".
func ResolveID(raw string) (uuid.UUID, error) {
	if _, err := uuid.Parse(raw); (err != nil || len(raw) != 36) && !cuid2.MatchString(raw) {
		return uuid.Nil, badRequest("Validation failed: Invalid cuid2", nil)
	}
	id, _ := legacyid.Resolve(raw)
	return id, nil
}
