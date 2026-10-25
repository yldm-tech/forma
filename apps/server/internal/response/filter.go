package response

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/embeddeddata"
	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/jsvalue"
	"github.com/yldm-tech/forma/apps/server/internal/survey/surveydoc"
)

// Condition is one filter condition: an operator and, for most, a value.
type Condition struct {
	Op    string          `json:"op"`
	Value json.RawMessage `json:"value,omitempty"`
}

// Conditions are the conditions on one key of a criteria group, all of which must hold. The JSON is one condition as an object, or several as an array, which is how the views put a range on one field (a rating above 2 and below 8). An empty array is malformed.
type Conditions []Condition

// UnmarshalJSON reads one condition object or an array of them.
func (cs *Conditions) UnmarshalJSON(b []byte) error {
	if trimmed := bytes.TrimSpace(b); len(trimmed) > 0 && trimmed[0] == '[' {
		var list []Condition
		if err := json.Unmarshal(trimmed, &list); err != nil {
			return err
		}
		*cs = list
		return nil
	}
	var c Condition
	if err := json.Unmarshal(b, &c); err != nil {
		return err
	}
	*cs = Conditions{c}
	return nil
}

// MarshalJSON writes a single condition as an object, the shape every client sent before ranges, and several as an array.
func (cs Conditions) MarshalJSON() ([]byte, error) {
	if len(cs) == 1 {
		return json.Marshal(cs[0])
	}
	return json.Marshal([]Condition(cs))
}

// Range bounds the creation time; either end may be open.
type Range struct {
	Min *time.Time `json:"min"`
	Max *time.Time `json:"max"`
}

// TagFilter keeps responses carrying every applied tag and none of the not applied ones.
type TagFilter struct {
	Applied    []string `json:"applied"`
	NotApplied []string `json:"notApplied"`
}

// Criteria is forma2's TResponseFilterCriteria, what the response views filter by. Every keyed group takes one condition or several per key (Conditions). Search is a keyword the response's free-text answers must contain.
type Criteria struct {
	Finished          *bool                    `json:"finished"`
	ResponseIDs       []uuid.UUID              `json:"responseIds"`
	CreatedAt         *Range                   `json:"createdAt"`
	ContactAttributes map[string]Conditions    `json:"contactAttributes"`
	Data              map[string]Conditions    `json:"data"`
	Variables         map[string]Conditions    `json:"variables"`
	Reserved          map[string]Conditions    `json:"reserved"`
	Tags              *TagFilter               `json:"tags"`
	Others            map[string]Conditions    `json:"others"`
	Meta              map[string]Conditions    `json:"meta"`
	Quotas            map[uuid.UUID]Conditions `json:"quotas"`
	Search            *string                  `json:"search"`
}

// clause is a conjunction of SQL conditions on the response row `r`, with their arguments in order.
type clause struct {
	sql  []string
	args []any
}

func (c *clause) add(q *cond) {
	c.sql = append(c.sql, q.b.String())
	c.args = append(c.args, q.args...)
}

func (c *clause) and() string {
	if len(c.sql) == 0 {
		return "TRUE"
	}
	return "(" + strings.Join(c.sql, " AND ") + ")"
}

// cond builds one SQL condition. Its parts are SQL text, arg values (written as placeholders) and JSON paths (written as path expressions with their keys as arguments), so every argument lands in the order of its placeholder.
type cond struct {
	b    strings.Builder
	args []any
}

type arg struct{ v any }

// jsonPath is a path into a JSON column of r.
type jsonPath struct {
	column string
	keys   []string
}

func q(parts ...any) *cond {
	c := &cond{}
	return c.w(parts...)
}

func (c *cond) w(parts ...any) *cond {
	for _, p := range parts {
		switch x := p.(type) {
		case string:
			c.b.WriteString(x)
		case arg:
			c.b.WriteString("?")
			c.args = append(c.args, x.v)
		case jsonPath:
			c.b.WriteString("(r." + x.column + " #> ARRAY[")
			for i, k := range x.keys {
				if i > 0 {
					c.b.WriteString(", ")
				}
				c.b.WriteString("?")
				c.args = append(c.args, k)
			}
			c.b.WriteString("]::text[])")
		case *cond:
			c.b.WriteString(x.b.String())
			c.args = append(c.args, x.args...)
		default:
			panic(fmt.Sprintf("cond: unsupported part %T", p))
		}
	}
	return c
}

