package survey

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/ai"
	"github.com/yldm-tech/forma/server/internal/authz"
	"github.com/yldm-tech/forma/server/internal/langcode"
	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/platform/v3api"
)

// forma2's V3_SURVEY_GENERATE_* prompt bounds: the body's limits, then the detail a prompt needs before any AI provider is called.
const (
	generatePromptMinLength       = 4
	generatePromptMaxLength       = 1200
	generatePromptDetailMinLength = 24
	generatePromptDetailMinWords  = 4
)

// GenerateInput is forma2's ZV3SurveyGenerateBody.
type GenerateInput struct {
	WorkspaceID uuid.UUID
	Prompt      string
	Type        string
	Languages   []string
}

// ParseGenerate reads the body of POST /api/v3/surveys/generate: a strict object, so an unknown key is refused.
func ParseGenerate(value any) (*GenerateInput, error) {
	o := asObject(value)
	if o == nil {
		return nil, v3api.BadRequest("Invalid request body", v3api.InvalidParam{Name: "body", Reason: "Invalid input: expected object, received " + jsType(value)})
	}
	in := &GenerateInput{Type: TypeLink}
	var params []v3api.InvalidParam
	invalid := func(name, reason string) {
		params = append(params, v3api.InvalidParam{Name: name, Reason: reason})
	}
	str := func(name string) (string, bool) {
		v, ok := o.get(name)
		if !ok {
			invalid(name, "Invalid input: expected string, received undefined")
			return "", false
		}
		s, isString := v.(string)
		if !isString {
			invalid(name, "Invalid input: expected string, received "+jsType(v))
		}
		return s, isString
	}

	if raw, ok := str("workspaceId"); ok {
		id, err := uuid.Parse(raw)
		if err != nil {
			invalid("workspaceId", "Invalid UUID")
		}
		in.WorkspaceID = id
	}
	if raw, ok := str("prompt"); ok {
		in.Prompt = strings.TrimSpace(raw)
		switch n := utf8.RuneCountInString(in.Prompt); {
		case n < generatePromptMinLength:
			invalid("prompt", fmt.Sprintf("Prompt must be at least %d characters", generatePromptMinLength))
		case n > generatePromptMaxLength:
			invalid("prompt", fmt.Sprintf("Prompt must be %d characters or less", generatePromptMaxLength))
		}
	}
	if _, present := o.get("type"); present {
		if raw, ok := str("type"); ok {
			if raw != TypeLink && raw != TypeApp {
				invalid("type", enumReason([]string{TypeLink, TypeApp}))
			}
			in.Type = raw
		}
	}
	if v, present := o.get("languages"); present {
		in.Languages, params = generateLanguages(v, params)
	}
	var unknown []string
	allowed := keySet("workspaceId", "prompt", "type", "languages")
	for _, k := range o.keys {
		if !allowed[k] {
			unknown = append(unknown, `"`+k+`"`)
		}
	}
	if len(unknown) == 1 {
		invalid("body", "Unrecognized key: "+unknown[0])
	} else if len(unknown) > 1 {
		invalid("body", "Unrecognized keys: "+strings.Join(unknown, ", "))
	}
	if len(params) > 0 {
		return nil, v3api.BadRequest("Invalid request body", params...)
	}
	return in, nil
}

