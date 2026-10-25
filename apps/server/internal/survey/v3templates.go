package survey

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/platform/v3api"
)

// TemplateCreateInput is forma2's ZV3TrustedTemplateCreateBody.
type TemplateCreateInput struct {
	WorkspaceID     uuid.UUID
	TemplateID      string
	Source          string
	SurveyType      string
	DefaultLanguage string
}

// ParseTemplateCreate reads the body of POST /api/v3/surveys/templates; unknown keys are ignored, as zod's non-strict object stripped them.
func ParseTemplateCreate(body json.RawMessage) (*TemplateCreateInput, error) {
	var raw struct {
		WorkspaceID     *string `json:"workspaceId"`
		TemplateID      *string `json:"templateId"`
		Source          *string `json:"source"`
		SurveyType      *string `json:"surveyType"`
		DefaultLanguage *string `json:"defaultLanguage"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, v3api.BadRequest("Invalid request body", v3api.InvalidParam{Name: "body", Reason: "Invalid input: expected object"})
	}
	in := &TemplateCreateInput{}
	var params []v3api.InvalidParam
	missing := func(name string) {
		params = append(params, v3api.InvalidParam{Name: name, Reason: "Invalid input: expected string, received undefined"})
	}
	if raw.WorkspaceID == nil {
		missing("workspaceId")
	} else if id, err := uuid.Parse(*raw.WorkspaceID); err != nil {
		params = append(params, v3api.InvalidParam{Name: "workspaceId", Reason: "Invalid UUID"})
	} else {
		in.WorkspaceID = id
	}
	if raw.TemplateID == nil {
		missing("templateId")
	} else if in.TemplateID = strings.TrimSpace(*raw.TemplateID); in.TemplateID == "" {
		params = append(params, v3api.InvalidParam{Name: "templateId", Reason: "Too small: expected string to have >=1 characters"})
	}
	for _, f := range []struct {
		name    string
		value   *string
		options []string
		dst     *string
	}{
		{"source", raw.Source, []string{"catalog", "custom", "xm"}, &in.Source},
		{"surveyType", raw.SurveyType, []string{TypeLink, TypeApp}, &in.SurveyType},
	} {
		switch {
		case f.value == nil:
			missing(f.name)
		case !slices.Contains(f.options, *f.value):
			params = append(params, v3api.InvalidParam{Name: f.name, Reason: enumReason(f.options)})
		default:
			*f.dst = *f.value
		}
	}
	switch {
	case raw.DefaultLanguage == nil:
		missing("defaultLanguage")
	case !IsSurveyDefaultLanguage(*raw.DefaultLanguage):
		params = append(params, v3api.InvalidParam{Name: "defaultLanguage", Reason: "Unsupported survey default language"})
	default:
		in.DefaultLanguage = *raw.DefaultLanguage
	}
	if len(params) > 0 {
		return nil, v3api.BadRequest("Invalid request body", params...)
	}
	return in, nil
}

// resolveTemplate is forma2's resolveTrustedTemplate: the custom template only as the custom source, the experience-management templates only as the xm source, rendered in the default language's dashboard locale with the workspace's name filled in.
func (s *Service) resolveTemplate(ctx context.Context, in *TemplateCreateInput) (*Template, *Preset, error) {
	name, err := s.repo.workspaceName(ctx, in.WorkspaceID)
	if err != nil || name == "" {
		return nil, nil, err
	}
	if (in.Source == "custom") != (in.TemplateID == CustomTemplateID) {
		return nil, nil, nil
	}
	if in.Source == "xm" && !slices.Contains(XMTemplateIDs, in.TemplateID) {
		return nil, nil, nil
	}
	t, err := s.catalog.Template(in.TemplateID, TemplateLocale(in.DefaultLanguage))
	if err != nil || t == nil {
		return nil, nil, err
	}
	var preset Preset
	if err := json.Unmarshal(t.Preset, &preset); err != nil {
		return nil, nil, fmt.Errorf("decode template preset: %w", err)
	}
	if err := withWorkspaceName(&preset, name); err != nil {
		return nil, nil, err
	}
	return t, &preset, nil
}

// toPublic is forma2's toV3PublicValue: an internal translatable map becomes a map with only the default language.
func toPublic(v any, defaultLanguage string) any {
	switch x := v.(type) {
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = toPublic(e, defaultLanguage)
		}
		return out
	case *object:
		if isInternalI18n(x) {
			out := newObject()
			out.set(defaultLanguage, x.values["default"])
			return out
		}
		out := newObject()
		for _, k := range x.keys {
			out.set(k, toPublic(x.values[k], defaultLanguage))
		}
		return out
	}
	return v
}

// CreateFromTemplateV3 is forma2's createTrustedTemplateSurveyResponse behind workspace.write, for a signed-in user: the template becomes a v3 create document and goes through the same create.
func (s *Service) CreateFromTemplateV3(ctx context.Context, caller Caller, in *TemplateCreateInput) (*v3Resource, error) {
	if err := s.requireWorkspaceV3(ctx, caller, in.WorkspaceID, authz.WorkspaceWrite); err != nil {
		return nil, err
	}
	_, preset, err := s.resolveTemplate(ctx, in)
	if err != nil {
		return nil, err
	}
	if preset == nil {
		return nil, v3api.BadRequest("Template not found", v3api.InvalidParam{Name: "templateId", Reason: "Unknown template for the requested source"})
	}
	part := func(raw json.RawMessage, fallback string) any {
		if isNullJSON(raw) {
			raw = json.RawMessage(fallback)
		}
		return toPublic(decoded(raw), in.DefaultLanguage)
	}
	doc := newObject()
	doc.set("workspaceId", in.WorkspaceID.String())
	doc.set("name", preset.Name)
	doc.set("type", in.SurveyType)
	doc.set("status", StatusDraft)
	doc.set("defaultLanguage", in.DefaultLanguage)
	doc.set("languages", []any{})
	doc.set("metadata", part(preset.Metadata, `{}`))
	doc.set("welcomeCard", part(preset.WelcomeCard, `{"enabled": false}`))
	doc.set("blocks", part(preset.Blocks, `[]`))
	doc.set("endings", part(preset.Endings, `[]`))
	doc.set("hiddenFields", decoded(preset.HiddenFields))
	doc.set("variables", part(preset.Variables, `[]`))
	d, issues := s.parseCreateBody(doc)
	if len(issues) > 0 {
		return nil, v3api.BadRequest("Invalid template survey document", invalidParams(issues, "data")...)
	}
	createdFrom := "template"
	switch in.Source {
	case "custom":
		createdFrom = "blank"
	case "xm":
		createdFrom = "xm-template"
	}
	return s.createFromDocument(ctx, caller, in.WorkspaceID, d, createdFrom)
}

// Templates is the template catalog for the editor's template picker, behind workspace.read: the custom template, then forma2's templates, with the workspace's name filled in. Given a survey default language, it renders in that language's template locale exactly as CreateFromTemplateV3 does, so a preview matches the survey created from it; without one, in the user's dashboard language.
func (s *Service) Templates(ctx context.Context, caller Caller, workspaceID uuid.UUID, surveyLanguage string) ([]*Template, error) {
	if err := s.authz.Require(ctx, caller.Actor, authz.WorkspaceRead, authz.Workspace(workspaceID)); err != nil {
		return nil, err
	}
	name, err := s.repo.workspaceName(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	locale := defaultLocale
	if surveyLanguage != "" {
		locale = surveyLanguage
	} else if user := caller.userID(); user != nil {
		if locale, err = s.repo.userLocale(ctx, *user); err != nil {
			return nil, err
		}
	}
	templates, err := s.catalog.Templates(TemplateLocale(locale))
	if err != nil {
		return nil, err
	}
	for _, t := range templates {
		var preset Preset
		if err := json.Unmarshal(t.Preset, &preset); err != nil {
			return nil, fmt.Errorf("decode template preset: %w", err)
		}
		if err := withWorkspaceName(&preset, name); err != nil {
			return nil, err
		}
		if t.Preset, err = json.Marshal(preset); err != nil {
			return nil, fmt.Errorf("encode template preset: %w", err)
		}
	}
	return templates, nil
}