func invalidFilter(field string) error {
	return apperr.Validation([]apperr.FieldError{{Field: "filterCriteria." + field, Rule: "invalid"}})
}

// jsonValue checks a condition's value against the JSON kinds it may be, and returns it.
func jsonValue(raw json.RawMessage, kinds ...string) (any, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return nil, false
	}
	kind := ""
	switch x := v.(type) {
	case string:
		kind = "string"
	case float64:
		kind = "number"
	case bool:
		kind = "boolean"
	case []any:
		kind = "array"
		for _, e := range x {
			if _, ok := e.(string); !ok {
				if _, ok := e.(float64); !ok {
					return nil, false
				}
			}
		}
	case map[string]any:
		kind = "object"
	}
	return v, slices.Contains(kinds, kind)
}

// jsonb is a value as a jsonb argument.
func jsonb(v any) arg {
	b, _ := json.Marshal(v) // a decoded JSON value always encodes
	return arg{string(b)}
}

var textOps = []string{"contains", "doesNotContain", "startsWith", "doesNotStartWith", "endsWith", "doesNotEndWith"}

// textCondition is Prisma's string_contains, string_starts_with and string_ends_with on a JSON path, negated by the doesNot operators: the value is a string that contains, starts or ends with s. Matched literally, without LIKE wildcards, and ignoring case, as the segment filters' ILIKE does: someone searching answers for "onboarding" means "Onboarding" too.
func textCondition(p jsonPath, op string, s string) *cond {
	text := q("lower(", p, " #>> '{}')")
	needle := q("lower(", arg{s}, ")")
	var test *cond
	switch op {
	case "contains", "doesNotContain":
		test = q("strpos(", text, ", ", needle, ") > 0")
	case "startsWith", "doesNotStartWith":
		test = q("starts_with(", text, ", ", needle, ")")
	default:
		test = q("right(", text, ", char_length(", needle, ")) = ", needle)
	}
	c := q("(jsonb_typeof(", p, ") = 'string' AND ", test, ")")
	if strings.HasPrefix(op, "doesNot") {
		return q("NOT ", c)
	}
	return c
}

var comparisons = map[string]string{"lessThan": "<", "lessEqual": "<=", "greaterThan": ">", "greaterEqual": ">="}

// comparison compares a JSON path's value with a number or a string of the same kind; jsonb orders values of different kinds by kind, which no filter means.
func comparison(p jsonPath, op string, v any) *cond {
	kind := "number"
	if _, isString := v.(string); isString {
		kind = "string"
	}
	return q("(jsonb_typeof(", p, ") = '"+kind+"' AND ", p, " "+comparisons[op]+" ", jsonb(v), "::jsonb)")
}

// typedCondition is forma2's buildJsonPathCondition for the typed Embedded Data and reserved groups: notEquals and isNotSet count an absent value as a match; a text operator applies only to a string field and a comparison only to a number or date field, any other pairing matching nothing. ok is false for a malformed condition.
func typedCondition(p jsonPath, c Condition, dataType string) (*cond, bool) {
	switch c.Op {
	case "isSet":
		return q(p, " IS NOT NULL"), true
	case "isNotSet":
		return q(p, " IS NULL"), true
	case "equals":
		v, ok := jsonValue(c.Value, "string", "number", "boolean")
		return q(p, " = ", jsonb(v), "::jsonb"), ok
	case "notEquals":
		v, ok := jsonValue(c.Value, "string", "number", "boolean")
		return q("(", p, " <> ", jsonb(v), "::jsonb OR ", p, " IS NULL)"), ok
	}
	if slices.Contains(textOps, c.Op) {
		v, ok := jsonValue(c.Value, "string")
		if !ok {
			return nil, false
		}
		if dataType != embeddeddata.TypeString {
			return q("FALSE"), true
		}
		return textCondition(p, c.Op, v.(string)), true
	}
	if _, ok := comparisons[c.Op]; ok {
		v, ok := jsonValue(c.Value, "string", "number")
		if !ok {
			return nil, false
		}
		if dataType != embeddeddata.TypeNumber && dataType != embeddeddata.TypeDate {
			return q("FALSE"), true
		}
		return comparison(p, c.Op, v), true
	}
	return nil, false
}

