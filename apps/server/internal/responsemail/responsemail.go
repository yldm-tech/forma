// Package responsemail renders a response into email content, the parts forma2 shared between the response-finished notification, survey follow-ups and the workflow send_email step: each element's label and answer (apps/web/lib/responses.ts getElementResponseMapping), recall expansion (apps/web/lib/utils/recall.ts parseRecallInfo), the follow-up body's send-time sanitization and its response blocks (apps/web/modules/email/lib/survey-response-email.ts), and the recipient a `to` value resolves to.
package responsemail

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/embeddeddata"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/jsvalue"
	"github.com/yldm-tech/forma/apps/server/internal/platform/mailer"
	"github.com/yldm-tech/forma/apps/server/internal/response"
	"github.com/yldm-tech/forma/apps/server/internal/survey/surveydoc"
)

// Language is one of a survey's languages: its code, and whether it is the default the survey's texts key as "default".
type Language struct {
	Code    string
	Default bool
}

// Survey is what rendering a response reads of its survey.
type Survey struct {
	*response.Survey
	Name      string
	Languages []Language
}

// LoadSurvey reads a survey with its name and languages; nil when it does not exist.
func LoadSurvey(ctx context.Context, db *gorm.DB, id uuid.UUID) (*Survey, error) {
	sv, err := response.LoadSurvey(ctx, db, id)
	if err != nil || sv == nil {
		return nil, err
	}
	out := &Survey{Survey: sv}
	var names []string
	if err := db.WithContext(ctx).Raw(`SELECT name FROM surveys WHERE id = ?`, id).Scan(&names).Error; err != nil {
		return nil, fmt.Errorf("load survey name: %w", err)
	}
	if len(names) > 0 {
		out.Name = names[0]
	}
	var languages []struct {
		Code      string
		IsDefault bool
	}
	if err := db.WithContext(ctx).Raw(`SELECT l.code, sl.is_default FROM survey_languages sl JOIN languages l ON l.id = sl.language_id
		WHERE sl.survey_id = ? ORDER BY sl.is_default DESC, l.code`, id).Scan(&languages).Error; err != nil {
		return nil, fmt.Errorf("load survey languages: %w", err)
	}
	for _, l := range languages {
		out.Languages = append(out.Languages, Language{Code: l.Code, Default: l.IsDefault})
	}
	return out, nil
}

// LanguageCode is forma2's getLanguageCode: the key the survey's texts use for the response's language, "default" for the default language, a language the survey does not have, or none. Codes compare case-insensitively.
func (sv *Survey) LanguageCode(language *string) string {
	if len(sv.Languages) == 0 || language == nil || *language == "" {
		return "default"
	}
	for _, l := range sv.Languages {
		if strings.EqualFold(l.Code, *language) {
			if l.Default {
				return "default"
			}
			return l.Code
		}
	}
	return "default"
}

// Answers is a response's stored JSON decoded for rendering.
type Answers struct {
	Response  *response.Response
	Data      map[string]any
	Variables map[string]any
	// raw keeps each answer's JSON, so an object answer lists its members in their stored order.
	raw map[string]json.RawMessage
}

// Decode reads the response's data and variables.
func Decode(r *response.Response) (*Answers, error) {
	a := &Answers{Response: r, Data: map[string]any{}, Variables: map[string]any{}, raw: map[string]json.RawMessage{}}
	if err := unmarshalObject(r.Data, &a.Data); err != nil {
		return nil, fmt.Errorf("decode response data: %w", err)
	}
	if err := unmarshalObject(r.Data, &a.raw); err != nil {
		return nil, fmt.Errorf("decode response data: %w", err)
	}
	if err := unmarshalObject(r.Variables, &a.Variables); err != nil {
		return nil, fmt.Errorf("decode response variables: %w", err)
	}
	return a, nil
}

func unmarshalObject[T any](raw json.RawMessage, dst *map[string]T) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return json.Unmarshal(raw, dst)
}

// ServerValues is forma2's buildServerEmbeddedValues: the readable reserved fields of the response under its answers.
func (a *Answers) ServerValues(sv *Survey) (map[string]any, error) {
	var ttc, meta map[string]any
	if err := unmarshalObject(a.Response.TTC, &ttc); err != nil {
		return nil, fmt.Errorf("decode response ttc: %w", err)
	}
	if err := unmarshalObject(a.Response.Meta, &meta); err != nil {
		return nil, fmt.Errorf("decode response meta: %w", err)
	}
	r := embeddeddata.ValueResponse{
		ID: a.Response.ID.String(), SurveyID: a.Response.SurveyID.String(), CreatedAt: a.Response.CreatedAt, UpdatedAt: a.Response.UpdatedAt,
		Finished: a.Response.Finished, Language: a.Response.Language, Data: a.Data, Variables: a.Variables, TTC: ttc, Meta: meta,
	}
	return embeddeddata.ServerValues(r, sv.Fields, sv.ElementIDs()), nil
}

// Elements is forma2's getElementResponseMapping: every element of the survey in order, labelled with its headline in the response's language (recall expanded against the answers, reserved values and variables, then reduced to text), with the answer converted for the email.
func (a *Answers) Elements(sv *Survey) ([]mailer.ResponseElement, error) {
	values, err := a.ServerValues(sv)
	if err != nil {
		return nil, err
	}
	lang := sv.LanguageCode(a.Response.Language)
	elements := sv.Elements()
	out := make([]mailer.ResponseElement, len(elements))
	for i, el := range elements {
		label := surveydoc.TextContent(ParseRecall(el.Headline[lang], values, a.Variables, mailer.DefaultLocale, false))
		out[i] = a.convert(el, label)
	}
	return out, nil
}

