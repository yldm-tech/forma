// Package answers is forma2's answer validation (validateResponseData in apps/web/modules/api/lib/validation.ts over validateBlockResponses in packages/surveys/src/lib/validation): the checks the survey runtime runs in the respondent's browser, run again on the server because the response endpoints are public. Only the elements whose key the response data carries are validated, so a partial response is never held to the questions it has not reached.
//
// The messages are the runtime's own, in the response's language, from a copy of packages/surveys/locales kept identical by a test.
package answers

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/yldm-tech/forma/server/internal/platform/jsvalue"
)

// Element types the checks treat specially.
const (
	typeOpenText             = "openText"
	typeMultipleChoiceMulti  = "multipleChoiceMulti"
	typeMultipleChoiceSingle = "multipleChoiceSingle"
	typeCTA                  = "cta"
	typeRanking              = "ranking"
	typeMatrix               = "matrix"
	typeAddress              = "address"
	typeContactInfo          = "contactInfo"
	typeFileUpload           = "fileUpload"
)

// otherChoiceID is the id of a choice element's "Other" option.
const otherChoiceID = "other"

// Error is one failed check of an element, forma2's TValidationError.
type Error struct {
	RuleID   string
	RuleType string
	Message  string
}

// ElementErrors is one element's failed checks.
type ElementErrors struct {
	ElementID string
	Errors    []Error
}

type choice struct {
	ID    string            `json:"id"`
	Label map[string]string `json:"label"`
}

type field struct {
	Show        bool              `json:"show"`
	Placeholder map[string]string `json:"placeholder"`
}

type rule struct {
	ID     string         `json:"id"`
	Type   string         `json:"type"`
	Field  string         `json:"field"`
	Params map[string]any `json:"params"`
}

type validation struct {
	Rules []rule `json:"rules"`
	Logic string `json:"logic"`
}

// element is what the checks read of a survey element. A missing list stays nil, so "choices" in element and the like can be told from an empty one.
type element struct {
	ID         string      `json:"id"`
	Type       string      `json:"type"`
	Required   bool        `json:"required"`
	InputType  *string     `json:"inputType"`
	Choices    []choice    `json:"choices"`
	Rows       []any       `json:"rows"`
	Validation *validation `json:"validation"`

	AddressLine1 *field `json:"addressLine1"`
	AddressLine2 *field `json:"addressLine2"`
	City         *field `json:"city"`
	State        *field `json:"state"`
	Zip          *field `json:"zip"`
	Country      *field `json:"country"`
	FirstName    *field `json:"firstName"`
	LastName     *field `json:"lastName"`
	Email        *field `json:"email"`
	Phone        *field `json:"phone"`
	Company      *field `json:"company"`
}

type block struct {
	Elements []element `json:"elements"`
}

// Validate is forma2's validateResponseData: the response data checked against the survey's blocks, every element whose key the data carries, in document order. It returns nil when every check passes. language is the response's language as the client sent it, which picks the choice labels a value may match; defaultLanguage is the survey's default language code, which the messages are written in when language is "default". A survey without blocks has nothing to check.
func Validate(blocks json.RawMessage, data map[string]any, language, defaultLanguage string, now time.Time) ([]ElementErrors, error) {
	if data == nil || len(blocks) == 0 {
		return nil, nil
	}
	var bs []block
	if err := json.Unmarshal(blocks, &bs); err != nil {
		return nil, fmt.Errorf("decode survey blocks: %w", err)
	}
	messageLanguage := language
	if language == "default" {
		messageLanguage = defaultLanguage
	}
	c := checker{language: language, t: translator(messageLanguage), now: now}
	var out []ElementErrors
	for _, b := range bs {
		for i := range b.Elements {
			e := &b.Elements[i]
			value, present := data[e.ID]
			if !present {
				continue
			}
			if errs := c.element(e, value); len(errs) > 0 {
				out = append(out, ElementErrors{ElementID: e.ID, Errors: errs})
			}
		}
	}
	return out, nil
}

// V1Details is forma2's formatValidationErrorsForV1Api: each element's messages joined with "; ", keyed "response.data.<element id>", in element order.
func V1Details(errs []ElementErrors) []Detail {
	out := make([]Detail, len(errs))
	for i, e := range errs {
		messages := make([]string, len(e.Errors))
		for j, err := range e.Errors {
			messages[j] = err.Message
		}
		out[i] = Detail{Key: "response.data." + e.ElementID, Message: strings.Join(messages, "; ")}
	}
	return out
}

// Detail is one entry of a v1 error's details.
type Detail struct {
	Key     string
	Message string
}

type checker struct {
	language string
	t        translate
	now      time.Time
}

// isEmpty is the runtime's emptiness: an empty string, array or object.
func isEmpty(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return x == ""
	case []any:
		return len(x) == 0
	case map[string]any:
		return len(x) == 0
	}
	return v == jsvalue.Undefined
}

