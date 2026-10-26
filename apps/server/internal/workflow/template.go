package workflow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/jsvalue"
	"github.com/yldm-tech/forma/apps/server/internal/response"
	"github.com/yldm-tech/forma/apps/server/internal/responsemail"
)

// placeholder is a template variable, {{path}} with optional spaces inside the braces. Paths are the names Template.lookup knows: answers.<element or hidden field id>, variables.<id>, contact.attributes.<key>, and the fixed response.*, survey.*, contact.*, workflow.* and run.* values.
var placeholder = regexp.MustCompile(`\{\{\s*([A-Za-z0-9_.:\-]+)\s*\}\}`)

// wholePlaceholder is a JSON string that is nothing but one placeholder, which RenderJSON replaces by the value itself rather than its text.
var wholePlaceholder = regexp.MustCompile(`^\{\{\s*([A-Za-z0-9_.:\-]+)\s*\}\}$`)

// Template is what a run's templates (an HTTP body, a chat message, an attribute value) render against: the response, its survey and contact, and the workflow and run. A test send has no response: KeepUnknown then leaves every placeholder it cannot fill as written, so the receiver sees where each value goes.
type Template struct {
	Fixed map[string]any
	// Answers is the response's data and variables; nil for a test send.
	Answers *responsemail.Answers
	// Attributes is the contact's attributes as the response recorded them.
	Attributes  map[string]any
	KeepUnknown bool
}

// lookup is the value a placeholder names; answer is set for answers.*, whose text is the answer's as emails show it.
func (t *Template) lookup(name string) (value any, answer string, ok bool) {
	if v, found := t.Fixed[name]; found {
		return v, "", true
	}
	switch {
	case name == "response.data" && t.Answers != nil:
		return t.Answers.Data, "", true
	case strings.HasPrefix(name, "answers.") && t.Answers != nil:
		key := strings.TrimPrefix(name, "answers.")
		v, found := t.Answers.Data[key]
		return v, key, found
	case strings.HasPrefix(name, "variables.") && t.Answers != nil:
		v, found := t.Answers.Variables[strings.TrimPrefix(name, "variables.")]
		return v, "", found
	case strings.HasPrefix(name, "contact.attributes."):
		v, found := t.Attributes[strings.TrimPrefix(name, "contact.attributes.")]
		return v, "", found
	}
	return nil, "", false
}

// text is a placeholder's value as text, and whether it resolved.
func (t *Template) text(name string) (string, bool) {
	v, answer, ok := t.lookup(name)
	if !ok {
		return "", false
	}
	if answer != "" {
		return t.Answers.Text(answer), true
	}
	return textOf(v), true
}

// textOf writes a value as text: a string as is, a number as JavaScript prints it, a list joined by "; ", an object as JSON, nothing for null.
func textOf(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case float64:
		return jsvalue.FormatNumber(x)
	case bool:
		if x {
			return "true"
		}
		return "false"
	case []any:
		parts := make([]string, 0, len(x))
		for _, item := range x {
			if s := textOf(item); s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, "; ")
	}
	raw, err := encodeJSON(v)
	if err != nil {
		return ""
	}
	return string(raw)
}

// RenderText fills every placeholder of a text template with its value's text, passed through escape; an unknown placeholder renders empty, or as written when KeepUnknown is set.
func (t *Template) RenderText(tmpl string, escape func(string) string) string {
	return placeholder.ReplaceAllStringFunc(tmpl, func(match string) string {
		name := placeholder.FindStringSubmatch(match)[1]
		s, ok := t.text(name)
		if !ok {
			if t.KeepUnknown {
				return match
			}
			return ""
		}
		return escape(s)
	})
}

// RenderJSON renders a JSON body template. The template must be JSON; placeholders live inside its strings, so whatever the values hold, the result is JSON too. A string that is a single placeholder becomes the value itself (a number stays a number, response.data an object); any other string gets the values' text.
func (t *Template) RenderJSON(tmpl string) ([]byte, error) {
	dec := json.NewDecoder(strings.NewReader(tmpl))
	dec.UseNumber()
	var doc any
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("the body is not JSON: %w", err)
	}
	if dec.More() {
		return nil, fmt.Errorf("the body is not one JSON value")
	}
	return encodeJSON(t.renderValue(doc))
}

func (t *Template) renderValue(v any) any {
	switch x := v.(type) {
	case string:
		if m := wholePlaceholder.FindStringSubmatch(x); m != nil {
			if value, _, ok := t.lookup(m[1]); ok {
				return value
			}
			if t.KeepUnknown {
				return x
			}
			return nil
		}
		return t.RenderText(x, func(s string) string { return s })
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = t.renderValue(item)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, item := range x {
			out[k] = t.renderValue(item)
		}
		return out
	}
	return v
}

// encodeJSON is json.Marshal without HTML escaping, as JSON.stringify writes.
func encodeJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// responseTemplate is the template a run renders against: the response's own values, its survey, and its contact as the response recorded it.
func responseTemplate(r *response.Response, sv *responsemail.Survey, a *responsemail.Answers) *Template {
	fixed := map[string]any{
		"response.id":        r.ID.String(),
		"response.createdAt": jsvalue.ISOString(r.CreatedAt),
		"response.updatedAt": jsvalue.ISOString(r.UpdatedAt),
		"response.finished":  r.Finished,
		"response.language":  derefOrNil(r.Language),
		"response.endingId":  derefOrNil(r.EndingID),
		"survey.id":          r.SurveyID.String(),
		"survey.name":        sv.Name,
		"contact.id":         nil,
		"contact.userId":     nil,
	}
	if r.Contact != nil {
		fixed["contact.id"] = r.Contact.ID.String()
		fixed["contact.userId"] = derefOrNil(r.Contact.UserID)
	}
	attributes := map[string]any{}
	// A snapshot that is not an object leaves the contact's attributes empty rather than failing the run.
	_ = json.Unmarshal(jsonObject(r.ContactAttributes), &attributes)
	return &Template{Fixed: fixed, Answers: a, Attributes: attributes}
}

// testTemplate is a test send's template: the workflow and its survey's name, every other placeholder left as written.
func testTemplate(workflowID uuid.UUID, surveyID, surveyName string) *Template {
	fixed := map[string]any{"workflow.id": workflowID.String()}
	if surveyName != "" {
		fixed["survey.id"], fixed["survey.name"] = surveyID, surveyName
	}
	return &Template{Fixed: fixed, Attributes: map[string]any{}, KeepUnknown: true}
}

func derefOrNil(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}
