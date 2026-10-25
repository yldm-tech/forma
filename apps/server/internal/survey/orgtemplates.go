package survey

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/audit"
)

// An organization's own survey templates (the survey_templates table): a survey's document saved by an owner or manager and offered in the template gallery of every workspace of the organization. Saving copies the document, so the template does not follow later edits of the survey; using one creates a draft from the copy, the way "Use this template" does from the catalog.

// maxOrganizationTemplates bounds how many templates an organization keeps, so the gallery stays a list a person can scan.
const maxOrganizationTemplates = 100

// Lengths of a template's name and description, in characters, as the table's CHECKs bound them.
const (
	maxTemplateNameLength        = 200
	maxTemplateDescriptionLength = 1000
)

// createdFromOrganizationTemplate is the created_from of a survey made from an organization template, for analytics.
const createdFromOrganizationTemplate = "organization-template"

// TemplateLanguage is one of the languages a template is translated into: the workspace language of the survey it was saved from, by code, since a workspace language belongs to one workspace.
type TemplateLanguage struct {
	Code    string  `json:"code"`
	Alias   *string `json:"alias"`
	Default bool    `json:"default"`
	Enabled bool    `json:"enabled"`
}

// OrganizationTemplatePreset is the survey document a template creates: the parts of the survey it was saved from that respondents see, with every translation.
type OrganizationTemplatePreset struct {
	WelcomeCard         json.RawMessage `json:"welcomeCard"`
	Blocks              json.RawMessage `json:"blocks"`
	Endings             json.RawMessage `json:"endings"`
	HiddenFields        json.RawMessage `json:"hiddenFields"`
	Variables           json.RawMessage `json:"variables"`
	Metadata            json.RawMessage `json:"metadata"`
	Styling             json.RawMessage `json:"styling"`
	SurveyClosedMessage json.RawMessage `json:"surveyClosedMessage"`
}

// OrganizationTemplate is a survey an organization saved as a template. The list leaves out the preset; reading one template includes it.
type OrganizationTemplate struct {
	ID             uuid.UUID                   `json:"id"`
	CreatedAt      time.Time                   `json:"createdAt"`
	UpdatedAt      time.Time                   `json:"updatedAt"`
	OrganizationID uuid.UUID                   `json:"organizationId"`
	Name           string                      `json:"name"`
	Description    string                      `json:"description"`
	Creator        *Creator                    `json:"creator"`
	QuestionCount  int                         `json:"questionCount"`
	Languages      []TemplateLanguage          `json:"languages"`
	Preset         *OrganizationTemplatePreset `json:"preset,omitempty"`
}

// OrganizationTemplates is the gallery's section of an organization's templates, newest first, and whether the caller may rename and delete them.
type OrganizationTemplates struct {
	Templates []*OrganizationTemplate `json:"templates"`
	CanManage bool                    `json:"canManage"`
}

// TemplateDetails are the name and description a template is saved or renamed with.
type TemplateDetails struct {
	Name        string
	Description string
}

// normalized trims the details and checks them against the table's bounds, as the field errors a binding failure reports.
func (d TemplateDetails) normalized() (TemplateDetails, error) {
	d.Name, d.Description = strings.TrimSpace(d.Name), strings.TrimSpace(d.Description)
	var fields []apperr.FieldError
	switch n := len([]rune(d.Name)); {
	case n == 0:
		fields = append(fields, apperr.FieldError{Field: "name", Rule: "required"})
	case n > maxTemplateNameLength:
		fields = append(fields, apperr.FieldError{Field: "name", Rule: "max", Param: fmt.Sprint(maxTemplateNameLength)})
	}
	if len([]rune(d.Description)) > maxTemplateDescriptionLength {
		fields = append(fields, apperr.FieldError{Field: "description", Rule: "max", Param: fmt.Sprint(maxTemplateDescriptionLength)})
	}
	if len(fields) > 0 {
		return d, apperr.Validation(fields)
	}
	return d, nil
}

