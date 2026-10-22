package survey

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
)

// isoTime is how v3 writes an instant: JavaScript's toISOString, UTC with milliseconds.
func isoTime(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }

func isoTimePtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := isoTime(*t)
	return &s
}

// v3ListItem is forma2's TV3SurveyListItem.
type v3ListItem struct {
	ID                     uuid.UUID `json:"id"`
	Name                   string    `json:"name"`
	WorkspaceID            uuid.UUID `json:"workspaceId"`
	Type                   string    `json:"type"`
	Status                 string    `json:"status"`
	PublishOn              *string   `json:"publishOn"`
	ArchivedAt             *string   `json:"archivedAt"`
	CreatedAt              string    `json:"createdAt"`
	UpdatedAt              string    `json:"updatedAt"`
	ResponseCount          int       `json:"responseCount"`
	CompletedResponseCount int       `json:"completedResponseCount"`
	Creator                *Creator  `json:"creator"`
}

func v3ListItems(items []ListItem) []v3ListItem {
	out := make([]v3ListItem, len(items))
	for i, it := range items {
		out[i] = v3ListItem{
			ID: it.ID, Name: it.Name, WorkspaceID: it.WorkspaceID, Type: it.Type, Status: it.Status, PublishOn: isoTimePtr(it.PublishOn),
			ArchivedAt: isoTimePtr(it.ArchivedAt), CreatedAt: isoTime(it.CreatedAt), UpdatedAt: isoTime(it.UpdatedAt),
			ResponseCount: it.ResponseCount, CompletedResponseCount: it.CompletedResponseCount, Creator: it.Creator,
		}
	}
	return out
}

// v3Resource is forma2's serializeV3SurveyResource output.
type v3Resource struct {
	ID              uuid.UUID       `json:"id"`
	WorkspaceID     uuid.UUID       `json:"workspaceId"`
	CreatedAt       string          `json:"createdAt"`
	UpdatedAt       string          `json:"updatedAt"`
	Name            string          `json:"name"`
	Type            string          `json:"type"`
	Status          string          `json:"status"`
	ArchivedAt      *string         `json:"archivedAt"`
	Metadata        any             `json:"metadata"`
	DefaultLanguage string          `json:"defaultLanguage"`
	Languages       []v3Language    `json:"languages"`
	WelcomeCard     any             `json:"welcomeCard"`
	Blocks          any             `json:"blocks"`
	Endings         any             `json:"endings"`
	HiddenFields    json.RawMessage `json:"hiddenFields"`
	Variables       json.RawMessage `json:"variables"`
	Distribution    *v3Distribution `json:"distribution,omitempty"`
	Targeting       *v3Targeting    `json:"targeting,omitempty"`
}

// i18nValue is forma2's getI18nValueForLanguage: the text under the code, or under a key whose canonical form is the code.
func i18nValue(o *object, code string) (string, bool) {
	if s, ok := o.values[code].(string); ok {
		return s, true
	}
	for _, k := range o.keys {
		if strings.EqualFold(canonicalKey(k), code) && canonicalKey(k) != "" {
			s, ok := o.values[k].(string)
			return s, ok
		}
	}
	return "", false
}

// publicValue is forma2's serializeCanonicalValue: every internal translatable map becomes a map keyed by the default language and the requested languages; with fallback a missing translation repeats the default text.
func publicValue(v any, defaultLanguage string, codes []string, fallback bool) any {
	switch x := v.(type) {
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = publicValue(e, defaultLanguage, codes, fallback)
		}
		return out
	case *object:
		if isInternalI18n(x) {
			out := newObject()
			defaultText := x.values["default"].(string)
			out.set(defaultLanguage, defaultText)
			includesDefault := false
			for _, code := range codes {
				if code == defaultLanguage {
					includesDefault = true
					continue
				}
				if text, ok := i18nValue(x, code); ok {
					out.set(code, text)
				} else if fallback {
					out.set(code, defaultText)
				}
			}
			if !includesDefault {
				out.remove(defaultLanguage)
			}
			return out
		}
		out := newObject()
		for _, k := range x.keys {
			out.set(k, publicValue(x.values[k], defaultLanguage, codes, fallback))
		}
		return out
	}
	return v
}

