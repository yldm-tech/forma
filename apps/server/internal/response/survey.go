package response

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/embeddeddata"
	"github.com/yldm-tech/forma/apps/server/internal/survey"
	"github.com/yldm-tech/forma/apps/server/internal/survey/surveydoc"
)

// SingleUse is a survey's single-use setting.
type SingleUse struct {
	Enabled     bool `json:"enabled"`
	IsEncrypted bool `json:"isEncrypted"`
}

// Survey is what collecting a response reads of the survey it belongs to. Blocks and Recaptcha are the stored JSON, for the checks of other modules (answer validation, file uploads, reCAPTCHA) that the client API runs between this package's steps.
type Survey struct {
	ID                          uuid.UUID
	WorkspaceID                 uuid.UUID
	Type                        string
	Status                      string
	Pin                         *string
	SingleUse                   SingleUse
	Recaptcha                   json.RawMessage
	IsVerifyEmailEnabled        bool
	IsCaptureIPEnabled          bool
	IsAnonymizeResponsesEnabled bool
	Blocks                      json.RawMessage
	HiddenFields                surveydoc.HiddenFields
	// DefaultLanguage is the code of the survey's default language, "" when it has none.
	DefaultLanguage string
	// Fields are the survey's Embedded Data fields, in their stored order.
	Fields []embeddeddata.Linked
	// IsSingleResponsePerEmailEnabled takes one response per verified address; it applies only with IsVerifyEmailEnabled, which stamps the address (oneResponseEmail).
	IsSingleResponsePerEmailEnabled bool

	blocks []surveydoc.Block
	// variables are the survey's variables, read for its total score (ScoreBounds).
	variables []surveydoc.Variable
}

// Elements are the survey's elements in document order, forma2's getElementsFromBlocks.
func (sv *Survey) Elements() []surveydoc.Element {
	var out []surveydoc.Element
	for _, b := range sv.blocks {
		out = append(out, b.Elements...)
	}
	return out
}

// ElementIDs are the ids of the survey's elements, the keys of response.data that hold answers.
func (sv *Survey) ElementIDs() []string {
	elements := sv.Elements()
	ids := make([]string, len(elements))
	for i, e := range elements {
		ids[i] = e.ID
	}
	return ids
}

// FileUploadElementIDs are the ids of the survey's file upload elements, forma2's getSurveyFileUploadElementIds.
func (sv *Survey) FileUploadElementIDs() map[string]bool {
	out := map[string]bool{}
	for _, e := range sv.Elements() {
		if e.Type == surveydoc.ElementFileUpload {
			out[e.ID] = true
		}
	}
	return out
}

// LoadSurvey reads a survey with its default language and Embedded Data fields; nil when it does not exist.
func LoadSurvey(ctx context.Context, db *gorm.DB, id uuid.UUID) (*Survey, error) {
	var rows []struct {
		ID                          uuid.UUID
		WorkspaceID                 uuid.UUID
		Type                        string
		Status                      string
		Pin                         *string
		SingleUse                   []byte
		Recaptcha                   []byte
		IsVerifyEmailEnabled        bool
		IsCaptureIPEnabled          bool
		IsAnonymizeResponsesEnabled bool
		Blocks                      []byte
		HiddenFields                []byte
		Variables                   []byte
		DefaultLanguage             *string
		// IsSingleResponsePerEmailEnabled is Survey's.
		IsSingleResponsePerEmailEnabled bool
	}
	err := db.WithContext(ctx).Raw(`SELECT s.id, s.workspace_id, s.type, s.status, s.pin, s.single_use, s.recaptcha,
		s.is_verify_email_enabled, s.is_capture_ip_enabled, s.is_anonymize_responses_enabled, s.blocks, s.hidden_fields, s.variables, s.is_single_response_per_email_enabled,
		(SELECT l.code FROM survey_languages sl JOIN languages l ON l.id = sl.language_id WHERE sl.survey_id = s.id AND sl.is_default) AS default_language
		FROM surveys s WHERE s.id = ?`, id).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("load survey: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	r := rows[0]
	sv := &Survey{
		ID: r.ID, WorkspaceID: r.WorkspaceID, Type: r.Type, Status: r.Status, Pin: r.Pin, Recaptcha: r.Recaptcha,
		IsVerifyEmailEnabled: r.IsVerifyEmailEnabled, IsCaptureIPEnabled: r.IsCaptureIPEnabled, IsAnonymizeResponsesEnabled: r.IsAnonymizeResponsesEnabled,
		IsSingleResponsePerEmailEnabled: r.IsSingleResponsePerEmailEnabled,
		Blocks:                          r.Blocks,
	}
	if r.DefaultLanguage != nil {
		sv.DefaultLanguage = *r.DefaultLanguage
	}
	if len(r.SingleUse) > 0 {
		if err := json.Unmarshal(r.SingleUse, &sv.SingleUse); err != nil {
			return nil, fmt.Errorf("decode survey %s single use: %w", id, err)
		}
	}
	if err := json.Unmarshal(r.Blocks, &sv.blocks); err != nil {
		return nil, fmt.Errorf("decode survey %s blocks: %w", id, err)
	}
	if err := json.Unmarshal(r.HiddenFields, &sv.HiddenFields); err != nil {
		return nil, fmt.Errorf("decode survey %s hidden fields: %w", id, err)
	}
	if err := json.Unmarshal(r.Variables, &sv.variables); err != nil {
		return nil, fmt.Errorf("decode survey %s variables: %w", id, err)
	}
	if sv.Fields, err = embeddeddata.LoadLinked(ctx, db, id); err != nil {
		return nil, err
	}
	return sv, nil
}

// Survey loads the survey a response is submitted to, by the id the client sent: forma2's getSurvey, 404 "Survey not found" naming that id when there is none.
func (s *Service) Survey(ctx context.Context, rawID string) (*Survey, error) {
	id, err := ResolveID(rawID)
	if err != nil {
		return nil, err
	}
	sv, err := LoadSurvey(ctx, s.db, id)
	if err != nil {
		return nil, err
	}
	if sv == nil {
		return nil, NotFound("Survey", &rawID)
	}
	return sv, nil
}

// CheckWorkspace is the client routes' "Survey is part of another workspace" (400).
func (sv *Survey) CheckWorkspace(workspaceID uuid.UUID) error {
	if sv.WorkspaceID != workspaceID {
		return badRequest("Survey is part of another workspace", details("workspaceId", workspaceID.String()))
	}
	return nil
}

// CheckAccepting is "Survey is not accepting submissions" (403): only a survey in progress takes responses, new or updated.
func (sv *Survey) CheckAccepting() error {
	if sv.Status != survey.StatusInProgress {
		return forbidden("Survey is not accepting submissions", details("surveyId", sv.ID.String()))
	}
	return nil
}
