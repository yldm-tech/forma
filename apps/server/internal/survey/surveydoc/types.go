// Package surveydoc is the survey document: the typed view of the JSON a survey stores (welcome card, blocks with their elements and logic, endings, hidden fields, variables, follow-ups) and the rules forma2 enforced on it with zod refinements, which the JSON Schemas exported from packages/types cannot express.
//
// The document is stored as the JSON it arrived as; these types are what the rules read. A field the types do not name is kept by the stored JSON and ignored by the rules, as zod's object schemas ignored it. Where forma2 parsed a value with a union of object variants (elements, choices, endings, conditions, operands, actions), the types read only the keys of the variant the value matches: zod stripped every other key, so it was neither validated nor able to fail the parse.
package surveydoc

import (
	"bytes"
	"encoding/json"
	"slices"
)

// declaredOnly decodes a JSON object into dst after dropping every key that keys does not list, as zod's non-strict object schemas strip the keys a variant does not declare. A value that is not an object goes to dst as it is, for the decoder to accept (null) or report.
func declaredOnly(data []byte, dst any, keys ...string) error {
	fields, ok := objectFields(data)
	if !ok {
		return json.Unmarshal(data, dst)
	}
	return decodeFields(fields, dst, keys...)
}

// objectFields splits a JSON object into its members; ok is false for any other JSON value.
func objectFields(data []byte) (map[string]json.RawMessage, bool) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, false
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(trimmed, &fields) != nil {
		return nil, false
	}
	return fields, true
}

func decodeFields(fields map[string]json.RawMessage, dst any, keys ...string) error {
	for k := range fields {
		if !slices.Contains(keys, k) {
			delete(fields, k)
		}
	}
	kept, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	return json.Unmarshal(kept, dst)
}

// stringField is a member's value when it is a JSON string, "" otherwise.
func stringField(fields map[string]json.RawMessage, key string) string {
	var s string
	if json.Unmarshal(fields[key], &s) != nil {
		return ""
	}
	return s
}

// declaredItems drops, from every object of the array under key, the members keys does not list. A value that is not an array of objects is left for the decoder.
func declaredItems(fields map[string]json.RawMessage, key string, keys ...string) {
	raw, ok := fields[key]
	if !ok {
		return
	}
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil {
		return
	}
	for i, item := range items {
		members, ok := objectFields(item)
		if !ok {
			continue
		}
		for k := range members {
			if !slices.Contains(keys, k) {
				delete(members, k)
			}
		}
		b, err := json.Marshal(members)
		if err != nil {
			return
		}
		items[i] = b
	}
	if b, err := json.Marshal(items); err == nil {
		fields[key] = b
	}
}

// I18nString is forma2's TI18nString: text by language code, "default" being the survey's default language. A nil map is an absent field.
type I18nString map[string]string

// Default is the default-language text, "" when absent.
func (s I18nString) Default() string { return s["default"] }

// HasDefault reports whether the "default" key is present, the refinement ZI18nString applies.
func (s I18nString) HasDefault() bool {
	_, ok := s["default"]
	return ok
}

// Element types, forma2's TSurveyElementTypeEnum.
const (
	ElementFileUpload           = "fileUpload"
	ElementOpenText             = "openText"
	ElementMultipleChoiceSingle = "multipleChoiceSingle"
	ElementMultipleChoiceMulti  = "multipleChoiceMulti"
	ElementNPS                  = "nps"
	ElementCTA                  = "cta"
	ElementRating               = "rating"
	ElementConsent              = "consent"
	ElementPictureSelection     = "pictureSelection"
	ElementCal                  = "cal"
	ElementDate                 = "date"
	ElementMatrix               = "matrix"
	ElementAddress              = "address"
	ElementRanking              = "ranking"
	ElementContactInfo          = "contactInfo"
	ElementCSAT                 = "csat"
	ElementCES                  = "ces"
	ElementSlider               = "slider"
)

// Choice is a choice of a choice element, a matrix row or column, or a picture of a picture selection.
type Choice struct {
	ID       string     `json:"id"`
	Label    I18nString `json:"label,omitempty"`
	ImageURL string     `json:"imageUrl,omitempty"`
	// Score is the points the choice adds to the survey's total score (packages/types/surveys/scoring.ts), read on single choice, multiple choice and picture selection elements only; nil when the choice has none.
	Score *float64 `json:"score,omitempty"`
}