// generateLanguages is forma2's languages of ZV3SurveyGenerateBody: one to every dashboard locale, each normalized to a dashboard locale, none repeated.
func generateLanguages(v any, params []v3api.InvalidParam) ([]string, []v3api.InvalidParam) {
	items, ok := v.([]any)
	if !ok {
		return nil, append(params, v3api.InvalidParam{Name: "languages", Reason: "Invalid input: expected array, received " + jsType(v)})
	}
	if len(items) < 1 {
		return nil, append(params, v3api.InvalidParam{Name: "languages", Reason: "Too small: expected array to have >=1 items"})
	}
	if len(items) > len(userLocales) {
		return nil, append(params, v3api.InvalidParam{Name: "languages", Reason: fmt.Sprintf("Too big: expected array to have <=%d items", len(userLocales))})
	}
	out := make([]string, 0, len(items))
	valid := true
	for i, item := range items {
		name := fmt.Sprintf("languages.%d", i)
		s, isString := item.(string)
		if !isString {
			params = append(params, v3api.InvalidParam{Name: name, Reason: "Invalid input: expected string, received " + jsType(item)})
			valid = false
			continue
		}
		code := strings.TrimSpace(s)
		if code == "" {
			params = append(params, v3api.InvalidParam{Name: name, Reason: "Language code is required"})
			valid = false
			continue
		}
		locale := generateLocale(code)
		if locale == "" {
			params = append(params, v3api.InvalidParam{Name: name, Reason: fmt.Sprintf("Language '%s' is not supported for AI survey creation", code)})
			valid = false
			continue
		}
		out = append(out, locale)
	}
	if valid {
		for i, code := range out {
			if slices.Contains(out[:i], code) {
				params = append(params, v3api.InvalidParam{Name: "languages", Reason: "languages must not repeat a locale"})
				break
			}
		}
	}
	return out, params
}

// generateLocale is forma2's normalizeV3SurveyGenerateLocale: the dashboard locale a code names, or "".
func generateLocale(code string) string {
	normalized := langcode.Normalize(code)
	if normalized == "" {
		return ""
	}
	for _, locale := range userLocales {
		if strings.EqualFold(locale, normalized) {
			return locale
		}
	}
	return ""
}

// GenerateValidation is forma2's TV3SurveyGenerateValidation.
type GenerateValidation struct {
	Valid         bool                 `json:"valid"`
	InvalidParams []v3api.InvalidParam `json:"invalid_params"`
	Languages     []languageRequest    `json:"languages"`
}

// GenerateResult is forma2's TV3SurveyGenerateResult: a create payload the caller sends to POST /api/v3/surveys as it is.
type GenerateResult struct {
	Language   string             `json:"language"`
	Payload    any                `json:"payload"`
	Validation GenerateValidation `json:"validation"`
}

// generatedPayloadError is forma2's V3SurveyGeneratedPayloadValidationError.
type generatedPayloadError struct {
	params []v3api.InvalidParam
}

func (e *generatedPayloadError) Error() string {
	if len(e.params) == 0 {
		return "Generated survey payload is invalid"
	}
	parts := make([]string, len(e.params))
	for i, p := range e.params {
		parts[i] = p.Name + ": " + p.Reason
	}
	return "Generated survey payload is invalid: " + strings.Join(parts, "; ")
}

// promptDetailProblem is forma2's assertV3SurveyGeneratePrompt: a prompt too thin to generate from is answered with feedback before any AI provider is called.
func promptDetailProblem(prompt string) error {
	if utf8.RuneCountInString(prompt) < generatePromptDetailMinLength || len(strings.Fields(prompt)) < generatePromptDetailMinWords {
		return v3api.BadRequest("Prompt needs more detail", v3api.InvalidParam{Name: "prompt", Reason: "Describe the survey goal, audience, or topic in a sentence so the AI can create a useful draft."})
	}
	return nil
}

// generationOrganization runs the guards both generation routes share: workspace.write, the workspace's organization, then the prompt's detail.
func (s *Service) generationOrganization(ctx context.Context, caller Caller, in *GenerateInput) (uuid.UUID, error) {
	if err := s.requireWorkspaceV3(ctx, caller, in.WorkspaceID, authz.WorkspaceWrite); err != nil {
		return uuid.Nil, err
	}
	org, err := s.repo.organizationOf(ctx, in.WorkspaceID)
	if err != nil {
		return uuid.Nil, err
	}
	if org == nil {
		return uuid.Nil, v3api.Forbidden("")
	}
	return *org, nil
}