// reservedLocators is forma2's RESERVED_FILTER_LOCATORS: where each filterable reserved field lives in meta. durationSeconds lives in ttc and has its own condition.
var reservedLocators = map[string][]string{
	"source": {"source"}, "url": {"url"}, "country": {"country"}, "action": {"action"},
	"browser": {"userAgent", "browser"}, "os": {"userAgent", "os"}, "deviceType": {"userAgent", "device"},
	"ipAddress": {"ipAddress"}, "pagePath": {"pagePath"}, "pageReferrer": {"pageReferrer"},
	"utmSource": {"utmSource"}, "utmMedium": {"utmMedium"}, "utmCampaign": {"utmCampaign"}, "utmTerm": {"utmTerm"}, "utmContent": {"utmContent"},
	"screenWidth": {"screenWidth"}, "screenHeight": {"screenHeight"}, "viewportWidth": {"viewportWidth"}, "viewportHeight": {"viewportHeight"},
	"timezone": {"timezone"}, "locale": {"locale"},
}

// durationCondition is forma2's buildDurationSecondsCondition: seconds against ttc._total in milliseconds, through the windows the read seam's Math.round(ms / 1000) projects onto each second. Only finished responses carry a total.
func durationCondition(c Condition) (*cond, bool) {
	p := jsonPath{"ttc", []string{"_total"}}
	switch c.Op {
	case "isSet":
		return q(p, " IS NOT NULL"), true
	case "isNotSet":
		return q(p, " IS NULL"), true
	}
	v, ok := jsonValue(c.Value, "string", "number", "boolean")
	if !ok {
		return nil, false
	}
	seconds, isNumber := v.(float64)
	if !isNumber {
		return q("FALSE"), true
	}
	lower, upper := seconds*1000-500, seconds*1000+500
	ms := func(op string, bound float64) *cond {
		return q("(jsonb_typeof(", p, ") = 'number' AND (", p, ")::numeric "+op+" ", arg{bound}, ")")
	}
	switch c.Op {
	case "equals":
		return q("(", ms(">=", lower), " AND ", ms("<", upper), ")"), true
	case "notEquals":
		return q("(", ms("<", lower), " OR ", ms(">=", upper), " OR ", p, " IS NULL)"), true
	case "greaterThan":
		return ms(">=", upper), true
	case "greaterEqual":
		return ms(">=", lower), true
	case "lessThan":
		return ms("<", lower), true
	case "lessEqual":
		return ms("<", upper), true
	}
	return q("FALSE"), true
}