// CharLimit is an open-text element's length limit.
type CharLimit struct {
	Enabled *bool    `json:"enabled,omitempty"`
	Min     *float64 `json:"min,omitempty"`
	Max     *float64 `json:"max,omitempty"`
}

// ToggleInput is one sub-field of an address or contact-info element.
type ToggleInput struct {
	Show        bool       `json:"show"`
	Required    bool       `json:"required"`
	Placeholder I18nString `json:"placeholder"`
}

// Element is a question of a block. It is the union of every element type's fields; Type says which apply.
type Element struct {
	ID        string     `json:"id"`
	Type      string     `json:"type"`
	Headline  I18nString `json:"headline"`
	Subheader I18nString `json:"subheader,omitempty"`
	ImageURL  *string    `json:"imageUrl,omitempty"`
	VideoURL  *string    `json:"videoUrl,omitempty"`
	Required  bool       `json:"required"`
	IsDraft   *bool      `json:"isDraft,omitempty"`

	Scale *string  `json:"scale,omitempty"`
	Range *float64 `json:"range,omitempty"`

	Placeholder            I18nString `json:"placeholder,omitempty"`
	InputType              string     `json:"inputType,omitempty"`
	CharLimit              *CharLimit `json:"charLimit,omitempty"`
	Label                  I18nString `json:"label,omitempty"`
	Choices                []Choice   `json:"choices,omitempty"`
	OtherOptionPlaceholder I18nString `json:"otherOptionPlaceholder,omitempty"`
	LowerLabel             I18nString `json:"lowerLabel,omitempty"`
	UpperLabel             I18nString `json:"upperLabel,omitempty"`
	HTML                   I18nString `json:"html,omitempty"`
	IncludeTime            *bool      `json:"includeTime,omitempty"`

	Min  *float64 `json:"min,omitempty"`
	Max  *float64 `json:"max,omitempty"`
	Step *float64 `json:"step,omitempty"`

	ButtonExternal *bool      `json:"buttonExternal,omitempty"`
	ButtonURL      *string    `json:"buttonUrl,omitempty"`
	CTAButtonLabel I18nString `json:"ctaButtonLabel,omitempty"`

	AllowedFileExtensions *[]string `json:"allowedFileExtensions,omitempty"`
	CalUserName           *string   `json:"calUserName,omitempty"`
	CalHost               *string   `json:"calHost,omitempty"`

	Rows    []Choice `json:"rows,omitempty"`
	Columns []Choice `json:"columns,omitempty"`

	AddressLine1 *ToggleInput `json:"addressLine1,omitempty"`
	AddressLine2 *ToggleInput `json:"addressLine2,omitempty"`
	City         *ToggleInput `json:"city,omitempty"`
	State        *ToggleInput `json:"state,omitempty"`
	Zip          *ToggleInput `json:"zip,omitempty"`
	Country      *ToggleInput `json:"country,omitempty"`
	FirstName    *ToggleInput `json:"firstName,omitempty"`
	LastName     *ToggleInput `json:"lastName,omitempty"`
	Email        *ToggleInput `json:"email,omitempty"`
	Phone        *ToggleInput `json:"phone,omitempty"`
	Company      *ToggleInput `json:"company,omitempty"`
}

// elementBaseKeys are ZSurveyElementBase's keys that Element reads; elementKeys are, by element type, the keys Element reads that only that type's schema declares.
var (
	elementBaseKeys = []string{"id", "type", "headline", "subheader", "imageUrl", "videoUrl", "required", "isDraft", "scale", "range"}
	labelKeys       = []string{"lowerLabel", "upperLabel"}
	elementKeys     = map[string][]string{
		ElementOpenText:             {"placeholder", "inputType", "charLimit"},
		ElementConsent:              {"label"},
		ElementMultipleChoiceSingle: {"choices", "otherOptionPlaceholder"},
		ElementMultipleChoiceMulti:  {"choices", "otherOptionPlaceholder"},
		ElementNPS:                  labelKeys,
		ElementCTA:                  {"buttonExternal", "buttonUrl", "ctaButtonLabel"},
		ElementRating:               labelKeys,
		ElementPictureSelection:     {"choices"},
		ElementDate:                 {"html", "includeTime"},
		ElementFileUpload:           {"allowedFileExtensions"},
		ElementCal:                  {"calUserName", "calHost"},
		ElementMatrix:               {"rows", "columns"},
		ElementAddress:              {"addressLine1", "addressLine2", "city", "state", "zip", "country"},
		ElementRanking:              {"choices", "otherOptionPlaceholder"},
		ElementContactInfo:          {"firstName", "lastName", "email", "phone", "company"},
		ElementCSAT:                 labelKeys,
		ElementCES:                  labelKeys,
		ElementSlider:               {"min", "max", "step", "lowerLabel", "upperLabel"},
	}
)