// OrganizationTemplates lists an organization's templates for anyone who can read the organization; canManage says whether the caller may also rename and delete them.
func (s *Service) OrganizationTemplates(ctx context.Context, caller Caller, organizationID uuid.UUID) (*OrganizationTemplates, error) {
	if err := s.authz.Require(ctx, caller.Actor, authz.OrganizationRead, authz.Organization(organizationID)); err != nil {
		return nil, err
	}
	canManage, err := s.authz.Can(ctx, caller.Actor, authz.OrganizationManage, authz.Organization(organizationID))
	if err != nil {
		return nil, err
	}
	templates, err := s.repo.organizationTemplates(ctx, organizationID)
	if err != nil {
		return nil, err
	}
	return &OrganizationTemplates{Templates: templates, CanManage: canManage}, nil
}

// OrganizationTemplate is one template with its preset, for the gallery's preview, behind organization.read.
func (s *Service) OrganizationTemplate(ctx context.Context, caller Caller, id uuid.UUID) (*OrganizationTemplate, error) {
	return s.authorizedTemplate(ctx, caller, id, authz.OrganizationRead)
}

// authorizedTemplate loads a template and checks the caller may take the organization action on its organization.
func (s *Service) authorizedTemplate(ctx context.Context, caller Caller, id uuid.UUID, action authz.Action) (*OrganizationTemplate, error) {
	t, err := s.repo.organizationTemplate(ctx, id)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, apperr.NotFound("Survey template", "")
	}
	if err := s.authz.Require(ctx, caller.Actor, action, authz.Organization(t.OrganizationID)); err != nil {
		return nil, err
	}
	return t, nil
}

// SaveAsTemplate saves a survey's document as a template of its organization: the caller must be able to read the survey and manage the organization (an owner or a manager). The template copies the survey as it is stored, so unsaved edits in an open editor are not part of it.
func (s *Service) SaveAsTemplate(ctx context.Context, caller Caller, surveyID uuid.UUID, details TemplateDetails) (*OrganizationTemplate, error) {
	details, err := details.normalized()
	if err != nil {
		return nil, err
	}
	sv, err := s.authorized(ctx, caller, surveyID, authz.SurveyRead)
	if err != nil {
		return nil, err
	}
	org, err := s.repo.organizationOf(ctx, sv.WorkspaceID)
	if err != nil {
		return nil, err
	}
	if org == nil {
		return nil, apperr.NotFound("Survey", "")
	}
	if err := s.authz.Require(ctx, caller.Actor, authz.OrganizationManage, authz.Organization(*org)); err != nil {
		return nil, err
	}
	if err := s.stateMutation(ctx, sv.WorkspaceID); err != nil {
		return nil, err
	}
	languages := make([]TemplateLanguage, len(sv.Languages))
	for i, l := range sv.Languages {
		languages[i] = TemplateLanguage{Code: l.Language.Code, Alias: l.Language.Alias, Default: l.Default, Enabled: l.Enabled}
	}
	t := &OrganizationTemplate{
		ID: uuid.Must(uuid.NewV7()), OrganizationID: *org, Name: details.Name, Description: details.Description, Languages: languages,
		Preset: &OrganizationTemplatePreset{
			WelcomeCard: sv.WelcomeCard, Blocks: stripIsDraft(sv.Blocks), Endings: sv.Endings, HiddenFields: sv.HiddenFields, Variables: sv.Variables,
			Metadata: sv.Metadata, Styling: sv.Styling, SurveyClosedMessage: sv.SurveyClosedMessage,
		},
	}
	created, err := s.repo.insertOrganizationTemplate(ctx, t, caller.userID())
	s.record(ctx, caller, audit.ActionCreated, audit.TargetSurveyTemplate, t.ID.String(), org, err, nil, created)
	return created, err
}

