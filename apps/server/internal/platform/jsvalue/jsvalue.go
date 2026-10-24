// Package jsvalue reproduces the JavaScript semantics forma2's rules were written in, for the values those rules read: JSON decoded by encoding/json (string, float64, bool, nil, []any, map[string]any) plus Undefined, JavaScript's absent value. Number(), String(), === and `new Date(s)` behave here as they did in forma2's Node.js server, which ran in UTC, so JavaScript's local time is UTC.
package jsvalue

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type undefined struct{}

// Undefined is JavaScript's undefined: a key that is absent, as opposed to one holding null.
var Undefined any = undefined{}

// IsNullish is `v === undefined || v === null`.
func IsNullish(v any) bool { return v == nil || v == Undefined }

// IsWhitespace is the set String.prototype.trim removes: Unicode white space and line terminators, plus the BOM.
func IsWhitespace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', '\u00a0', '\u1680', '\u2028', '\u2029', '\u202f', '\u205f', '\u3000', '\ufeff':
		return true
	}
	return r >= '\u2000' && r <= '\u200a'
}

// Trim is String.prototype.trim.
func Trim(s string) string {
	return strings.TrimFunc(s, IsWhitespace)
}

var decimal = regexp.MustCompile(`^[+-]?(?:[0-9]+\.?[0-9]*|\.[0-9]+)(?:[eE][+-]?[0-9]+)?$`)

// ParseNumber is Number(s) for a string: surrounding whitespace is ignored, "" is 0, and decimal, exponent, Infinity and 0x/0o/0b literals parse. It is NaN where Number() is.
func ParseNumber(s string) float64 {
	s = Trim(s)
	switch s {
	case "":
		return 0
	case "Infinity", "+Infinity":
		return math.Inf(1)
	case "-Infinity":
		return math.Inf(-1)
	}
	if len(s) > 2 && s[0] == '0' {
		base := 0
		switch s[1] {
		case 'x', 'X':
			base = 16
		case 'o', 'O':
			base = 8
		case 'b', 'B':
			base = 2
		}
		if base != 0 {
			n, err := strconv.ParseUint(s[2:], base, 64)
			if err != nil {
				return math.NaN()
			}
			return float64(n)
		}
	}
	if !decimal.MatchString(s) {
		return math.NaN()
	}
	f, err := strconv.ParseFloat(s, 64)
	// Out of range is not an error: ParseFloat then returns ±Inf or ±0, as JavaScript does.
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		return math.NaN()
	}
	return f
}

// Number is JavaScript's Number(v).
func Number(v any) float64 {
	switch x := v.(type) {
	case nil:
		return 0
	case bool:
		if x {
			return 1
		}
		return 0
	case float64:
		return x
	case string:
		return ParseNumber(x)
	case []any:
		// An array converts through its string form: [] is 0, ["5"] is 5, anything longer is NaN.
		return ParseNumber(String(x))
	}
	return math.NaN()
}

// FormatNumber is JavaScript's String(n).
func FormatNumber(f float64) string {
	switch {
	case math.IsNaN(f):
		return "NaN"
	case math.IsInf(f, 1):
		return "Infinity"
	case math.IsInf(f, -1):
		return "-Infinity"
	case f == 0:
		return "0"
	}
	if a := math.Abs(f); a >= 1e-6 && a < 1e21 {
		return strconv.FormatFloat(f, 'f', -1, 64)
	}
	s := strconv.FormatFloat(f, 'e', -1, 64)
	mantissa, exp, _ := strings.Cut(s, "e")
	sign := exp[0]
	digits := strings.TrimLeft(exp[1:], "0")
	return mantissa + "e" + string(sign) + digits
}

// String is JavaScript's String(v).
func String(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case bool:
		return strconv.FormatBool(x)
	case float64:
		return FormatNumber(x)
	case string:
		return x
	case []any:
		parts := make([]string, len(x))
		for i, e := range x {
			if !IsNullish(e) {
				parts[i] = String(e)
			}
		}
		return strings.Join(parts, ",")
	case map[string]any:
		return "[object Object]"
	}
	return "undefined"
}

