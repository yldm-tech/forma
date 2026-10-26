package response

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/embeddeddata"
	"github.com/yldm-tech/forma/apps/server/internal/langcode"
	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/jobs"
	"github.com/yldm-tech/forma/apps/server/internal/platform/jsvalue"
	"github.com/yldm-tech/forma/apps/server/internal/tag"
)

// MaxTTC is forma2's MAX_RESPONSE_TTC: one element's time to complete is clamped to [0, 24 h] in milliseconds.
const MaxTTC = 1000 * 60 * 60 * 24

// CreateInput is a new response as the client API validated it (ZResponseInput, or ZResponseInputV2) and after its checks ran.
type CreateInput struct {
	WorkspaceID uuid.UUID
	// UserID is v1's userId: the response is attached to the workspace contact whose userId attribute holds it, when there is one. No contact is created on this path.
	UserID *string
	// ContactID is v2's contactId: the response is attached to that contact when it exists in the workspace, and is created without one when it does not.
	ContactID *uuid.UUID
	// DisplayID is the display the response answers, as the client sent it.
	DisplayID *string
	// SingleUseID is the id SingleUseID returned.
	SingleUseID *string
	Finished    bool
	EndingID    *string
	Language    *string
	// Data is the data Ingest returned, with whatever the checks after it wrote (the verified email).
	Data        map[string]any
	IngestFlags []embeddeddata.Flag
	// Variables is nil when the client sent none.
	Variables map[string]any
	// TTC is nil when the client sent none.
	TTC     map[string]float64
	Meta    ClientMeta
	Request RequestInfo
	// Locale is the respondent's resolved locale, carried to the pipeline.
	Locale string
}

// normalizeLanguage is forma2's normalizeResponseLanguage: trimmed, blank as absent, a known code in its canonical form and anything else as it came.
func normalizeLanguage(language *string) *string {
	if language == nil {
		return nil
	}
	trimmed := jsvalue.Trim(*language)
	if trimmed == "" {
		return nil
	}
	if canonical := langcode.Normalize(trimmed); canonical != "" {
		return &canonical
	}
	return &trimmed
}

// clampTTC is ZResponseTtcInput: every value into [0, MaxTTC].
func clampTTC(ttc map[string]float64) map[string]float64 {
	out := make(map[string]float64, len(ttc))
	for k, v := range ttc {
		out[k] = min(max(v, 0), MaxTTC)
	}
	return out
}

// assertDisplayOwnership is forma2's assertDisplayOwnership: a response may answer a display only of its own workspace and survey, not yet answered, and shown to no other contact. Each failure is a 400 naming the display as the client sent it.
func assertDisplayOwnership(ctx context.Context, tx *jobs.Tx, raw string, workspaceID, surveyID uuid.UUID, contactID *uuid.UUID) (uuid.UUID, error) {
	id, err := ResolveID(raw)
	if err != nil {
		return uuid.Nil, err
	}
	d, err := loadDisplayForResponse(ctx, tx.DB, id)
	if err != nil {
		return uuid.Nil, err
	}
	refuse := func(why string) error { return badRequest(fmt.Sprintf("Display %s %s", raw, why), nil) }
	switch {
	case d == nil:
		return uuid.Nil, refuse("not found")
	case d.WorkspaceID != workspaceID:
		return uuid.Nil, refuse("belongs to a different workspace")
	case d.SurveyID != surveyID:
		return uuid.Nil, refuse("is associated with a different survey")
	case d.ResponseID != nil:
		return uuid.Nil, refuse("is already linked to a response")
	case d.ContactID != nil && (contactID == nil || *d.ContactID != *contactID):
		return uuid.Nil, refuse("belongs to a different contact")
	}
	return id, nil
}

// createError is forma2's handleClientResponseCreateError for a unique violation: the display's race is the ownership message, a reused single-use link and any other duplicate are 409.
func createError(err error, displayID *string) error {
	constraint, ok := uniqueViolation(err)
	if !ok {
		return err
	}
	switch constraint {
	case constraintDisplay:
		raw := ""
		if displayID != nil {
			raw = *displayID
		}
		return badRequest(fmt.Sprintf("Display %s is already linked to a response", raw), nil)
	case constraintSingleUse:
		return conflict("Response already submitted for this single-use link")
	}
	return conflict("Response already exists")
}