// convert is forma2's convertResponseValue: rankings and file uploads as lists (a single string as a list of one), picture selections as the image URLs of the chosen pictures, anything else as processResponseData's text.
func (a *Answers) convert(el surveydoc.Element, label string) mailer.ResponseElement {
	e := mailer.ResponseElement{Label: label, Type: el.Type}
	answer, present := a.Data[el.ID]
	switch el.Type {
	case surveydoc.ElementRanking, surveydoc.ElementFileUpload:
		switch x := answer.(type) {
		case string:
			e.Items = []string{x}
		case []any:
			e.Items = make([]string, len(x))
			for i, item := range x {
				if s, ok := item.(string); ok {
					e.Items[i] = s
				} else if !jsvalue.IsNullish(item) {
					e.Items[i] = jsvalue.String(item)
				}
			}
		default:
			// Anything else is not a list forma2 could render; a missing answer is not rendered.
			if present && answer != nil {
				e.Items = []string{}
			}
		}
	case surveydoc.ElementPictureSelection:
		e.Items = []string{}
		imageURL := func(id any) (string, bool) {
			for _, c := range el.Choices {
				if s, ok := id.(string); ok && c.ID == s && c.ImageURL != "" {
					return c.ImageURL, true
				}
			}
			return "", false
		}
		switch x := answer.(type) {
		case string:
			if u, ok := imageURL(x); ok {
				e.Items = append(e.Items, u)
			}
		case []any:
			for _, id := range x {
				if u, ok := imageURL(id); ok {
					e.Items = append(e.Items, u)
				}
			}
		}
	default:
		e.Text = processResponseData(answer, a.raw[el.ID])
	}
	return e
}

// Text is forma2's processResponseData of the answer stored under key, "" when there is none.
func (a *Answers) Text(key string) string {
	return processResponseData(a.Data[key], a.raw[key])
}

// processResponseData is forma2's processResponseData:a string as it is, a number as JavaScript prints it, a list joined by "; " without its empty entries, an object as "key: value" lines in its stored order without empty values, anything else as "".
func processResponseData(v any, raw json.RawMessage) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return jsvalue.FormatNumber(x)
	case []any:
		parts := []string{}
		for _, e := range x {
			if e == nil || e == "" {
				continue
			}
			parts = append(parts, jsvalue.String(e))
		}
		return strings.Join(parts, "; ")
	case map[string]any:
		members, err := jsvalue.DecodeObject(raw)
		if err != nil {
			return ""
		}
		lines := []string{}
		for _, m := range members {
			if m.Value == "" {
				continue
			}
			lines = append(lines, m.Key+": "+jsvalue.String(m.Value))
		}
		return strings.Join(lines, "\n")
	}
	return ""
}

// ComputedValues is the email's variables block: each computed field of the survey whose response slot holds a string or a number, with its name and whether it is a number field.
func (a *Answers) ComputedValues(sv *Survey) []mailer.EmbeddedValue {
	var out []mailer.EmbeddedValue
	for _, f := range embeddeddata.OfSource(sv.Fields, embeddeddata.SourceComputed) {
		var value string
		switch x := a.Variables[f.StorageKey].(type) {
		case string:
			value = x
		case float64:
			value = jsvalue.FormatNumber(x)
		default:
			continue
		}
		out = append(out, mailer.EmbeddedValue{Name: f.Name, Number: f.DataType == embeddeddata.TypeNumber, Value: value})
	}
	return out
}

// HiddenValues is the email's hidden fields block: each ingested field of the survey whose answer is a non-empty string.
func (a *Answers) HiddenValues(sv *Survey) []mailer.HiddenValue {
	var out []mailer.HiddenValue
	for _, f := range embeddeddata.OfSource(sv.Fields, embeddeddata.SourceIngested) {
		if s, ok := a.Data[f.StorageKey].(string); ok && s != "" {
			out = append(out, mailer.HiddenValue{ID: f.StorageKey, Value: s})
		}
	}
	return out
}

// PublicBase is the origin relative storage URLs in emails resolve against, forma2's getPublicDomain: PUBLIC_URL, else WEBAPP_URL.
func PublicBase(cfg config.Config) string {
	if cfg.PublicURL != nil {
		return strings.TrimRight(cfg.PublicURL.String(), "/")
	}
	if cfg.WebAppURL != nil {
		return strings.TrimRight(cfg.WebAppURL.String(), "/")
	}
	return ""
}

// ResolveStorageURL is forma2's resolveStorageUrl as the email senders called it: a relative /storage URL made absolute against the public domain (the app's when there is none), anything else unchanged.
func ResolveStorageURL(publicBase, raw string) string {
	if strings.HasPrefix(raw, "/storage/") && publicBase != "" {
		return strings.TrimRight(publicBase, "/") + raw
	}
	return raw
}

// WithAbsoluteFiles makes the URLs of picture selection and file upload answers absolute, as forma2 did before rendering them into an email.
func WithAbsoluteFiles(elements []mailer.ResponseElement, publicBase string) []mailer.ResponseElement {
	for i, e := range elements {
		if e.Type != surveydoc.ElementPictureSelection && e.Type != surveydoc.ElementFileUpload {
			continue
		}
		for j, u := range e.Items {
			elements[i].Items[j] = ResolveStorageURL(publicBase, u)
		}
	}
	return elements
}