// UpdateOrganizationTemplate renames a template or changes its description, behind organization.manage.
func (s *Service) UpdateOrganizationTemplate(ctx context.Context, caller Caller, id uuid.UUID, details TemplateDetails) (*OrganizationTemplate, error) {
	details, err := details.normalized()
	if err != nil {
		return nil, err
	}
	old, err := s.authorizedTemplate(ctx, caller, id, authz.OrganizationManage)
	if err != nil {
		return nil, err
	}
	updated, err := s.repo.updateOrganizationTemplate(ctx, id, details)
	if err == nil && updated == nil {
		err = apperr.NotFound("Survey template", "")
	}
	s.record(ctx, caller, audit.ActionUpdated, audit.TargetSurveyTemplate, id.String(), &old.OrganizationID, err, old.summary(), updated)
	return updated, err
}

// DeleteOrganizationTemplate deletes a template, behind organization.manage. Surveys created from it are not affected.
func (s *Service) DeleteOrganizationTemplate(ctx context.Context, caller Caller, id uuid.UUID) error {
	old, err := s.authorizedTemplate(ctx, caller, id, authz.OrganizationManage)
	if err != nil {
		return err
	}
	deleted, err := s.repo.deleteOrganizationTemplate(ctx, id)
	if err == nil && !deleted {
		err = apperr.NotFound("Survey template", "")
	}
	s.record(ctx, caller, audit.ActionDeleted, audit.TargetSurveyTemplate, id.String(), &old.OrganizationID, err, old.summary(), nil)
	return err
}

