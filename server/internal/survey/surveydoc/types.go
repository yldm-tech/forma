// Package surveydoc is the survey document: the typed view of the JSON a survey stores (welcome card, blocks with their elements and logic, endings, hidden fields, variables, follow-ups) and the rules forma2 enforced on it with zod refinements, which the JSON Schemas exported from packages/types cannot express.
//
// The document is stored as the JSON it arrived as; these types are what the rules read. A field the types do not name is kept by the stored JSON and ignored by the rules, as zod's object schemas ignored it.
package surveydoc

import (
	"bytes"
	"encoding/json"
)

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
)

// Choice is a choice of a choice element, a matrix row or column, or a picture of a picture selection.
type Choice struct {
	ID       string     `json:"id"`
	Label    I18nString `json:"label,omitempty"`
	ImageURL string     `json:"imageUrl,omitempty"`
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
	// IsGroup is forma2's isConditionGroup: the object carries a "conditions" key.
	IsGroup bool `json:"-"`
}

func (c *Condition) UnmarshalJSON(data []byte) error {
	type plain Condition
	var p plain
	if err := json.Unmarshal(data, &p); err != nil {
		return err
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(data, &keys); err != nil {
		return err
	}
	_, p.IsGroup = keys["conditions"]
	*c = Condition(p)
	return nil
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
	// Metadata title and description, the translatable metadata keys.
	MetadataTitle       I18nString
	MetadataDescription I18nString
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
