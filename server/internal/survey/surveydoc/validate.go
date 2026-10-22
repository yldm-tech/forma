package surveydoc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/yldm-tech/forma/server/internal/jsonschema"
)

// Document is a survey document as stored JSON, one value per column. A nil or JSON-null optional part is absent.
type Document struct {
	WelcomeCard  json.RawMessage
	Blocks       json.RawMessage
	Endings      json.RawMessage
	HiddenFields json.RawMessage
	Variables    json.RawMessage
	Metadata     json.RawMessage

	Styling             json.RawMessage
	SurveyClosedMessage json.RawMessage
	SingleUse           json.RawMessage
	Recaptcha           json.RawMessage
	WorkspaceOverwrites json.RawMessage
	InlineTriggers      json.RawMessage
}

// FollowUpInput is a follow-up as a save carries it, before its parts are known to have the right shape.
type FollowUpInput struct {
	ID      string          `json:"id"`
	Name    string          `json:"name"`
	Trigger json.RawMessage `json:"trigger"`
	Action  json.RawMessage `json:"action"`
	Deleted bool            `json:"deleted,omitempty"`
}

// Validator checks survey documents: the exported JSON Schemas for structure, then forma2's refinements.
type Validator struct {
	schemas *jsonschema.Validator
}

func NewValidator(schemas *jsonschema.Validator) *Validator {
	return &Validator{schemas: schemas}
}

func isAbsent(raw json.RawMessage) bool {
	return len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

// schemaIssues validates one part against its schema; the violations become issues under prefix.
func (v *Validator) schemaIssues(schema string, raw json.RawMessage, prefix ...any) ([]Issue, error) {
	violations, err := v.schemas.Violations(schema, raw)
	if err != nil {
		return nil, err
	}
	out := make([]Issue, len(violations))
	for i, viol := range violations {
		p := extend(prefix)
		for _, loc := range viol.Location {
			if n, err := strconv.Atoi(loc); err == nil {
				p = append(p, n)
			} else {
				p = append(p, loc)
			}
		}
		out[i] = Issue{Path: p, Message: viol.Message}
	}
	return out, nil
}

// Structure checks every present part against its exported JSON Schema. Required parts (welcome card, blocks, endings, hidden fields, variables, metadata) must be present.
func (v *Validator) Structure(d Document, followUps []FollowUpInput) ([]Issue, error) {
	var out []Issue
	parts := []struct {
		schema   string
		name     string
		raw      json.RawMessage
		required bool
	}{
		{"survey.welcome_card", "welcomeCard", d.WelcomeCard, true},
		{"survey.blocks", "blocks", d.Blocks, true},
		{"survey.endings", "endings", d.Endings, true},
		{"survey.hidden_fields", "hiddenFields", d.HiddenFields, true},
		{"survey.variables", "variables", d.Variables, true},
		{"survey.metadata", "metadata", d.Metadata, true},
		{"survey.styling", "styling", d.Styling, false},
		{"survey.closed_message", "surveyClosedMessage", d.SurveyClosedMessage, false},
		{"survey.single_use", "singleUse", d.SingleUse, false},
		{"survey.recaptcha", "recaptcha", d.Recaptcha, false},
		{"survey.workspace_overwrites", "workspaceOverwrites", d.WorkspaceOverwrites, false},
		{"survey.inline_triggers", "inlineTriggers", d.InlineTriggers, false},
	}
	for _, p := range parts {
		if isAbsent(p.raw) {
			if p.required {
				out = append(out, Issue{Path: path(p.name), Message: "Invalid input: expected a value, received null"})
			}
			continue
		}
		issues, err := v.schemaIssues(p.schema, p.raw, p.name)
		if err != nil {
			return nil, fmt.Errorf("validate %s: %w", p.name, err)
		}
		out = append(out, issues...)
	}
	for i, f := range followUps {
		for _, part := range []struct {
			schema, name string
			raw          json.RawMessage
		}{{"survey_follow_up.trigger", "trigger", f.Trigger}, {"survey_follow_up.action", "action", f.Action}} {
			if isAbsent(part.raw) {
				out = append(out, Issue{Path: path("followUps", i, part.name), Message: "Invalid input: expected object, received null"})
				continue
			}
			issues, err := v.schemaIssues(part.schema, part.raw, "followUps", i, part.name)
			if err != nil {
				return nil, fmt.Errorf("validate follow-up %s: %w", part.name, err)
			}
			out = append(out, issues...)
		}
	}
	return out, nil
}

// Decode reads the typed view of a document that passed Structure.
func Decode(d Document, languages []Language, followUps []FollowUpInput) (*Survey, error) {
	s := &Survey{Languages: languages}
	for _, p := range []struct {
		raw json.RawMessage
		dst any
	}{
		{d.WelcomeCard, &s.WelcomeCard}, {d.Blocks, &s.Blocks}, {d.Endings, &s.Endings},
		{d.HiddenFields, &s.HiddenFields}, {d.Variables, &s.Variables},
	} {
		if isAbsent(p.raw) {
			continue
		}
		if err := json.Unmarshal(p.raw, p.dst); err != nil {
			return nil, fmt.Errorf("decode survey document: %w", err)
		}
	}
	if !isAbsent(d.Metadata) {
		var meta struct {
			Title       I18nString `json:"title"`
			Description I18nString `json:"description"`
		}
		if err := json.Unmarshal(d.Metadata, &meta); err != nil {
			return nil, fmt.Errorf("decode survey metadata: %w", err)
		}
		s.MetadataTitle, s.MetadataDescription = meta.Title, meta.Description
	}
	for _, f := range followUps {
		fu := FollowUp{ID: f.ID, Name: f.Name, Deleted: f.Deleted}
		if err := json.Unmarshal(f.Trigger, &fu.Trigger); err != nil {
			return nil, fmt.Errorf("decode follow-up trigger: %w", err)
		}
		if err := json.Unmarshal(f.Action, &fu.Action); err != nil {
			return nil, fmt.Errorf("decode follow-up action: %w", err)
		}
		s.FollowUps = append(s.FollowUps, fu)
	}
	return s, nil
}

// Check is the full validation of a survey save, forma2's ZSurvey: structure first; when the structure holds, the field refinements and the cross-field refinement, all reported together.
func (v *Validator) Check(d Document, languages []Language, followUps []FollowUpInput) ([]Issue, *Survey, error) {
	structure, err := v.Structure(d, followUps)
	if err != nil || len(structure) > 0 {
		return structure, nil, err
	}
	s, err := Decode(d, languages, followUps)
	if err != nil {
		return nil, nil, err
	}
	out := FieldIssues(s)
	out = append(out, Refine(s)...)
	return out, s, nil
}

// SchemaIssues checks a document against one exported schema by name, reporting violations under prefix.
func (v *Validator) SchemaIssues(schema string, raw json.RawMessage, prefix ...any) ([]Issue, error) {
	return v.schemaIssues(schema, raw, prefix...)
}