// StrictEqual is JavaScript's ===. Arrays and objects are equal only to themselves, which for decoded JSON means the same slice or map.
func StrictEqual(a, b any) bool {
	switch x := a.(type) {
	case []any, map[string]any:
		if reflect.TypeOf(a) != reflect.TypeOf(b) {
			return false
		}
		return reflect.ValueOf(x).Pointer() == reflect.ValueOf(b).Pointer() && reflect.ValueOf(x).Len() == reflect.ValueOf(b).Len()
	case float64:
		y, ok := b.(float64)
		return ok && x == y
	}
	switch b.(type) {
	case []any, map[string]any:
		return false
	}
	return a == b
}

// isoDateTime is the ECMAScript date time string format: a date, optionally a time, optionally an offset. A date alone is UTC; a time without an offset is local time.
var isoDateTime = regexp.MustCompile(`^([+-][0-9]{6}|[0-9]{4})(?:-([0-9]{2})(?:-([0-9]{2}))?)?(?:T([0-9]{2}):([0-9]{2})(?::([0-9]{2})(?:\.([0-9]+))?)?(Z|[+-][0-9]{2}:[0-9]{2})?)?$`)

// legacyNumeric covers the slash and space separated forms V8's fallback parser reads as local time: 2024/01/15, 2024-1-5, 2024-01-15 10:30.
var legacyNumeric = regexp.MustCompile(`^([0-9]{4})[-/]([0-9]{1,2})[-/]([0-9]{1,2})(?:[ T]([0-9]{1,2}):([0-9]{2})(?::([0-9]{2})(?:\.([0-9]+))?)?)?\s*(Z|UTC|GMT)?$`)

// legacyMonthFirst and legacyDayFirst cover "Jan 15, 2024", "January 15 2024 10:30", "15 January 2024" and "15 Jan 2024".
var (
	legacyMonthFirst = regexp.MustCompile(`^([A-Za-z]{3,9})\.? ([0-9]{1,2}),? ([0-9]{4})(?: ([0-9]{1,2}):([0-9]{2})(?::([0-9]{2}))?)?$`)
	legacyDayFirst   = regexp.MustCompile(`^([0-9]{1,2}) ([A-Za-z]{3,9})\.?,? ([0-9]{4})(?: ([0-9]{1,2}):([0-9]{2})(?::([0-9]{2}))?)?$`)
)

var monthNames = map[string]int{
	"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6, "jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12,
}

// ParseDate is `new Date(s)` for the formats forma2's inputs used: the ECMAScript format exactly as V8 reads it (months 1-12, days 1-31 rolled over into the next month, hour 24 only as midnight), and the common slash, space and month-name forms of its fallback parser. ok is false for an Invalid Date. Times are truncated to milliseconds, as a JavaScript Date holds them.
func ParseDate(s string) (time.Time, bool) {
	s = Trim(s)
	if m := isoDateTime.FindStringSubmatch(s); m != nil {
		return isoDate(m)
	}
	if m := legacyNumeric.FindStringSubmatch(s); m != nil {
		return civilDate(atoi(m[1]), atoi(m[2]), atoi(m[3]), m[4], m[5], m[6], m[7])
	}
	if m := legacyMonthFirst.FindStringSubmatch(s); m != nil {
		month, ok := monthNumber(m[1])
		if !ok {
			return time.Time{}, false
		}
		return civilDate(atoi(m[3]), month, atoi(m[2]), m[4], m[5], m[6], "")
	}
	if m := legacyDayFirst.FindStringSubmatch(s); m != nil {
		month, ok := monthNumber(m[2])
		if !ok {
			return time.Time{}, false
		}
		return civilDate(atoi(m[3]), month, atoi(m[1]), m[4], m[5], m[6], "")
	}
	return time.Time{}, false
}