// where is forma2's buildWhereClause for one survey: every criteria group ANDed. Names the survey's declared fields or elements shadow drop out of the reserved group, and an unknown reserved or variable name filters nothing; a malformed condition is refused.
func where(sv *Survey, f *Criteria) (*clause, error) {
	w := &clause{}
	if f == nil {
		return w, nil
	}
	if f.Finished != nil {
		w.add(q("r.finished = ", arg{*f.Finished}))
	}
	if f.CreatedAt != nil {
		if f.CreatedAt.Max != nil {
			w.add(q("r.created_at <= ", arg{*f.CreatedAt.Max}))
		}
		if f.CreatedAt.Min != nil {
			w.add(q("r.created_at >= ", arg{*f.CreatedAt.Min}))
		}
	}
	if f.Tags != nil {
		for _, name := range f.Tags.Applied {
			w.add(q("EXISTS (SELECT 1 FROM response_tags rt JOIN tags t ON t.id = rt.tag_id WHERE rt.response_id = r.id AND t.name = ", arg{name}, ")"))
		}
		if len(f.Tags.NotApplied) > 0 {
			w.add(q("NOT EXISTS (SELECT 1 FROM response_tags rt JOIN tags t ON t.id = rt.tag_id WHERE rt.response_id = r.id AND t.name IN ", arg{f.Tags.NotApplied}, ")"))
		}
	}
	for key, cs := range f.ContactAttributes {
		if len(cs) == 0 {
			return nil, invalidFilter("contactAttributes")
		}
		for _, c := range cs {
			condition, ok := attributeCondition(jsonPath{"contact_attributes", []string{key}}, c)
			if !ok {
				return nil, invalidFilter("contactAttributes")
			}
			w.add(condition)
		}
	}
	for key, cs := range f.Meta {
		if len(cs) == 0 {
			return nil, invalidFilter("meta")
		}
		p := jsonPath{"meta", []string{key}}
		if key == "browser" || key == "os" || key == "device" {
			p.keys = []string{"userAgent", key}
		}
		for _, c := range cs {
			switch {
			case c.Op == "equals" || c.Op == "notEquals":
				v, ok := jsonValue(c.Value, "string", "number", "boolean")
				if !ok {
					return nil, invalidFilter("meta")
				}
				op := " = "
				if c.Op == "notEquals" {
					op = " <> "
				}
				w.add(q(p, op, jsonb(v), "::jsonb"))
			case slices.Contains(textOps, c.Op):
				v, ok := jsonValue(c.Value, "string")
				if !ok {
					return nil, invalidFilter("meta")
				}
				w.add(textCondition(p, c.Op, v.(string)))
			default:
				return nil, invalidFilter("meta")
			}
		}
	}
	for key, cs := range f.Others {
		// forma2 used the lowercased key as a column name; the response views only ever filter the language.
		if strings.ToLower(key) != "language" || len(cs) == 0 {
			return nil, invalidFilter("others")
		}
		for _, c := range cs {
			v, ok := jsonValue(c.Value, "string", "number")
			if !ok {
				return nil, invalidFilter("others")
			}
			text := fmt.Sprint(v)
			if n, isNumber := v.(float64); isNumber {
				text = jsvalue.FormatNumber(n)
			}
			switch c.Op {
			case "equals":
				w.add(q("r.language = ", arg{text}))
			case "notEquals":
				w.add(q("r.language <> ", arg{text}))
			default:
				return nil, invalidFilter("others")
			}
		}
	}
	if err := reservedAndVariables(w, sv, f); err != nil {
		return nil, err
	}
	for key, cs := range f.Data {
		if len(cs) == 0 {
			return nil, invalidFilter("data")
		}
		for _, c := range cs {
			if err := dataCondition(w, sv, key, c); err != nil {
				return nil, err
			}
		}
	}
	if f.Search != nil {
		if keyword := strings.TrimSpace(*f.Search); keyword != "" {
			w.add(searchCondition(sv, keyword))
		}
	}
	if f.ResponseIDs != nil {
		if len(f.ResponseIDs) == 0 {
			w.add(q("FALSE"))
		} else {
			w.add(q("r.id IN ", arg{f.ResponseIDs}))
		}
	}
	for quotaID, cs := range f.Quotas {
		if len(cs) == 0 {
			return nil, invalidFilter("quotas")
		}
		for _, c := range cs {
			switch c.Op {
			case statusScreenedIn, statusScreenedOut:
				w.add(q("EXISTS (SELECT 1 FROM response_quota_links l WHERE l.response_id = r.id AND l.quota_id = ", arg{quotaID}, " AND l.status = ", arg{c.Op}, ")"))
			case "screenedOutNotInQuota":
				w.add(q("NOT EXISTS (SELECT 1 FROM response_quota_links l WHERE l.response_id = r.id AND l.quota_id = ", arg{quotaID}, ")"))
			default:
				return nil, invalidFilter("quotas")
			}
		}
	}
	return w, nil
}

// searchElementTypes are the elements whose answers are the respondent's own words, what the keyword search reads: an open text answer, and the fields of an address or contact info answer.
var searchElementTypes = []string{surveydoc.ElementOpenText, surveydoc.ElementAddress, surveydoc.ElementContactInfo}

