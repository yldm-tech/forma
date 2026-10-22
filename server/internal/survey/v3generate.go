package survey

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/authz"
	"github.com/yldm-tech/forma/server/internal/langcode"
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

// errAINotConfigured is forma2's problemAIUnavailable for ai_instance_not_configured.
var errAINotConfigured = &v3api.Problem{Status: http.StatusServiceUnavailable, Title: "AI Unavailable", Detail: "AI is not configured for this Forma instance.", Code: "ai_instance_not_configured"}

// GenerateV3 is forma2's POST /api/v3/surveys/generate behind workspace.write, up to the AI provider call: a prompt without enough detail is answered with feedback, and every other request with the answer forma2 gave on an instance without an AI provider, which is what this server is. AI survey generation is not ported: it needs an AI provider client, which is not among this server's dependencies.
func (s *Service) GenerateV3(ctx context.Context, caller Caller, in *GenerateInput) error {
	if err := s.requireWorkspaceV3(ctx, caller, in.WorkspaceID, authz.WorkspaceWrite); err != nil {
		return err
	}
	if utf8.RuneCountInString(in.Prompt) < generatePromptDetailMinLength || len(strings.Fields(in.Prompt)) < generatePromptDetailMinWords {
		return v3api.BadRequest("Prompt needs more detail", v3api.InvalidParam{Name: "prompt", Reason: "Describe the survey goal, audience, or topic in a sentence so the AI can create a useful draft."})
	}
	return errAINotConfigured
}