// Create is the write of forma2's client response create (createResponseWithQuotaEvaluation, then sendToPipeline), in one transaction: the meta is captured, the contact resolved and snapshotted, the display's ownership checked, the response inserted, the survey's quotas evaluated and the pipeline events enqueued. A quota that ends the survey is reported in the result.
func (s *Service) Create(ctx context.Context, sv *Survey, in CreateInput) (*Result, error) {
	meta := captureMeta(ctx, sv, in.Meta, in.Request)
	ttc := map[string]float64{}
	if in.TTC != nil {
		ttc = clampTTC(in.TTC)
		if in.Finished {
			ttc = ttcTotal(ttc)
		}
	}
	language := normalizeLanguage(in.Language)
	variables := in.Variables
	if variables == nil {
		variables = map[string]any{}
	}
	data := in.Data
	if data == nil {
		data = map[string]any{}
	}
	flags := in.IngestFlags
	if flags == nil {
		flags = []embeddeddata.Flag{}
	}

	var result *Result
	err := s.jobs.Transaction(ctx, s.db, func(tx *jobs.Tx) error {
		var owner *contactRef
		var ownershipContact *uuid.UUID
		switch {
		case in.UserID != nil && *in.UserID != "":
			id, err := contactIDByUserID(ctx, tx.DB, in.WorkspaceID, *in.UserID)
			if err != nil {
				return err
			}
			if id != nil {
				owner = &contactRef{ID: *id}
				ownershipContact = id
			}
		case in.ContactID != nil:
			ownershipContact = in.ContactID
			exists, err := contactInWorkspace(ctx, tx.DB, in.WorkspaceID, *in.ContactID)
			if err != nil {
				return err
			}
			if exists {
				owner = &contactRef{ID: *in.ContactID}
			}
		}
		if owner != nil {
			attrs, err := contactAttributes(ctx, tx.DB, owner.ID)
			if err != nil {
				return err
			}
			owner.Attributes = attrs
		}

		var displayID *uuid.UUID
		if in.DisplayID != nil && *in.DisplayID != "" {
			id, err := assertDisplayOwnership(ctx, tx, *in.DisplayID, in.WorkspaceID, sv.ID, ownershipContact)
			if err != nil {
				return err
			}
			displayID = &id
		}

		if email := sv.oneResponseEmail(data); email != "" {
			if err := claimVerifiedEmail(ctx, tx, sv.ID, email); err != nil {
				return err
			}
		}

		row, err := insertResponse(ctx, tx, sv.ID, owner, displayID, in, language, data, variables, ttc, meta, flags)
		if err != nil {
			return createError(err, in.DisplayID)
		}

		quotaLanguage := "default"
		if language != nil {
			quotaLanguage = *language
		}
		full := s.evaluateQuotas(ctx, tx.DB, sv, screening{
			ResponseID: row.ID, Finished: row.Finished, Data: data, Variables: variables, Language: quotaLanguage,
			Row: valueResponse(row, data, variables, ttc, meta),
		})

		events := []string{EventResponseCreated}
		if in.Finished {
			events = append(events, EventResponseFinished)
		}
		for _, event := range events {
			if err := tx.Enqueue(ctx, PipelineArgs{Event: event, WorkspaceID: in.WorkspaceID, SurveyID: sv.ID, Response: row, Locale: in.Locale}, nil); err != nil {
				return err
			}
		}
		result = &Result{ID: row.ID, QuotaFull: full.full()}
		return nil
	})
	if err != nil {
		var ae *apperr.Error
		if errors.As(err, &ae) {
			return nil, ae
		}
		return nil, fmt.Errorf("create response: %w", err)
	}
	return result, nil
}

// oneResponseEmail is the verified address a new response of a survey that takes one response per verified email is counted under, or "" when the survey takes any number. The address is the one the verified-email gate stamped from its token (verifiedEmailKey), never one the client sent: the gate runs before Create and replaces it.
func (sv *Survey) oneResponseEmail(data map[string]any) string {
	if !sv.IsVerifyEmailEnabled || !sv.IsSingleResponsePerEmailEnabled {
		return ""
	}
	email, _ := data[verifiedEmailKey].(string)
	return email
}