// searchCondition is a response one of whose free-text answers contains keyword, ignoring case as the text operators do: a text answer containing it, or a list answer (address, contact info) one of whose fields does. A survey without such elements has nothing to find.
func searchCondition(sv *Survey, keyword string) *cond {
	var keys []string
	for _, e := range sv.Elements() {
		if slices.Contains(searchElementTypes, e.Type) {
			keys = append(keys, e.ID)
		}
	}
	if len(keys) == 0 {
		return q("FALSE")
	}
	needle := q("lower(", arg{keyword}, ")")
	// CASE, unlike AND, fixes the evaluation order, so jsonb_array_elements_text only ever reads an array.
	return q("EXISTS (SELECT 1 FROM jsonb_each(r.data) a WHERE a.key IN ", arg{keys}, " AND CASE jsonb_typeof(a.value)",
		" WHEN 'string' THEN strpos(lower(a.value #>> '{}'), ", needle, ") > 0",
		" WHEN 'array' THEN EXISTS (SELECT 1 FROM jsonb_array_elements_text(a.value) f WHERE strpos(lower(f), ", needle, ") > 0)",
		" ELSE FALSE END)")
}

// attributeNumberPattern is text a contact attribute snapshot holds for a number: the response stores every attribute as text, a number as JavaScript's String(n) prints it.
const attributeNumberPattern = `^-?([0-9]+\.?[0-9]*|\.[0-9]+)([eE][-+]?[0-9]+)?$`

// attributeDatePattern is the start of a date attribute's text, Date#toISOString's YYYY-MM-DD; dateValuePattern is a date the views compare it with.
const (
	attributeDatePattern = `^[0-9]{4}-[0-9]{2}-[0-9]{2}`
	dateValuePattern     = attributeDatePattern + `$`
)

var dateValue = regexp.MustCompile(dateValuePattern)

// attributeNumber is a contact attribute's value as a number: a JSON number as it is (responses imported from forma2 hold some), text that reads as a number cast to one, NULL for anything else. CASE keeps the cast from text that would not parse.
func attributeNumber(p jsonPath) *cond {
	return q("(CASE jsonb_typeof(", p, ") WHEN 'number' THEN (", p, ")::numeric WHEN 'string' THEN CASE WHEN (", p, " #>> '{}') ~ ", arg{attributeNumberPattern},
		" THEN (", p, " #>> '{}')::numeric END END)")
}

// attributeDay is a date contact attribute's day, the YYYY-MM-DD its ISO text starts with (UTC, as toISOString writes it), NULL for a value that is no date.
func attributeDay(p jsonPath) *cond {
	return q("(CASE WHEN jsonb_typeof(", p, ") = 'string' AND (", p, " #>> '{}') ~ ", arg{attributeDatePattern}, " THEN left(", p, " #>> '{}', 10) END)")
}

// attributeCondition is one condition on the contact attribute snapshot a response holds, by the attribute's type, which the operator and the value's kind name: a string value compares as text (equals, notEquals and the text operators), a number value as a number (equals, notEquals and the comparisons), a YYYY-MM-DD value with isBefore, isAfter or isSameDay as a day. notEquals and isNotSet count an absent attribute, or no contact at all, as a match. ok is false for a malformed condition.
func attributeCondition(p jsonPath, c Condition) (*cond, bool) {
	switch c.Op {
	case "isSet":
		return q(p, " IS NOT NULL"), true
	case "isNotSet":
		return q(p, " IS NULL"), true
	case "isBefore", "isAfter", "isSameDay":
		v, ok := jsonValue(c.Value, "string")
		if !ok || !dateValue.MatchString(v.(string)) {
			return nil, false
		}
		cmp := map[string]string{"isBefore": " < ", "isAfter": " > ", "isSameDay": " = "}[c.Op]
		return q(attributeDay(p), cmp, arg{v}), true
	}
	if slices.Contains(textOps, c.Op) {
		v, ok := jsonValue(c.Value, "string")
		if !ok {
			return nil, false
		}
		return textCondition(p, c.Op, v.(string)), true
	}
	v, ok := jsonValue(c.Value, "string", "number")
	if !ok {
		return nil, false
	}
	n, isNumber := v.(float64)
	switch {
	case c.Op == "equals" && isNumber:
		return q(attributeNumber(p), " = ", arg{n}), true
	case c.Op == "notEquals" && isNumber:
		return q("(", attributeNumber(p), " <> ", arg{n}, " OR ", attributeNumber(p), " IS NULL)"), true
	case c.Op == "equals":
		return q(p, " = ", jsonb(v), "::jsonb"), true
	case c.Op == "notEquals":
		return q("(", p, " <> ", jsonb(v), "::jsonb OR ", p, " IS NULL)"), true
	}
	if cmp, isComparison := comparisons[c.Op]; isComparison && isNumber {
		return q(attributeNumber(p), " "+cmp+" ", arg{n}), true
	}
	return nil, false
}