func monthNumber(name string) (int, bool) {
	lower := strings.ToLower(name)
	n, ok := monthNames[lower[:3]]
	if !ok {
		return 0, false
	}
	full := []string{"january", "february", "march", "april", "may", "june", "july", "august", "september", "october", "november", "december"}[n-1]
	// V8 accepts the three-letter abbreviation or any longer prefix of the full name.
	return n, strings.HasPrefix(full, lower)
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

func isoDate(m []string) (time.Time, bool) {
	year := atoi(strings.TrimPrefix(m[1], "+"))
	if m[1] == "-000000" {
		return time.Time{}, false
	}
	month, day := 1, 1
	if m[2] != "" {
		month = atoi(m[2])
	}
	if m[3] != "" {
		day = atoi(m[3])
	}
	if m[4] == "" {
		if month < 1 || month > 12 || day < 1 || day > 31 {
			return time.Time{}, false
		}
		return time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC), true
	}
	t, ok := civilDate(year, month, day, m[4], m[5], m[6], m[7])
	if !ok || m[8] == "" || m[8] == "Z" {
		return t, ok
	}
	sign := 1
	if m[8][0] == '-' {
		sign = -1
	}
	oh, om := atoi(m[8][1:3]), atoi(m[8][4:6])
	if oh > 23 || om > 59 {
		return time.Time{}, false
	}
	return t.Add(-time.Duration(sign) * (time.Duration(oh)*time.Hour + time.Duration(om)*time.Minute)), true
}

// civilDate builds a UTC instant from calendar fields as V8 validates them; empty time fields are zero.
func civilDate(year, month, day int, hh, mm, ss, frac string) (time.Time, bool) {
	hour, minute, second := atoi(hh), atoi(mm), atoi(ss)
	ms := 0
	if frac != "" {
		ms = atoi((frac + "00")[:3])
	}
	if month < 1 || month > 12 || day < 1 || day > 31 || minute > 59 || second > 59 {
		return time.Time{}, false
	}
	if hour > 24 || (hour == 24 && (minute != 0 || second != 0 || ms != 0)) {
		return time.Time{}, false
	}
	return time.Date(year, time.Month(month), day, hour, minute, second, ms*int(time.Millisecond), time.UTC), true
}

// ISOString is Date#toISOString.
func ISOString(t time.Time) string {
	t = t.UTC()
	year := t.Year()
	var y string
	switch {
	case year >= 0 && year <= 9999:
		y = fmt.Sprintf("%04d", year)
	case year < 0:
		y = fmt.Sprintf("-%06d", -year)
	default:
		y = fmt.Sprintf("+%06d", year)
	}
	return y + t.Format("-01-02T15:04:05.000Z")
}

// Member is one property of a JSON object.
type Member struct {
	Key   string
	Value any
}

// DecodeObject decodes a JSON object into its properties in the order Object.keys enumerates them after JSON.parse: integer keys ascending, then the rest in the order they first appeared. A repeated key keeps its first position and its last value, as JSON.parse does.
func DecodeObject(raw []byte) ([]Member, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, errors.New("not a JSON object")
	}
	var members []Member
	index := map[string]int{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, _ := tok.(string)
		var v any
		if err := dec.Decode(&v); err != nil {
			return nil, err
		}
		if i, seen := index[key]; seen {
			members[i].Value = v
			continue
		}
		index[key] = len(members)
		members = append(members, Member{Key: key, Value: v})
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	sort.SliceStable(members, func(i, j int) bool {
		a, aIndex := arrayIndex(members[i].Key)
		b, bIndex := arrayIndex(members[j].Key)
		switch {
		case aIndex && bIndex:
			return a < b
		default:
			return aIndex && !bIndex
		}
	})
	return members, nil
}

// arrayIndex reports whether key is an array index as JavaScript orders object keys: the canonical decimal form of an integer below 2^32 - 1.
func arrayIndex(key string) (uint64, bool) {
	if key == "" || (len(key) > 1 && key[0] == '0') {
		return 0, false
	}
	n, err := strconv.ParseUint(key, 10, 64)
	if err != nil || n >= 1<<32-1 {
		return 0, false
	}
	return n, true
}
