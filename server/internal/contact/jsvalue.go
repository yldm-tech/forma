package contact

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// forma2 typed attribute values with JavaScript's Number() and Date parsing, and printed them with String(n) and Date#toISOString(). The helpers here reproduce those semantics for the inputs forma2 accepted, so a value is typed, stored and read back as it was. forma2 ran in UTC, so JavaScript's local time is UTC here.

var jsDecimal = regexp.MustCompile(`^[+-]?(?:[0-9]+\.?[0-9]*|\.[0-9]+)(?:[eE][+-]?[0-9]+)?$`)

// jsNumber is JavaScript's Number(s): surrounding whitespace is ignored, "" is 0, and decimal, exponent, Infinity and 0x/0o/0b literals parse. ok is false where Number() gives NaN.
func jsNumber(s string) (float64, bool) {
	s = strings.TrimFunc(s, isJSWhitespace)
	switch s {
	case "":
		return 0, true
	case "Infinity", "+Infinity":
		return math.Inf(1), true
	case "-Infinity":
		return math.Inf(-1), true
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
				return 0, false
			}
			return float64(n), true
		}
	}
	if !jsDecimal.MatchString(s) {
		return 0, false
	}
	f, err := strconv.ParseFloat(s, 64)
	// Out of range is not an error: ParseFloat then returns ±Inf or ±0, as JavaScript does.
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		return 0, false
	}
	return f, true
}

// isJSWhitespace is the set String.prototype.trim removes: Unicode white space and line terminators, plus the BOM.
func isJSWhitespace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', '\u00a0', '\u1680', '\u2028', '\u2029', '\u202f', '\u205f', '\u3000', '\ufeff':
		return true
	}
	return r >= '\u2000' && r <= '\u200a'
}

// jsTrim is String.prototype.trim.
func jsTrim(s string) string {
	return strings.TrimFunc(s, isJSWhitespace)
}

// jsNumberString is JavaScript's String(n).
func jsNumberString(f float64) string {
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

// isoDateTime is the ECMAScript date time string format: a date, optionally a time, optionally an offset. A date alone is UTC; a time without an offset is local time.
var isoDateTime = regexp.MustCompile(`^([+-][0-9]{6}|[0-9]{4})(?:-([0-9]{2})(?:-([0-9]{2}))?)?(?:T([0-9]{2}):([0-9]{2})(?::([0-9]{2})(?:\.([0-9]+))?)?(Z|[+-][0-9]{2}:[0-9]{2})?)?$`)

// legacyNumeric covers the slash and space separated forms V8's fallback parser reads as local time: 2024/01/15, 2024-1-5, 2024-01-15 10:30.
var legacyNumeric = regexp.MustCompile(`^([0-9]{4})[-/]([0-9]{1,2})[-/]([0-9]{1,2})(?:[ T]([0-9]{1,2}):([0-9]{2})(?::([0-9]{2})(?:\.([0-9]+))?)?)?\s*(Z|UTC|GMT)?$`)

// legacyMonthName covers "Jan 15, 2024", "January 15 2024 10:30", "15 January 2024" and "15 Jan 2024".
var (
	legacyMonthFirst = regexp.MustCompile(`^([A-Za-z]{3,9})\.? ([0-9]{1,2}),? ([0-9]{4})(?: ([0-9]{1,2}):([0-9]{2})(?::([0-9]{2}))?)?$`)
	legacyDayFirst   = regexp.MustCompile(`^([0-9]{1,2}) ([A-Za-z]{3,9})\.?,? ([0-9]{4})(?: ([0-9]{1,2}):([0-9]{2})(?::([0-9]{2}))?)?$`)
)

var monthNames = map[string]int{
	"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6, "jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12,
}

// jsDate is `new Date(s)` for the formats forma2's inputs used: the ECMAScript format exactly as V8 reads it (months 1-12, days 1-31 rolled over into the next month, hour 24 only as midnight), and the common slash, space and month-name forms of its fallback parser. ok is false for an Invalid Date. Times are truncated to milliseconds, as a JavaScript Date holds them.
func jsDate(s string) (time.Time, bool) {
	s = jsTrim(s)
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

// jsLocalDate is `new Date(year, monthIndex, day)`: fields roll over, and a year from 0 to 99 means 1900 to 1999.
func jsLocalDate(year, monthIndex, day int) time.Time {
	if year >= 0 && year <= 99 {
		year += 1900
	}
	return time.Date(year, time.Month(monthIndex+1), day, 0, 0, 0, 0, time.UTC)
}

// jsISOString is Date#toISOString.
func jsISOString(t time.Time) string {
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

// jsParseInt is parseInt(s, 10): leading whitespace, an optional sign, then as many digits as there are. ok is false for NaN.
func jsParseInt(s string) (int, bool) {
	s = strings.TrimLeftFunc(s, isJSWhitespace)
	neg := false
	if s != "" && (s[0] == '+' || s[0] == '-') {
		neg = s[0] == '-'
		s = s[1:]
	}
	end := 0
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0, false
	}
	n, err := strconv.Atoi(s[:end])
	if err != nil {
		return 0, false
	}
	if neg {
		n = -n
	}
	return n, true
}