// UnmarshalJSON reads the keys the element's type declares: a choice has a label (ZSurveyElementChoice) or, in a picture selection, an image (ZSurveyPictureChoice), never both, and a score only where scoring reads one.
func (e *Element) UnmarshalJSON(data []byte) error {
	type plain Element
	fields, ok := objectFields(data)
	if !ok {
		return json.Unmarshal(data, (*plain)(e))
	}
	typ := stringField(fields, "type")
	choiceKeys := []string{"id", "label"}
	switch typ {
	case ElementPictureSelection:
		choiceKeys = []string{"id", "imageUrl", "score"}
	case ElementMultipleChoiceSingle, ElementMultipleChoiceMulti:
		choiceKeys = []string{"id", "label", "score"}
	}
	declaredItems(fields, "choices", choiceKeys...)
	declaredItems(fields, "rows", "id", "label")
	declaredItems(fields, "columns", "id", "label")
	*e = Element{}
	return decodeFields(fields, (*plain)(e), append(slices.Clone(elementBaseKeys), elementKeys[typ]...)...)
}

// OpenTextInputType is the element's input type with zod's default ("text").
func (e *Element) OpenTextInputType() string {
	if e.InputType == "" {
		return "text"
	}
	return e.InputType
}

// IsExternalButton is a CTA's buttonExternal with zod's default (false).
func (e *Element) IsExternalButton() bool { return e.ButtonExternal != nil && *e.ButtonExternal }

// Operand is a logic operand: a static value or a reference to an element, variable, hidden field or reserved field.
type Operand struct {
	Type  string            `json:"type"`
	Value json.RawMessage   `json:"value,omitempty"`
	Meta  map[string]string `json:"meta,omitempty"`
}

// UnmarshalJSON reads the keys the operand's type declares: only an element operand (or the API's deprecated question operand) carries meta.
func (o *Operand) UnmarshalJSON(data []byte) error {
	type plain Operand
	keys := []string{"type", "value"}
	if fields, ok := objectFields(data); ok {
		if t := stringField(fields, "type"); t == "element" || t == "question" {
			keys = append(keys, "meta")
		}
	}
	*o = Operand{}
	return declaredOnly(data, (*plain)(o), keys...)
}

// String returns the value when it is a JSON string.
func (o *Operand) String() (string, bool) {
	var s string
	if len(o.Value) == 0 || json.Unmarshal(o.Value, &s) != nil {
		return "", false
	}
	return s, true
}

// Number returns the value when it is a JSON number.
func (o *Operand) Number() (float64, bool) {
	if len(o.Value) == 0 || o.Value[0] == '"' || o.Value[0] == '[' {
		return 0, false
	}
	var n float64
	if json.Unmarshal(o.Value, &n) != nil {
		return 0, false
	}
	return n, true
}

// Strings returns the value when it is a JSON array of strings; ok is false for any other shape, including an array holding a non-string.
func (o *Operand) Strings() (values []string, isArray bool, allStrings bool) {
	if len(o.Value) == 0 || o.Value[0] != '[' {
		return nil, false, false
	}
	var raw []json.RawMessage
	if json.Unmarshal(o.Value, &raw) != nil {
		return nil, false, false
	}
	allStrings = true
	for _, r := range raw {
		var s string
		if json.Unmarshal(r, &s) != nil {
			allStrings = false
			continue
		}
		values = append(values, s)
	}
	return values, true, allStrings
}

// Truthy is JavaScript truthiness of a static value: "" and 0 are falsy, arrays are always truthy.
func (o *Operand) Truthy() bool {
	if s, ok := o.String(); ok {
		return s != ""
	}
	if n, ok := o.Number(); ok {
		return n != 0
	}
	return len(o.Value) > 0 && !bytes.Equal(o.Value, []byte("null"))
}