// CreateFromOrganizationTemplate creates a draft in a workspace from one of its organization's templates, behind workspace.write: the template's document, languages and styling under the template's name, of the type the gallery creates, and nothing that belongs to a survey's run. A template of another organization is not found.
func (s *Service) CreateFromOrganizationTemplate(ctx context.Context, caller Caller, id, workspaceID uuid.UUID, surveyType string) (*Survey, error) {
	if err := s.authz.Require(ctx, caller.Actor, authz.WorkspaceWrite, authz.Workspace(workspaceID)); err != nil {
		return nil, err
	}
	org, err := s.repo.organizationOf(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	if org == nil {
		return nil, apperr.NotFound("Workspace", workspaceID.String())
	}
	t, err := s.repo.organizationTemplate(ctx, id)
	if err != nil {
		return nil, err
	}
	if t == nil || t.OrganizationID != *org {
		return nil, apperr.NotFound("Survey template", "")
	}
	if err := s.stateMutation(ctx, workspaceID); err != nil {
		return nil, err
	}
	sv, err := s.createFromOrganizationTemplate(ctx, caller, t, workspaceID, surveyType, *org)
	if err != nil {
		s.record(ctx, caller, audit.ActionCreated, audit.TargetSurvey, "", org, err, nil, nil)
		return nil, err
	}
	s.record(ctx, caller, audit.ActionCreated, audit.TargetSurvey, sv.ID.String(), org, nil, nil, sv)
	s.captureCreated(ctx, caller, sv, createdFromOrganizationTemplate)
	return sv, nil
}

func (s *Service) createFromOrganizationTemplate(ctx context.Context, caller Caller, t *OrganizationTemplate, workspaceID uuid.UUID, surveyType string, organizationID uuid.UUID) (*Survey, error) {
	links, err := s.repo.templateLanguageLinks(ctx, workspaceID, t.Languages)
	if err != nil {
		return nil, err
	}
	p := t.Preset
	rec := defaultRecord()
	rec.Name, rec.Type, rec.Status, rec.CreatedBy = t.Name, surveyType, StatusDraft, caller.userID()
	rec.WelcomeCard, rec.Blocks, rec.Endings, rec.HiddenFields, rec.Variables = p.WelcomeCard, p.Blocks, p.Endings, p.HiddenFields, p.Variables
	rec.Metadata, rec.Styling, rec.SurveyClosedMessage = p.Metadata, p.Styling, p.SurveyClosedMessage
	return s.insertSurvey(ctx, newSurvey{WorkspaceID: workspaceID, Record: rec, Languages: links, PrivateSegmentFilters: json.RawMessage(`[]`)}, organizationID)
}

// summary is the template without its preset, as the audit log records it.
func (t *OrganizationTemplate) summary() *OrganizationTemplate {
	out := *t
	out.Preset = nil
	return &out
}

// ---- repository ----

const organizationTemplateColumns = `t.id, t.created_at, t.updated_at, t.organization_id, t.name, t.description, u.name AS creator_name, t.languages,
	(SELECT count(*) FROM jsonb_array_elements(t.blocks) b, jsonb_array_elements(CASE jsonb_typeof(b->'elements') WHEN 'array' THEN b->'elements' ELSE '[]' END) e) AS question_count`

const organizationTemplatePresetColumns = `, t.welcome_card, t.blocks, t.endings, t.hidden_fields, t.variables, t.metadata, t.styling, t.survey_closed_message`

type organizationTemplateRow struct {
	ID                  uuid.UUID
	CreatedAt           time.Time
	UpdatedAt           time.Time
	OrganizationID      uuid.UUID
	Name                string
	Description         string
	CreatorName         *string
	Languages           []byte
	QuestionCount       int
	WelcomeCard         []byte
	Blocks              []byte
	Endings             []byte
	HiddenFields        []byte
	Variables           []byte
	Metadata            []byte
	Styling             []byte
	SurveyClosedMessage []byte
}

func (row organizationTemplateRow) template(withPreset bool) (*OrganizationTemplate, error) {
	t := &OrganizationTemplate{
		ID: row.ID, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, OrganizationID: row.OrganizationID, Name: row.Name, Description: row.Description,
		QuestionCount: row.QuestionCount, Languages: []TemplateLanguage{},
	}
	if row.CreatorName != nil {
		t.Creator = &Creator{Name: *row.CreatorName}
	}
	if err := json.Unmarshal(row.Languages, &t.Languages); err != nil {
		return nil, fmt.Errorf("decode survey template languages: %w", err)
	}
	if withPreset {
		t.Preset = &OrganizationTemplatePreset{
			WelcomeCard: rawOrNull(row.WelcomeCard), Blocks: rawOrNull(row.Blocks), Endings: rawOrNull(row.Endings), HiddenFields: rawOrNull(row.HiddenFields),
			Variables: rawOrNull(row.Variables), Metadata: rawOrNull(row.Metadata), Styling: rawOrNull(row.Styling), SurveyClosedMessage: rawOrNull(row.SurveyClosedMessage),
		}
	}
	return t, nil
}

// organizationTemplates lists an organization's templates without their presets, newest first.
func (r *Repository) organizationTemplates(ctx context.Context, organizationID uuid.UUID) ([]*OrganizationTemplate, error) {
	var rows []organizationTemplateRow
	if err := r.db.WithContext(ctx).Raw(`SELECT `+organizationTemplateColumns+` FROM survey_templates t LEFT JOIN users u ON u.id = t.created_by
		WHERE t.organization_id = ? ORDER BY t.created_at DESC, t.id DESC`, organizationID).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("list survey templates: %w", err)
	}
	out := make([]*OrganizationTemplate, len(rows))
	for i, row := range rows {
		t, err := row.template(false)
		if err != nil {
			return nil, err
		}
		out[i] = t
	}
	return out, nil
}

// organizationTemplate loads one template with its preset, or nil when there is none.
func (r *Repository) organizationTemplate(ctx context.Context, id uuid.UUID) (*OrganizationTemplate, error) {
	var rows []organizationTemplateRow
	if err := r.db.WithContext(ctx).Raw(`SELECT `+organizationTemplateColumns+organizationTemplatePresetColumns+` FROM survey_templates t LEFT JOIN users u ON u.id = t.created_by
		WHERE t.id = ?`, id).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load survey template: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0].template(true)
}

