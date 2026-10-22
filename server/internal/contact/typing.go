package contact

import (
	"regexp"
	"strings"
	"time"
)

// Attribute key data types (contact_attribute_keys.data_type).
const (
	TypeString = "string"
	TypeNumber = "number"
	TypeDate   = "date"
)

// Columns is one stored attribute value: exactly one of the three is set, as the contact_attributes check constraint requires. A number or date that does not parse is stored as Text, forma2's "graceful degradation" of a typed value it could not read; it then reads back verbatim.
type Columns struct {
	Text   *string
	Number *float64
	Date   *time.Time
}

func textColumns(s string) Columns    { return Columns{Text: &s} }
func numberColumns(f float64) Columns { return Columns{Number: &f} }
func dateColumns(t time.Time) Columns { t = t.UTC(); return Columns{Date: &t} }
func (c Columns) parsedAs(dataType string) bool {
	switch dataType {
	case TypeNumber:
		return c.Number != nil
	case TypeDate:
		return c.Date != nil
	}
	return true
}

// String is forma2's readAttributeValue: a number as String(n), a date as its ISO string, anything else as stored.
func (c Columns) String() string {
	switch {
	case c.Number != nil:
		return jsNumberString(*c.Number)
	case c.Date != nil:
		return jsISOString(*c.Date)
	case c.Text != nil:
		return *c.Text
	}
	return ""
}