// GenerateV3 is forma2's POST /api/v3/surveys/generate behind workspace.write: a survey draft written by the instance's AI provider, returned as a create payload that already passed the create validation. Nothing is stored.
func (s *Service) GenerateV3(ctx context.Context, caller Caller, in *GenerateInput) (*GenerateResult, error) {
	organizationID, err := s.generationOrganization(ctx, caller, in)
	if err != nil {
		return nil, err
	}
	if err := promptDetailProblem(in.Prompt); err != nil {
		return nil, err
	}
	out, err := s.ai.GenerateObject(ctx, organizationID, ai.FeatureSurveyGeneration, generationRequest(in))
	if err == nil {
		var res *GenerateResult
		if res, err = s.finishGeneration(ctx, organizationID, in, out.Object, nil); err == nil {
			return res, nil
		}
	}
	return nil, s.generationProblem(ctx, err, in.WorkspaceID, organizationID)
}

// finishGeneration is forma2's finishV3SurveyGeneration, where both routes converge: the draft becomes a validated create payload, then each further requested language is translated into it one after another. A language that fails, or comes back with a gap, is dropped rather than failing a draft that already succeeded.
func (s *Service) finishGeneration(ctx context.Context, organizationID uuid.UUID, in *GenerateInput, raw []byte, onTranslation func(languageCode string, index, total int)) (*GenerateResult, error) {
	draft, params := parseGeneratedDraft(raw, false)
	if len(params) > 0 {
		return nil, &generatedPayloadError{params: params}
	}
	draft.Blocks = normalizeBlocks(draft.Blocks)
	payload, err := decodeValue(encodeValue(buildCreatePayload(in, draft)))
	if err != nil {
		return nil, fmt.Errorf("decode generated payload: %w", err)
	}
	d, issues := s.parseCreateBody(payload)
	if len(issues) > 0 {
		return nil, &generatedPayloadError{params: invalidParams(issues, "data")}
	}
	docParams, _, err := validateDocument(d)
	if err != nil {
		return nil, err
	}
	if len(docParams) > 0 {
		return nil, &generatedPayloadError{params: docParams}
	}
	res := &GenerateResult{Language: draft.Language, Payload: payload, Validation: GenerateValidation{Valid: true, InvalidParams: []v3api.InvalidParam{}, Languages: languageRequests(d)}}

	var extra []string
	for _, code := range in.Languages {
		if !strings.EqualFold(code, draft.Language) {
			extra = append(extra, code)
		}
	}
	if len(extra) == 0 {
		return res, nil
	}
	added, err := s.translatePayload(ctx, organizationID, in.WorkspaceID, asObject(payload), draft.Language, extra, onTranslation)
	if err != nil {
		return nil, err
	}
	langs := []any{}
	res.Validation.Languages = []languageRequest{}
	for i, code := range append([]string{draft.Language}, added...) {
		l := newObject()
		l.set("code", code)
		l.set("default", i == 0)
		l.set("enabled", true)
		langs = append(langs, l)
		res.Validation.Languages = append(res.Validation.Languages, languageRequest{Code: code, Default: i == 0, Enabled: true})
	}
	asObject(payload).set("languages", langs)
	return res, nil
}

// translatableTexts is forma2's collectTranslatableFields: every value the builder wrote in the source language, by the path the walk reached it on.
func translatableTexts(node any, source, path string, found map[string]*object, order *[]string) {
	switch v := node.(type) {
	case *object:
		if len(v.keys) == 1 && v.keys[0] == source {
			if _, ok := v.values[source].(string); ok {
				found[path] = v
				*order = append(*order, path)
				return
			}
		}
		for _, k := range v.keys {
			next := k
			if path != "" {
				next = path + "." + k
			}
			translatableTexts(v.values[k], source, next, found, order)
		}
	case []any:
		for i, item := range v {
			translatableTexts(item, source, fmt.Sprintf("%s.%d", path, i), found, order)
		}
	}
}