// Condition is a single condition or, when IsGroup, a nested group of conditions.
type Condition struct {
	ID           string      `json:"id"`
	Connector    string      `json:"connector,omitempty"`
	Conditions   []Condition `json:"conditions,omitempty"`
	LeftOperand  *Operand    `json:"leftOperand,omitempty"`
	Operator     string      `json:"operator,omitempty"`
	RightOperand *Operand    `json:"rightOperand,omitempty"`
	// IsGroup is the variant zod matched: ZSingleCondition comes first in the union and refuses a connector, so an object with one is a group and any other object a single condition, whose "conditions" key, if any, was stripped.
	IsGroup bool `json:"-"`
}

func (c *Condition) UnmarshalJSON(data []byte) error {
	type plain Condition
	fields, ok := objectFields(data)
	if !ok {
		return json.Unmarshal(data, (*plain)(c))
	}
	_, group := fields["connector"]
	*c = Condition{}
	var err error
	if group {
		err = decodeFields(fields, (*plain)(c), "id", "connector", "conditions")
	} else {
		err = decodeFields(fields, (*plain)(c), "id", "leftOperand", "operator", "rightOperand")
	}
	c.IsGroup = group
	return err
}

// Action is a block logic action: calculate, requireAnswer or jumpToBlock.
type Action struct {
	ID         string   `json:"id"`
	Objective  string   `json:"objective"`
	VariableID string   `json:"variableId,omitempty"`
	Operator   string   `json:"operator,omitempty"`
	Value      *Operand `json:"value,omitempty"`
	Target     string   `json:"target,omitempty"`
}

// UnmarshalJSON reads the keys the action's objective declares: a calculation has a variable, an operator and a value, the other actions a target.
func (a *Action) UnmarshalJSON(data []byte) error {
	type plain Action
	keys := []string{"id", "objective", "target"}
	if fields, ok := objectFields(data); ok && stringField(fields, "objective") == "calculate" {
		keys = []string{"id", "objective", "variableId", "operator", "value"}
	}
	*a = Action{}
	return declaredOnly(data, (*plain)(a), keys...)
}

// Logic is one logic rule of a block.
type Logic struct {
	ID         string    `json:"id"`
	Conditions Condition `json:"conditions"`
	Actions    []Action  `json:"actions"`
}

// Block is a page of the survey.
type Block struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	Elements        []Element  `json:"elements"`
	Logic           []Logic    `json:"logic,omitempty"`
	LogicFallback   string     `json:"logicFallback,omitempty"`
	ButtonLabel     I18nString `json:"buttonLabel,omitempty"`
	BackButtonLabel I18nString `json:"backButtonLabel,omitempty"`
}

// Ending is an end screen or a redirect.
type Ending struct {
	ID              string     `json:"id"`
	Type            string     `json:"type"`
	Headline        I18nString `json:"headline,omitempty"`
	Subheader       I18nString `json:"subheader,omitempty"`
	ButtonLabel     I18nString `json:"buttonLabel,omitempty"`
	ButtonLink      *string    `json:"buttonLink,omitempty"`
	ImageURL        *string    `json:"imageUrl,omitempty"`
	VideoURL        *string    `json:"videoUrl,omitempty"`
	HideDefaultIcon *bool      `json:"hideDefaultIcon,omitempty"`
	URL             *string    `json:"url,omitempty"`
	Label           *string    `json:"label,omitempty"`
}

// UnmarshalJSON reads the keys the ending's type declares: ZSurveyEndScreenCard's card fields or ZSurveyRedirectUrlCard's url and label.
func (e *Ending) UnmarshalJSON(data []byte) error {
	type plain Ending
	keys := []string{"id", "type"}
	if fields, ok := objectFields(data); ok {
		switch stringField(fields, "type") {
		case "endScreen":
			keys = append(keys, "headline", "subheader", "buttonLabel", "buttonLink", "imageUrl", "videoUrl", "hideDefaultIcon")
		case "redirectToUrl":
			keys = append(keys, "url", "label")
		}
	}
	*e = Ending{}
	return declaredOnly(data, (*plain)(e), keys...)
}

// WelcomeCard is the card shown before the first block.
type WelcomeCard struct {
	Enabled           bool       `json:"enabled"`
	Headline          I18nString `json:"headline,omitempty"`
	Subheader         I18nString `json:"subheader,omitempty"`
	FileURL           *string    `json:"fileUrl,omitempty"`
	ButtonLabel       I18nString `json:"buttonLabel,omitempty"`
	TimeToFinish      *bool      `json:"timeToFinish,omitempty"`
	ShowResponseCount *bool      `json:"showResponseCount,omitempty"`
	VideoURL          *string    `json:"videoUrl,omitempty"`
}