var (
	// csvDatePrefix is the prefix detectAttributeDataType treats as a date: YYYY-MM-DD, YYYY/MM/DD, DD-MM-YYYY or MM/DD/YYYY.
	csvDatePrefix = regexp.MustCompile(`^([0-9]{4}[-/][0-9]{2}[-/][0-9]{2}|[0-9]{2}[-/][0-9]{2}[-/][0-9]{4})`)
	isoDatePrefix = regexp.MustCompile(`^[0-9]{4}[-/][0-9]{2}[-/][0-9]{2}`)
	// sdkISODate is the only date shape the SDK and the contact editor may send: what Date#toISOString produces, or its date part.
	sdkISODate        = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}(T[0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]{3})?Z?)?$`)
	datePartSeparator = regexp.MustCompile(`[-/]`)
)

// dateFromParts is forma2's parseDateFromParts, in its order: DD-MM-YYYY when the first part exceeds 12, MM-DD-YYYY when the second does, and MM-DD-YYYY otherwise. Its YYYY-MM-DD branch came after the first test and so never ran; a year-first value only reaches here without two-digit parts ("2024-1-5"), and is read day-first as forma2 read it.
func dateFromParts(p1, p2, p3 int) time.Time {
	switch {
	case p1 > 12:
		return jsLocalDate(p3, p2-1, p1)
	case p2 > 12:
		return jsLocalDate(p3, p1-1, p2)
	}
	return jsLocalDate(p3, p1-1, p2)
}

// tryParseDate is forma2's tryParseDate: an ISO-like prefix is parsed as a whole by `new Date`, other values are split on - and / into day, month and year by dateFromParts. ok is false when it gives no date or an Invalid Date.
func tryParseDate(s string) (time.Time, bool) {
	if isoDatePrefix.MatchString(s) {
		return jsDate(s)
	}
	parts := datePartSeparator.Split(s, -1)
	if len(parts) < 3 {
		return time.Time{}, false
	}
	var n [3]int
	for i := range 3 {
		v, ok := jsParseInt(parts[i])
		if !ok {
			return time.Time{}, false
		}
		n[i] = v
	}
	return dateFromParts(n[0], n[1], n[2]), true
}

// detectCSVType is forma2's detectAttributeDataType, the type a CSV import gives a new key from its first value: a recognised date, then anything Number() reads, then string.
func detectCSVType(value string) string {
	s := jsTrim(value)
	if csvDatePrefix.MatchString(s) {
		if _, ok := tryParseDate(s); ok {
			return TypeDate
		}
	}
	if s != "" {
		if _, ok := jsNumber(s); ok {
			return TypeNumber
		}
	}
	return TypeString
}

// Value is an attribute value as a client sends it: a string, or a number when Number is set. Booleans arrive converted to "true" and "false".
type Value struct {
	String   string
	Number   float64
	IsNumber bool
}

// Text is the value as JavaScript's String() prints it.
func (v Value) Text() string {
	if v.IsNumber {
		return jsNumberString(v.Number)
	}
	return v.String
}

// detectSDKType is forma2's detectSDKAttributeDataType, the type a key created by the SDK or the contact editor gets: a number is a number, an ISO date string is a date, and every other string is a string, numeric-looking or not.
func detectSDKType(v Value) string {
	if v.IsNumber {
		return TypeNumber
	}
	s := jsTrim(v.String)
	if sdkISODate.MatchString(s) {
		if _, ok := jsDate(s); ok {
			return TypeDate
		}
	}
	return TypeString
}

// prepareColumns is forma2's prepareAttributeColumnsForStorage for a value of a known type; a number or date that does not parse degrades to text.
func prepareColumns(v Value, dataType string) Columns {
	switch dataType {
	case TypeNumber:
		if v.IsNumber {
			return numberColumns(v.Number)
		}
		if f, ok := jsNumber(v.String); ok {
			return numberColumns(f)
		}
		return textColumns(v.String)
	case TypeDate:
		if v.IsNumber {
			if t, ok := msDate(v.Number); ok {
				return dateColumns(t)
			}
			return textColumns(v.Text())
		}
		if t, ok := tryParseDate(jsTrim(v.String)); ok {
			return dateColumns(t)
		}
		if t, ok := jsDate(v.String); ok {
			return dateColumns(t)
		}
		return textColumns(v.String)
	}
	return textColumns(v.Text())
}

// msDate is `new Date(ms)`: valid within ±8.64e15 milliseconds of the epoch.
func msDate(ms float64) (time.Time, bool) {
	if ms != ms || ms > 8.64e15 || ms < -8.64e15 {
		return time.Time{}, false
	}
	return time.UnixMilli(int64(ms)).UTC(), true
}

// prepareCSVColumns is how the CSV import stores a cell. It is prepareColumns, except that an empty cell of a number or date key is stored as empty text: forma2 stored Number("") there, a 0 nobody wrote.
func prepareCSVColumns(value, dataType string) Columns {
	if dataType != TypeString && jsTrim(value) == "" {
		return textColumns(value)
	}
	return prepareColumns(Value{String: value}, dataType)
}

// TypeError is a value refused by the strict check for an existing key; Code and Params feed the client's translation, Message is forma2's English text.
type TypeError struct {
	Code    string            `json:"code"`
	Params  map[string]string `json:"params"`
	Message string            `json:"-"`
}

// validationTemplates are forma2's VALIDATION_ERROR_TEMPLATES (apps/web/modules/contacts/lib/validate-attribute-type.ts).
var validationTemplates = map[string]string{
	"string_type_mismatch": "Attribute '{key}' expects a string but received a {type}. Pass an actual string value.",
	"number_type_mismatch": "Attribute '{key}' expects a number but received a string. Pass an actual number value (e.g., 123 instead of \"123\").",
	"date_invalid":         "Attribute '{key}' expects a valid date. Received: Invalid Date",
	"date_format_invalid":  "Attribute '{key}' expects a date in ISO 8601 format (e.g., \"2024-01-15\" or \"2024-01-15T10:30:00.000Z\") or a Date object. Received: \"{value}\"",
	"date_unexpected_type": "Attribute '{key}' expects a valid date. Received: {type} value '{value}'",
}

func fillTemplate(template string, params map[string]string) string {
	for k, v := range params {
		template = strings.ReplaceAll(template, "{"+k+"}", v)
	}
	return template
}

func typeError(code string, params map[string]string) *TypeError {
	t, ok := validationTemplates[code]
	if !ok {
		t = code
	}
	return &TypeError{Code: code, Params: params, Message: fillTemplate(t, params)}
}

// validateValue is forma2's validateAndParseAttributeValue, the strict check for writing to an existing key: a string key takes only strings, a number key only numbers, and a date key only an ISO 8601 string.
func validateValue(v Value, dataType, key string) (Columns, *TypeError) {
	switch dataType {
	case TypeNumber:
		if v.IsNumber {
			return numberColumns(v.Number), nil
		}
		return Columns{}, typeError("number_type_mismatch", map[string]string{"key": key})
	case TypeDate:
		if v.IsNumber {
			return Columns{}, typeError("date_unexpected_type", map[string]string{"key": key, "type": "number", "value": v.Text()})
		}
		s := jsTrim(v.String)
		if sdkISODate.MatchString(s) {
			if t, ok := jsDate(s); ok {
				return dateColumns(t), nil
			}
		}
		return Columns{}, typeError("date_format_invalid", map[string]string{"key": key, "value": s})
	}
	if v.IsNumber {
		return Columns{}, typeError("string_type_mismatch", map[string]string{"key": key, "type": "number"})
	}
	return textColumns(v.String), nil
}