// translatePayload is forma2's translateV3SurveyPayloadLanguages: the payload is filled in place, and the languages that translated in full are returned in order.
func (s *Service) translatePayload(ctx context.Context, organizationID, workspaceID uuid.UUID, payload *object, source string, targets []string, onTranslation func(string, int, int)) ([]string, error) {
	found := map[string]*object{}
	var order []string
	translatableTexts(payload, source, "", found, &order)
	fields := make([]ai.TranslationField, len(order))
	for i, path := range order {
		text, _ := found[path].values[source].(string)
		// A generated draft has no rich text, so there is no markup to preserve.
		fields[i] = ai.TranslationField{Path: path, DefaultText: text}
	}
	var added []string
	for i, target := range targets {
		if onTranslation != nil {
			onTranslation(target, i+1, len(targets))
		}
		out, err := s.ai.TranslateFields(ctx, organizationID, fields, source, target)
		if err != nil {
			if ai.IsAbort(ctx, err) {
				return nil, err
			}
			s.log.ErrorContext(ctx, "Failed to translate a generated survey into a requested language", "targetLanguage", target, "sourceLanguage", source, "workspaceId", workspaceID.String(), "err", err)
			continue
		}
		if len(out.FailedPaths) > 0 {
			s.log.ErrorContext(ctx, "Dropped a generated survey language that was only partially translated", "targetLanguage", target, "sourceLanguage", source, "workspaceId", workspaceID.String(), "failedCount", len(out.FailedPaths))
			continue
		}
		for _, path := range order {
			if text, ok := out.Translations[path]; ok {
				found[path].set(target, text)
			}
		}
		added = append(added, target)
	}
	return added, nil
}

// generationProblem is forma2's mapV3SurveyGenerateError.
func (s *Service) generationProblem(ctx context.Context, err error, workspaceID, organizationID uuid.UUID) error {
	var p *v3api.Problem
	if errors.As(err, &p) {
		return p
	}
	var payloadErr *generatedPayloadError
	if errors.As(err, &payloadErr) {
		return &v3api.Problem{Status: http.StatusUnprocessableEntity, Title: "Unprocessable Content", Detail: payloadErr.Error(), Code: "ai_generated_payload_invalid", InvalidParams: payloadErr.params}
	}
	var tokenErr *ai.OutputTokenLimitError
	if errors.As(err, &tokenErr) {
		return &v3api.Problem{Status: http.StatusUnprocessableEntity, Title: "Unprocessable Content", Detail: "The generated survey exceeded the AI output token limit. Simplify the prompt or split it into smaller surveys.", Code: "ai_output_too_long"}
	}
	if e, ok := apperr.As(err); ok {
		switch {
		case e.Status == http.StatusTooManyRequests:
			return v3api.TooManyRequests(ai.QuotaExceededMessage, e.RetryAfter)
		case ai.UnavailableMessage(e.Code) != "":
			status := http.StatusForbidden
			if e.Code == ai.CodeInstanceNotConfigured {
				status = http.StatusServiceUnavailable
			}
			return &v3api.Problem{Status: status, Title: "AI Unavailable", Detail: ai.UnavailableMessage(e.Code), Code: e.Code}
		case e.Code == apperr.CodeNotFound:
			return &v3api.Problem{Status: http.StatusNotFound, Title: "Not Found", Detail: "Organization not found", Code: "not_found", Details: map[string]any{"resource_type": "Organization", "resource_id": organizationID.String()}}
		}
	}
	s.log.ErrorContext(ctx, "Failed to generate v3 survey create payload", "workspaceId", workspaceID.String(), "organizationId", organizationID.String(), "err", err)
	return &v3api.Problem{Status: http.StatusBadGateway, Title: "Bad Gateway", Detail: "The AI provider could not generate a valid survey draft. Try again or add more detail.", Code: "bad_gateway"}
}