// HiddenFields are the values a survey takes from its link or SDK call.
type HiddenFields struct {
	Enabled  bool     `json:"enabled"`
	FieldIDs []string `json:"fieldIds,omitempty"`
}

// Variable is a value logic computes while the survey runs.
type Variable struct {
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Type  string          `json:"type"`
	Value json.RawMessage `json:"value,omitempty"`
	// IsScore marks the survey's total score, which the runtime raises by the score of every picked choice. Only a number variable holds it.
	IsScore bool `json:"isScore,omitempty"`
}

// ScoreVariableUniqueMessage is SCORE_VARIABLE_UNIQUE_MESSAGE, the refinement on a survey's variables in packages/types/surveys/types.ts.
const ScoreVariableUniqueMessage = "Only one variable can hold the total score"

// ScoreVariable is the survey's total score variable: the first number variable marked isScore, nil when there is none (packages/types/surveys/scoring.ts getScoreVariable).
func ScoreVariable(variables []Variable) *Variable {
	for i := range variables {
		if variables[i].Type == "number" && variables[i].IsScore {
			return &variables[i]
		}
	}
	return nil
}

// Language is one of a survey's languages as the rules see it: its stored code and flags.
type Language struct {
	Code    string `json:"code"`
	Default bool   `json:"default"`
	Enabled bool   `json:"enabled"`
}

// FollowUpTrigger says which responses a follow-up fires on.
type FollowUpTrigger struct {
	Type       string `json:"type"`
	Properties *struct {
		EndingIDs []string `json:"endingIds"`
	} `json:"properties"`
}

// FollowUpAction is the email a follow-up sends.
type FollowUpAction struct {
	Type       string `json:"type"`
	Properties struct {
		To                  string   `json:"to"`
		From                string   `json:"from"`
		ReplyTo             []string `json:"replyTo"`
		Subject             string   `json:"subject"`
		Body                string   `json:"body"`
		AttachResponseData  bool     `json:"attachResponseData"`
		IncludeVariables    *bool    `json:"includeVariables,omitempty"`
		IncludeHiddenFields *bool    `json:"includeHiddenFields,omitempty"`
	} `json:"properties"`
}

// FollowUp is a follow-up definition as a survey save carries it; Deleted marks one to remove.
type FollowUp struct {
	ID      string          `json:"id"`
	Name    string          `json:"name"`
	Trigger FollowUpTrigger `json:"trigger"`
	Action  FollowUpAction  `json:"action"`
	Deleted bool            `json:"deleted,omitempty"`
}

// Survey is the part of a survey the document rules read.
type Survey struct {
	WelcomeCard  WelcomeCard
	Blocks       []Block
	Endings      []Ending
	HiddenFields HiddenFields
	Variables    []Variable
	Languages    []Language
	FollowUps    []FollowUp
	// Metadata title and description, the translatable metadata keys, and the social preview image.
	MetadataTitle       I18nString
	MetadataDescription I18nString
	MetadataOGImage     *string
	// Styling is nil when the survey has none.
	Styling *Styling
}

// Styling is the part of a survey's styling the rules read: ZLogo's URL and the background ZSurveyStylingBackground refines.
type Styling struct {
	Logo *struct {
		URL *string `json:"url"`
	} `json:"logo"`
	Background *struct {
		Bg     *string `json:"bg"`
		BgType *string `json:"bgType"`
	} `json:"background"`
}

// ElementRef locates an element of the survey.
type ElementRef struct {
	Block   int
	Element int
	Data    *Element
}

// elementIndex maps every element id to its last occurrence, as forma2's Map.set did: a later duplicate overwrites the earlier one.
func (s *Survey) elementIndex() map[string]ElementRef {
	out := map[string]ElementRef{}
	for b := range s.Blocks {
		for e := range s.Blocks[b].Elements {
			out[s.Blocks[b].Elements[e].ID] = ElementRef{Block: b, Element: e, Data: &s.Blocks[b].Elements[e]}
		}
	}
	return out
}

func (s *Survey) variable(id string) *Variable {
	for i := range s.Variables {
		if s.Variables[i].ID == id {
			return &s.Variables[i]
		}
	}
	return nil
}

func (s *Survey) hasHiddenField(id string) bool {
	for _, f := range s.HiddenFields.FieldIDs {
		if f == id {
			return true
		}
	}
	return false
}