// localized is the runtime's getLocalizedValue: the label in language, else its default when that translation is missing or blank.
func localized(label map[string]string, language string) string {
	if label == nil {
		return ""
	}
	if _, ok := label["default"]; !ok {
		return ""
	}
	if v, ok := label[language]; ok && jsvalue.Trim(v) != "" {
		return v
	}
	return label["default"]
}

func (c checker) required() Error {
	return Error{RuleID: "required", RuleType: "minLength", Message: c.t("please_fill_out_this_field", nil)}
}

func (c checker) invalidOption() Error {
	return Error{RuleID: "invalidOption", RuleType: "minLength", Message: c.t("invalid_format", nil)}
}

// element is the runtime's validateElementResponse.
func (c checker) element(e *element, value any) []Error {
	var errs []Error
	if err, ok := c.requiredCheck(e, value); ok {
		errs = append(errs, err)
	}
	if err, ok := c.singleSelectOther(e, value); ok {
		errs = append(errs, err)
	}
	if err, ok := c.multiSelectOther(e, value); ok {
		errs = append(errs, err)
	}
	if err, ok := c.choiceMembership(e, value); ok {
		errs = append(errs, err)
	}

	var authorRules []rule
	logic := "and"
	if e.Validation != nil {
		authorRules = e.Validation.Rules
		if e.Validation.Logic != "" {
			logic = e.Validation.Logic
		}
	}
	// Implicit rules come from the element type, not the author, so they are always ANDed: folded into an "or" group they would be an alternative the input satisfies by construction.
	implicit := implicitRules(e, authorRules)
	if len(authorRules) == 0 && len(implicit) == 0 {
		return errs
	}
	if len(authorRules) > 0 {
		if logic == "or" {
			errs = c.anyRule(authorRules, e, value, errs)
		} else {
			errs = c.allRules(authorRules, e, value, errs)
		}
	}
	return c.allRules(implicit, e, value, errs)
}

func (c checker) requiredCheck(e *element, value any) (Error, bool) {
	if !e.Required || e.Type == typeCTA {
		return Error{}, false
	}
	switch e.Type {
	case typeRanking:
		if arr, ok := value.([]any); isEmpty(value) || !ok || len(arr) < 1 {
			return c.required(), true
		}
		return Error{}, false
	case typeMatrix:
		if isEmpty(value) {
			return c.required(), true
		}
		if obj, ok := value.(map[string]any); ok && e.Rows != nil && answeredRows(obj) == 0 {
			return c.required(), true
		}
		return Error{}, false
	}
	if isEmpty(value) {
		return c.required(), true
	}
	return Error{}, false
}

func answeredRows(obj map[string]any) int {
	n := 0
	for _, v := range obj {
		if s, ok := v.(string); !ok || s != "" {
			if v != nil {
				n++
			}
		}
	}
	return n
}

func hasOther(e *element) bool {
	return slices.ContainsFunc(e.Choices, func(ch choice) bool { return ch.ID == otherChoiceID })
}

// multiSelectOther is the runtime's validateMultiSelectOtherValue: a selected "Other" (the "" sentinel) needs its text after it.
func (c checker) multiSelectOther(e *element, value any) (Error, bool) {
	arr, ok := value.([]any)
	if e.Type != typeMultipleChoiceMulti || !ok || !hasOther(e) {
		return Error{}, false
	}
	sentinel := slices.IndexFunc(arr, func(v any) bool { s, ok := v.(string); return ok && s == "" })
	if sentinel == -1 {
		return Error{}, false
	}
	text := jsvalue.Undefined
	if sentinel+1 < len(arr) {
		text = arr[sentinel+1]
	}
	if s, ok := text.(string); !ok || jsvalue.Trim(s) == "" {
		return c.required(), true
	}
	return Error{}, false
}

// singleSelectOther is the runtime's validateSingleSelectOtherValue: a value that is no configured choice is the "Other" text, which must not be blank.
func (c checker) singleSelectOther(e *element, value any) (Error, bool) {
	s, ok := value.(string)
	if e.Type != typeMultipleChoiceSingle || !ok {
		return Error{}, false
	}
	if !hasOther(e) || (e.Required && s == "") {
		return Error{}, false
	}
	for _, ch := range e.Choices {
		if ch.ID != otherChoiceID && (ch.ID == s || localized(ch.Label, c.language) == s) {
			return Error{}, false
		}
	}
	if jsvalue.Trim(s) == "" {
		return c.required(), true
	}
	return Error{}, false
}