func reservedAndVariables(w *clause, sv *Survey, f *Criteria) error {
	shadowed := map[string]bool{}
	for _, name := range embeddeddata.ShadowingNames(sv.Fields, sv.ElementIDs()) {
		shadowed[name] = true
	}
	for name, cs := range f.Reserved {
		if len(cs) == 0 {
			return invalidFilter("reserved")
		}
		if shadowed[name] {
			continue
		}
		keys, known := reservedLocators[name]
		if !known && name != "durationSeconds" {
			continue
		}
		dataType := embeddeddata.TypeString
		for _, e := range embeddeddata.ReservedCatalog {
			if e.Name == name {
				dataType = e.DataType
			}
		}
		for _, c := range cs {
			var condition *cond
			var ok bool
			if name == "durationSeconds" {
				condition, ok = durationCondition(c)
			} else {
				condition, ok = typedCondition(jsonPath{"meta", keys}, c, dataType)
			}
			if !ok {
				return invalidFilter("reserved")
			}
			w.add(condition)
		}
	}
	computed := embeddeddata.OfSource(sv.Fields, embeddeddata.SourceComputed)
	for key, cs := range f.Variables {
		if len(cs) == 0 {
			return invalidFilter("variables")
		}
		field := embeddeddata.FindComputed(computed, key)
		if field == nil {
			continue
		}
		for _, c := range cs {
			condition, ok := typedCondition(jsonPath{"variables", []string{key}}, c, field.DataType)
			if !ok {
				return invalidFilter("variables")
			}
			w.add(condition)
		}
	}
	return nil
}