// claimVerifiedEmail refuses a new response for an address that already has one in the survey, finished or not (409, details.code CodeEmailAlreadyResponded). Counting unfinished ones too is what makes the limit hold: a response is created with its first answers, so two tabs started from one verification link would otherwise both pass and both finish. A respondent who reloads goes on in the response they started (the page keeps its id), which is an update, not a create. A transaction-scoped advisory lock on the survey and address serializes racing creates, so the check and the insert are one step. Addresses compare case-insensitively, as PostgreSQL's lower() reads them, the same rule the link page uses to show "already responded" (linksurvey.Repository.verifiedEmailResponded).
func claimVerifiedEmail(ctx context.Context, tx *jobs.Tx, surveyID uuid.UUID, email string) error {
	if err := tx.WithContext(ctx).Exec(`SELECT pg_advisory_xact_lock(hashtextextended('response-email:' || ? || ':' || lower(?), 0))`, surveyID.String(), email).Error; err != nil {
		return fmt.Errorf("lock verified email: %w", err)
	}
	var taken bool
	if err := tx.WithContext(ctx).Raw(`SELECT EXISTS (SELECT 1 FROM responses WHERE survey_id = ? AND lower(data->>'verifiedEmail') = lower(?))`, surveyID, email).Scan(&taken).Error; err != nil {
		return fmt.Errorf("find response by verified email: %w", err)
	}
	if taken {
		return emailAlreadyResponded()
	}
	return nil
}

func insertResponse(ctx context.Context, tx *jobs.Tx, surveyID uuid.UUID, owner *contactRef, displayID *uuid.UUID, in CreateInput, language *string,
	data, variables map[string]any, ttc map[string]float64, meta map[string]any, flags []embeddeddata.Flag,
) (Response, error) {
	row := Response{SurveyID: surveyID, DisplayID: displayID, ContactAttributes: json.RawMessage("null"), Finished: in.Finished, EndingID: in.EndingID, Tags: []tag.Tag{}, SingleUseID: in.SingleUseID, Language: language}
	var err error
	if row.Data, err = json.Marshal(data); err != nil {
		return Response{}, fmt.Errorf("encode response data: %w", err)
	}
	if row.Variables, err = json.Marshal(variables); err != nil {
		return Response{}, fmt.Errorf("encode response variables: %w", err)
	}
	if row.TTC, err = json.Marshal(ttc); err != nil {
		return Response{}, fmt.Errorf("encode response ttc: %w", err)
	}
	if row.Meta, err = json.Marshal(meta); err != nil {
		return Response{}, fmt.Errorf("encode response meta: %w", err)
	}
	flagsJSON, err := json.Marshal(flags)
	if err != nil {
		return Response{}, fmt.Errorf("encode ingest flags: %w", err)
	}
	var contactID *uuid.UUID
	var attributes any
	if owner != nil {
		contactID = &owner.ID
		row.Contact = owner.apiContact()
		if row.ContactAttributes, err = json.Marshal(owner.Attributes); err != nil {
			return Response{}, fmt.Errorf("encode contact attributes: %w", err)
		}
		attributes = string(row.ContactAttributes)
	}
	var out struct {
		ID        uuid.UUID
		CreatedAt time.Time
		UpdatedAt time.Time
	}
	err = tx.WithContext(ctx).Raw(`INSERT INTO responses (survey_id, contact_id, display_id, finished, ending_id, data, variables, ttc, meta, ingest_flags, contact_attributes, single_use_id, language)
		VALUES (?, ?, ?, ?, ?, ?::jsonb, ?::jsonb, ?::jsonb, ?::jsonb, ?::jsonb, ?::jsonb, ?, ?) RETURNING id, created_at, updated_at`,
		surveyID, contactID, displayID, in.Finished, in.EndingID, string(row.Data), string(row.Variables), string(row.TTC), string(row.Meta), string(flagsJSON), attributes, in.SingleUseID, language,
	).Scan(&out).Error
	if err != nil {
		return Response{}, err
	}
	row.ID, row.CreatedAt, row.UpdatedAt = out.ID, out.CreatedAt, out.UpdatedAt
	return row, nil
}

// valueResponse is the persisted response as the reserved fields read it.
func valueResponse(row Response, data, variables map[string]any, ttc map[string]float64, meta map[string]any) embeddeddata.ValueResponse {
	ttcValues := make(map[string]any, len(ttc))
	for k, v := range ttc {
		ttcValues[k] = v
	}
	return embeddeddata.ValueResponse{
		ID: row.ID.String(), SurveyID: row.SurveyID.String(), CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, Finished: row.Finished,
		Language: row.Language, Data: data, Variables: variables, TTC: ttcValues, Meta: meta,
	}
}