// choiceMembership is the runtime's validateChoiceMembership: without an "Other" option, a choice element only takes its configured choices, by id or label in the response's language.
func (c checker) choiceMembership(e *element, value any) (Error, bool) {
	if e.Type != typeMultipleChoiceSingle && e.Type != typeMultipleChoiceMulti {
		return Error{}, false
	}
	if e.Choices == nil || hasOther(e) || isEmpty(value) {
		return Error{}, false
	}
	known := map[string]bool{}
	for _, ch := range e.Choices {
		known[ch.ID] = true
		if label := localized(ch.Label, c.language); label != "" {
			known[label] = true
		}
	}
	submitted, isArray := value.([]any)
	if !isArray {
		submitted = []any{value}
	}
	for _, v := range submitted {
		s, ok := v.(string)
		if ok && s == "" {
			continue
		}
		if !ok || !known[s] {
			return c.invalidOption(), true
		}
	}
	return Error{}, false
}

func hasRule(rules []rule, typ, field string) bool {
	return slices.ContainsFunc(rules, func(r rule) bool { return r.Type == typ && r.Field == field })
}

// implicitRules are the checks an element's type implies and its author did not write: an email, URL or phone open text, and the shown email and phone of a contact info element.
func implicitRules(e *element, author []rule) []rule {
	var out []rule
	switch e.Type {
	case typeOpenText:
		if e.InputType == nil {
			return nil
		}
		for _, t := range []string{"email", "url", "phone"} {
			if *e.InputType == t && !slices.ContainsFunc(author, func(r rule) bool { return r.Type == t }) {
				out = append(out, rule{ID: "__implicit_" + t + "__", Type: t})
			}
		}
	case typeContactInfo:
		if e.Email != nil && e.Email.Show && !hasRule(author, "email", "email") {
			out = append(out, rule{ID: "__implicit_email_field__", Type: "email", Field: "email"})
		}
		if e.Phone != nil && e.Phone.Show && !hasRule(author, "phone", "phone") {
			out = append(out, rule{ID: "__implicit_phone_field__", Type: "phone", Field: "phone"})
		}
	}
	return out
}

var (
	addressFields     = []string{"addressLine1", "addressLine2", "city", "state", "zip", "country"}
	contactInfoFields = []string{"firstName", "lastName", "email", "phone", "company"}
)

// fieldValue is the runtime's getFieldValue: a rule on one field of an address or contact info element reads that field's position in the answer.
func fieldValue(r rule, e *element, value any) any {
	if r.Field == "" {
		return value
	}
	arr, ok := value.([]any)
	if !ok {
		return ""
	}
	var order []string
	switch e.Type {
	case typeAddress:
		order = addressFields
	case typeContactInfo:
		order = contactInfoFields
	default:
		return ""
	}
	if i := slices.Index(order, r.Field); i >= 0 && i < len(arr) {
		if arr[i] == nil {
			return ""
		}
		return arr[i]
	}
	return ""
}

// fieldLabel is the runtime's getFieldLabel: the placeholder of the address or contact info field a rule checks.
func fieldLabel(e *element, name, language string) string {
	var f *field
	switch {
	case e.Type == typeAddress && e.AddressLine1 != nil:
		f = map[string]*field{"addressLine1": e.AddressLine1, "addressLine2": e.AddressLine2, "city": e.City, "state": e.State, "zip": e.Zip, "country": e.Country}[name]
	case e.Type == typeContactInfo && e.FirstName != nil:
		f = map[string]*field{"firstName": e.FirstName, "lastName": e.LastName, "email": e.Email, "phone": e.Phone, "company": e.Company}[name]
	}
	if f == nil || f.Placeholder == nil {
		return ""
	}
	return localized(f.Placeholder, language)
}

// message is the runtime's getDefaultErrorMessage: the rule's message, after the field's label for a field rule.
func (c checker) message(r rule, e *element) string {
	v, ok := validators[r.Type]
	if !ok {
		return c.t("invalid_format", nil)
	}
	msg := v.message(c, r.Params, e)
	if r.Field != "" {
		if label := fieldLabel(e, r.Field, c.language); label != "" {
			return label + ": " + msg
		}
	}
	return msg
}

// anyRule is the runtime's executeOrLogic: the element passes its rules when one of them passes.
func (c checker) anyRule(rules []rule, e *element, value any, initial []Error) []Error {
	var failed []Error
	for _, r := range rules {
		v, ok := validators[r.Type]
		if !ok {
			continue
		}
		if v.check(c, fieldValue(r, e, value), r.Params, e) {
			return initial
		}
		failed = append(failed, Error{RuleID: r.ID, RuleType: r.Type, Message: c.message(r, e)})
	}
	return append(initial, failed...)
}

// allRules is the runtime's executeAndLogic: every rule must pass.
func (c checker) allRules(rules []rule, e *element, value any, initial []Error) []Error {
	errs := initial
	for _, r := range rules {
		v, ok := validators[r.Type]
		if !ok {
			continue
		}
		if !v.check(c, fieldValue(r, e, value), r.Params, e) {
			errs = append(errs, Error{RuleID: r.ID, RuleType: r.Type, Message: c.message(r, e)})
		}
	}
	return errs
}