// insertOrganizationTemplate stores a template, refusing one more than an organization may keep. The organization's row is locked first, so two saves cannot both pass the count.
func (r *Repository) insertOrganizationTemplate(ctx context.Context, t *OrganizationTemplate, createdBy *uuid.UUID) (*OrganizationTemplate, error) {
	languages, err := json.Marshal(t.Languages)
	if err != nil {
		return nil, fmt.Errorf("encode survey template languages: %w", err)
	}
	p := t.Preset
	err = r.transaction(ctx, func(tx *Repository) error {
		if _, err := tx.exec(ctx, `SELECT 1 FROM organizations WHERE id = ? FOR NO KEY UPDATE`, t.OrganizationID); err != nil {
			return fmt.Errorf("lock organization: %w", err)
		}
		var n int
		if err := tx.db.WithContext(ctx).Raw(`SELECT count(*) FROM survey_templates WHERE organization_id = ?`, t.OrganizationID).Row().Scan(&n); err != nil {
			return fmt.Errorf("count survey templates: %w", err)
		}
		if n >= maxOrganizationTemplates {
			return apperr.Conflict(fmt.Sprintf("An organization can keep at most %d survey templates. Delete one to save another.", maxOrganizationTemplates))
		}
		_, err := tx.exec(ctx, `INSERT INTO survey_templates (id, organization_id, created_by, name, description, welcome_card, blocks, endings, hidden_fields, variables, metadata, styling, survey_closed_message, languages)
			VALUES (?, ?, ?, ?, ?, ?::jsonb, ?::jsonb, ?::jsonb, ?::jsonb, ?::jsonb, ?::jsonb, ?::jsonb, ?::jsonb, ?::jsonb)`,
			t.ID, t.OrganizationID, createdBy, t.Name, t.Description, jsonParam(p.WelcomeCard), jsonParam(p.Blocks), jsonParam(p.Endings), jsonParam(p.HiddenFields),
			jsonParam(p.Variables), jsonParam(p.Metadata), jsonParam(p.Styling), jsonParam(p.SurveyClosedMessage), string(languages))
		if err != nil {
			return fmt.Errorf("create survey template: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	created, err := r.organizationTemplate(ctx, t.ID)
	if err != nil {
		return nil, err
	}
	return created.summary(), nil
}

// updateOrganizationTemplate sets a template's name and description, or returns nil when it is gone.
func (r *Repository) updateOrganizationTemplate(ctx context.Context, id uuid.UUID, d TemplateDetails) (*OrganizationTemplate, error) {
	n, err := r.exec(ctx, `UPDATE survey_templates SET name = ?, description = ?, updated_at = now() WHERE id = ?`, d.Name, d.Description, id)
	if err != nil {
		return nil, fmt.Errorf("update survey template: %w", err)
	}
	if n == 0 {
		return nil, nil
	}
	t, err := r.organizationTemplate(ctx, id)
	if err != nil || t == nil {
		return nil, err
	}
	return t.summary(), nil
}

func (r *Repository) deleteOrganizationTemplate(ctx context.Context, id uuid.UUID) (bool, error) {
	n, err := r.exec(ctx, `DELETE FROM survey_templates WHERE id = ?`, id)
	if err != nil {
		return false, fmt.Errorf("delete survey template: %w", err)
	}
	return n > 0, nil
}

// templateLanguageLinks resolves a template's languages in a workspace by code, creating the ones it lacks with the template's alias, as copying a survey into another workspace does.
func (r *Repository) templateLanguageLinks(ctx context.Context, workspaceID uuid.UUID, languages []TemplateLanguage) ([]languageLink, error) {
	links := make([]languageLink, 0, len(languages))
	for _, l := range languages {
		var id uuid.UUID
		err := r.db.WithContext(ctx).Raw(`INSERT INTO languages (workspace_id, code, alias) VALUES (?, ?, ?)
			ON CONFLICT (workspace_id, code) DO UPDATE SET code = EXCLUDED.code RETURNING id`, workspaceID, l.Code, l.Alias).Row().Scan(&id)
		if err != nil {
			return nil, fmt.Errorf("resolve survey template language: %w", err)
		}
		links = append(links, languageLink{LanguageID: id, Default: l.Default, Enabled: l.Enabled})
	}
	return links, nil
}