func dataCondition(w *clause, sv *Survey, key string, c Condition) error {
	p := jsonPath{"data", []string{key}}
	value := func(kinds ...string) (any, error) {
		v, ok := jsonValue(c.Value, kinds...)
		if !ok {
			return nil, invalidFilter("data")
		}
		return v, nil
	}
	switch c.Op {
	case "submitted":
		w.add(q(p, " IS NOT NULL"))
	case "filledOut":
		w.add(q(p, " <> '[]'::jsonb"))
	case "skipped":
		w.add(q("(", p, " IS NULL OR ", p, ` = '""'::jsonb OR `, p, " = '[]'::jsonb)"))
	case "equals":
		v, err := value("string", "number", "boolean")
		if err != nil {
			return err
		}
		w.add(q(dataContains(p.keys, v), " AND ", p, " = ", jsonb(v), "::jsonb"))
	case "notEquals":
		v, err := value("string", "number", "boolean")
		if err != nil {
			return err
		}
		w.add(q("(", p, " <> ", jsonb(v), "::jsonb OR ", p, " IS NULL)"))
	case "lessThan", "lessEqual", "greaterThan", "greaterEqual":
		v, err := value("string", "number")
		if err != nil {
			return err
		}
		w.add(comparison(p, c.Op, v))
	case "includesAll":
		v, err := value("array")
		if err != nil {
			return err
		}
		w.add(includesAll(sv, key, v.([]any)))
	case "includesOne":
		v, err := value("array")
		if err != nil {
			return err
		}
		w.add(includesOne(sv, key, v.([]any)))
	case "uploaded":
		w.add(q(p, ` <> '"skipped"'::jsonb`))
	case "notUploaded":
		w.add(q("(", p, ` = '"skipped"'::jsonb OR `, p, " IS NULL)"))
	case "clicked", "accepted", "booked":
		w.add(q(dataContains(p.keys, c.Op), " AND ", p, " = ", jsonb(c.Op), "::jsonb"))
	case "matrix":
		var rows map[string]string
		if json.Unmarshal(c.Value, &rows) != nil || len(rows) == 0 {
			return invalidFilter("data")
		}
		// forma2 filtered on the condition's first row alone; the views send one row per condition.
		keys := make([]string, 0, len(rows))
		for k := range rows {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		w.add(matrixCell(sv, key, keys[0], rows[keys[0]]))
	case "contains", "doesNotContain", "startsWith", "doesNotStartWith", "endsWith", "doesNotEndWith":
		v, err := value("string")
		if err != nil {
			return err
		}
		w.add(textCondition(p, c.Op, v.(string)))
	case "isCompletelySubmitted", "isPartiallySubmitted", "isEmpty", "isNotEmpty", "isAnyOf":
		// Part of the filter grammar, but forma2's buildWhereClause had no branch for them: they filter nothing.
	default:
		return invalidFilter("data")
	}
	return nil
}

// elementByID is the survey's element answering under key, nil when there is none.
func elementByID(sv *Survey, key string) *surveydoc.Element {
	for _, e := range sv.Elements() {
		if e.ID == key {
			return &e
		}
	}
	return nil
}

func isChoiceElement(el *surveydoc.Element) bool {
	return el != nil && (el.Type == surveydoc.ElementMultipleChoiceMulti || el.Type == surveydoc.ElementMultipleChoiceSingle)
}

// languageCodes are the languages a label is written in: "default" first, then the others in code order, so the SQL a filter compiles to is the same on every run.
func languageCodes(l surveydoc.I18nString) []string {
	codes := make([]string, 0, len(l))
	for code := range l {
		if code != "default" {
			codes = append(codes, code)
		}
	}
	slices.Sort(codes)
	return append([]string{"default"}, codes...)
}

// labelVariants are a label's texts in every language it is written in, each once and none empty.
func labelVariants(l surveydoc.I18nString) []string {
	var out []string
	for _, code := range languageCodes(l) {
		if s := l[code]; s != "" && !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out
}

// choiceByDefault is the choice, row or column whose default-language label is label, nil when there is none.
func choiceByDefault(choices []surveydoc.Choice, label string) *surveydoc.Choice {
	for i, c := range choices {
		if c.Label.Default() == label {
			return &choices[i]
		}
	}
	return nil
}

// answerVariants are the stored answers a filter value stands for on el. The views name a choice by its default-language label while a response holds the label of the language it was answered in, as the summary reads it, so a value naming a choice of a choice element stands for that choice's label in every language. Any other value stands for itself.
func answerVariants(el *surveydoc.Element, v any) []any {
	out := []any{v}
	s, isString := v.(string)
	if !isString || !isChoiceElement(el) {
		return out
	}
	if ch := choiceByDefault(el.Choices, s); ch != nil {
		for _, label := range labelVariants(ch.Label) {
			if !slices.Contains(out, any(label)) {
				out = append(out, label)
			}
		}
	}
	return out
}

// includesOne is forma2's includesOne, matching a chosen choice in every language. Choosing the "Other" choice of a choice element means "answered with something no predefined label covers": a multi-select answer that is not an ordering of distinct predefined labels, a single-select answer that is none of them, where the labels of the other chosen choices, in any language, count as covered by none. Only an answer counts, as the summary counts it: an absent, empty-string or empty-list answer is skipped, never Other, even when every label is chosen. Otherwise a value matches as an element of an array answer or as the whole answer.
func includesOne(sv *Survey, key string, values []any) *cond {
	p := jsonPath{"data", []string{key}}
	chosen := map[string]bool{}
	for _, v := range values {
		chosen[jsvalue.String(v)] = true
	}
	el := elementByID(sv, key)
	if isChoiceElement(el) {
		for _, ch := range el.Choices {
			if ch.ID != "other" || !chosen[ch.Label.Default()] {
				continue
			}
			list := q("ARRAY[")
			n := 0
			for _, choice := range el.Choices {
				if chosen[choice.Label.Default()] {
					continue
				}
				for _, label := range labelVariants(choice.Label) {
					if chosen[label] {
						continue
					}
					if n > 0 {
						list.w(", ")
					}
					list.w(jsonb(label), "::jsonb")
					n++
				}
			}
			list.w("]")
			// The negation of the skipped operator.
			answered := q("(", p, " IS NOT NULL AND ", p, ` <> '""'::jsonb AND `, p, " <> '[]'::jsonb)")
			if n == 0 {
				return answered
			}
			if el.Type == surveydoc.ElementMultipleChoiceMulti {
				return q("(", answered, " AND NOT (jsonb_typeof(", p, ") = 'array'",
					" AND NOT EXISTS (SELECT 1 FROM jsonb_array_elements(", p, ") e WHERE NOT (e.value = ANY (", list, ")))",
					" AND (SELECT count(*) = count(DISTINCT e.value) FROM jsonb_array_elements(", p, ") e)))")
			}
			return q("(", answered, " AND NOT (", p, " = ANY (", list, ")))")
		}
	}
	var answers []any
	for _, v := range values {
		for _, a := range answerVariants(el, v) {
			if !slices.Contains(answers, a) {
				answers = append(answers, a)
			}
		}
	}
	if len(answers) == 0 {
		return q("FALSE")
	}
	// An array answer holding v, or v itself: both are containments of response.data, which the GIN index answers.
	c := q("(")
	for i, v := range answers {
		if i > 0 {
			c.w(" OR ")
		}
		c.w(dataContains(p.keys, []any{v}), " OR ", dataContains(p.keys, v))
	}
	return c.w(")")
}

// includesAll is an array answer holding every value. A value naming a choice of a choice element holds when the answer has that choice in any language: an AND over the values of an OR over each one's labels, every term a containment of response.data, which the GIN index answers.
func includesAll(sv *Survey, key string, values []any) *cond {
	p := jsonPath{"data", []string{key}}
	el := elementByID(sv, key)
	groups := make([][]any, len(values))
	translated := false
	for i, v := range values {
		groups[i] = answerVariants(el, v)
		translated = translated || len(groups[i]) > 1
	}
	if !translated {
		return q(dataContains(p.keys, values), " AND ", p, " @> ", jsonb(values), "::jsonb")
	}
	c := q("(")
	for i, group := range groups {
		if i > 0 {
			c.w(" AND ")
		}
		c.w("(")
		for j, v := range group {
			if j > 0 {
				c.w(" OR ")
			}
			c.w(dataContains(p.keys, []any{v}))
		}
		c.w(")")
	}
	return c.w(")")
}

// matrixCell is a matrix answer giving column to row, both named by their default-language labels. A response keys its answer by the row label and gives the column label of the language it was answered in, as the summary reads it, so the cell matches under each language's pair of row and column labels; a row or column the element does not have matches only as named.
func matrixCell(sv *Survey, key, row, column string) *cond {
	type pair struct{ row, column string }
	pairs := []pair{{row, column}}
	if el := elementByID(sv, key); el != nil && el.Type == surveydoc.ElementMatrix {
		r, c := choiceByDefault(el.Rows, row), choiceByDefault(el.Columns, column)
		if r != nil && c != nil {
			for _, code := range languageCodes(r.Label) {
				lp := pair{r.Label[code], c.Label[code]}
				if lp.row != "" && lp.column != "" && !slices.Contains(pairs, lp) {
					pairs = append(pairs, lp)
				}
			}
		}
	}
	match := func(lp pair) *cond {
		cell := []string{key, lp.row}
		return q(dataContains(cell, lp.column), " AND ", jsonPath{"data", cell}, " = ", jsonb(lp.column), "::jsonb")
	}
	if len(pairs) == 1 {
		return match(pairs[0])
	}
	c := q("(")
	for i, lp := range pairs {
		if i > 0 {
			c.w(" OR ")
		}
		c.w("(", match(lp), ")")
	}
	return c.w(")")
}

// dataContains is `r.data @> {keys...: v}`, the containment the GIN index on responses.data (jsonb_path_ops) answers. Beside an exact path condition that implies it, it lets the planner narrow a large survey's responses through the index instead of reading every row.
func dataContains(keys []string, v any) *cond {
	doc := v
	for i := len(keys) - 1; i >= 0; i-- {
		doc = map[string]any{keys[i]: doc}
	}
	return q("r.data @> ", jsonb(doc), "::jsonb")
}