func decoded(raw json.RawMessage) any {
	v, err := decodeValue(raw)
	if err != nil {
		return nil
	}
	return v
}

// v3DistributionOf is forma2's surveyToV3Distribution: stored values that break the public constraints are clamped to null (or 0 for the delay).
func v3DistributionOf(s *Survey) *v3Distribution {
	nonNegative := func(v *int) *int {
		if v != nil && *v >= 0 {
			return v
		}
		return nil
	}
	d := &v3Distribution{DisplayOption: s.DisplayOption, DisplayLimit: nonNegative(s.DisplayLimit), RecontactDays: nonNegative(s.RecontactDays), AutoClose: nonNegative(s.AutoClose), Triggers: []v3Trigger{}}
	if p := s.DisplayPercentage; p != nil && *p >= 0.01 && *p <= 100 {
		d.DisplayPercentage = p
	}
	if a := s.AutoComplete; a != nil && *a >= 1 {
		d.AutoComplete = a
	}
	if s.Delay >= 0 {
		d.Delay = s.Delay
	}
	for _, t := range s.Triggers {
		d.Triggers = append(d.Triggers, v3Trigger{ActionClassID: t.ActionClass.ID.String()})
	}
	return d
}

// v3TargetingOf is forma2's surveyToV3Targeting: the segment's filters, empty for everyone.
func v3TargetingOf(s *Survey) *v3Targeting {
	if s.Segment == nil || isNullJSON(s.Segment.Filters) {
		return &v3Targeting{Filters: json.RawMessage(`[]`)}
	}
	return &v3Targeting{Filters: s.Segment.Filters}
}

// toV3Resource is forma2's serializeV3SurveyResource; lang narrows translations to the requested languages, each a selector resolved against the survey's languages.
func toV3Resource(s *Survey, lang []string) (*v3Resource, *languageError) {
	defaultLanguage := v3DefaultLanguage(s.Languages)
	languages := v3Languages(s.Languages)
	codes := make([]string, 0, len(languages))
	for _, l := range languages {
		codes = append(codes, l.Code)
	}
	fallback := false
	if len(lang) > 0 {
		requested := make([]string, 0, len(lang))
		seen := map[string]bool{}
		for _, selector := range lang {
			code, err := resolveLanguage(selector, languages)
			if err != nil {
				return nil, err
			}
			if !seen[code] {
				seen[code] = true
				requested = append(requested, code)
			}
		}
		codes, fallback = requested, true
	}
	r := &v3Resource{
		ID: s.ID, WorkspaceID: s.WorkspaceID, CreatedAt: isoTime(s.CreatedAt), UpdatedAt: isoTime(s.UpdatedAt), Name: s.Name, Type: s.Type,
		Status: s.Status, ArchivedAt: isoTimePtr(s.ArchivedAt), DefaultLanguage: defaultLanguage, Languages: languages,
		WelcomeCard: publicValue(decoded(s.WelcomeCard), defaultLanguage, codes, fallback),
		Blocks:      publicValue(decoded(s.Blocks), defaultLanguage, codes, fallback),
		Endings:     publicValue(decoded(s.Endings), defaultLanguage, codes, fallback),
		HiddenFields: s.HiddenFields, Variables: s.Variables,
	}
	meta := decoded(s.Metadata)
	if mo := asObject(meta); mo != nil {
		out := mo.clone()
		for _, key := range metadataI18nKeys {
			if v, ok := mo.get(key); ok {
				out.set(key, publicValue(v, defaultLanguage, codes, fallback))
			}
		}
		meta = out
	}
	r.Metadata = meta
	if s.Type == TypeApp {
		r.Distribution, r.Targeting = v3DistributionOf(s), v3TargetingOf(s)
	}
	return r, nil
}
