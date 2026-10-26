package answers

import (
	"math"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode/utf16"

	"golang.org/x/text/unicode/norm"

	"github.com/yldm-tech/forma/apps/server/internal/platform/jsvalue"
)

// validator is one rule type of the runtime's validators registry: whether a value passes, and the message when it does not.
type validator struct {
	check   func(c checker, value any, params map[string]any, e *element) bool
	message func(c checker, params map[string]any, e *element) string
}

// jsLength is String.prototype.length: UTF-16 code units.
func jsLength(s string) int {
	return len(utf16.Encode([]rune(s)))
}

// num reads a numeric parameter; ok is false when it is absent or not a number, where JavaScript compared with undefined and every comparison failed.
func num(params map[string]any, key string) (float64, bool) {
	f, ok := params[key].(float64)
	return f, ok
}

// str reads a string parameter.
func str(params map[string]any, key string) (string, bool) {
	s, ok := params[key].(string)
	return s, ok
}

// arg is a message parameter as the runtime's i18n printed it: a number in JavaScript's String() form, a string as it is.
func arg(params map[string]any, key string) string {
	switch v := params[key].(type) {
	case float64:
		return jsvalue.FormatNumber(v)
	case string:
		return v
	case nil:
		return "undefined"
	}
	return jsvalue.String(params[key])
}

// floatPrefix is the longest number Number.parseFloat reads from the start of a string.
var floatPrefix = regexp.MustCompile(`^[+-]?(?:Infinity|(?:[0-9]+\.?[0-9]*|\.[0-9]+)(?:[eE][+-]?[0-9]+)?)`)

// parseFloatPrefix is Number.parseFloat: the longest decimal prefix after leading white space, NaN when there is none.
func parseFloatPrefix(s string) float64 {
	s = strings.TrimLeftFunc(s, jsvalue.IsWhitespace)
	m := floatPrefix.FindString(s)
	if m == "" {
		return math.NaN()
	}
	return jsvalue.ParseNumber(m)
}

// numeric is the runtime's parseNumericValue: a number, or a string's float prefix; ok is false when there is none.
func numeric(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case string:
		f := parseFloatPrefix(x)
		return f, !math.IsNaN(f)
	}
	return 0, false
}

// nonEmptyString is the guard most text rules start with: only a non-empty string is checked.
func nonEmptyString(v any) (string, bool) {
	s, ok := v.(string)
	return s, ok && s != ""
}

// countSelections is the runtime's countSelections: a multi-select answer's choices, where the "" sentinel and the "Other" text after it count as one.
func countSelections(arr []any) int {
	sentinel := false
	count := 0
	for i, item := range arr {
		if s, ok := item.(string); ok && s == "" {
			sentinel = true
			continue
		}
		if i > 0 {
			if prev, ok := arr[i-1].(string); ok && prev == "" {
				continue
			}
		}
		count++
	}
	if sentinel {
		count++
	}
	return count
}

var (
	emailPattern = regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
	// phoneDigits is what a phone number must be once its separators are gone: an optional leading + and 7 to 15 digits of any script (E.164 allows at most 15).
	phoneDigits = regexp.MustCompile(`^\+?\p{Nd}{7,15}$`)
)

// phoneSeparators are the characters people write between the digits of a phone number, after NFKC (which already folds full-width forms and the ideographic space): space, - . ( ) /, the Unicode hyphens and dashes U+2010 to U+2015, the minus sign, and the katakana prolonged sound mark a Japanese IME types for a hyphen. The runtime's validatePhone (packages/surveys/src/lib/validation/validators/validation-utils.ts) strips the same set.
const phoneSeparators = " -.()/\u2010\u2011\u2012\u2013\u2014\u2015\u2212\u30fc"

// isValidPhone is the runtime's validatePhone: NFKC-normalized, separators dropped, then an optional + and 7 to 15 digits. The answer itself is stored as typed. testdata/phone.json holds the cases both sides must agree on.
func isValidPhone(s string) bool {
	digits := strings.Map(func(r rune) rune {
		if strings.ContainsRune(phoneSeparators, r) {
			return -1
		}
		return r
	}, norm.NFKC.String(s))
	return phoneDigits.MatchString(digits)
}

// maxPatternLength and maxPatternInputLength are the runtime's ReDoS guards: a longer pattern or value fails the rule.
const (
	maxPatternLength      = 512
	maxPatternInputLength = 4096
)

// compilePattern is `new RegExp(pattern, flags)`. A pattern or flag JavaScript would refuse, or one Go's regexp cannot express (lookaround, backreferences), does not compile, and like an invalid pattern in the runtime the rule then passes.
func compilePattern(pattern, flags string) (*regexp.Regexp, bool) {
	prefix, sticky := "", false
	for _, f := range flags {
		switch f {
		case 'i', 'm', 's':
			if strings.ContainsRune(prefix, f) {
				return nil, false
			}
			prefix += string(f)
		case 'y':
			sticky = true
		case 'g', 'u', 'd', 'v':
		default:
			return nil, false
		}
	}
	expr := pattern
	if sticky {
		// test() of a fresh sticky regexp matches only at index 0.
		expr = `\A(?:` + expr + `)`
	}
	if prefix != "" {
		expr = "(?" + prefix + ")" + expr
	}
	re, err := regexp.Compile(expr)
	return re, err == nil
}

// isValidURL is `new URL(s)` succeeding: an absolute URL, with a host when its scheme is one of the WHATWG special schemes, which also take their slashes as optional ("http:example.com"). Surrounding spaces and control characters are ignored, as the URL parser strips them.
func isValidURL(raw string) bool {
	s := strings.TrimFunc(raw, func(r rune) bool { return r <= ' ' })
	u, err := url.Parse(s)
	if err != nil || u.Scheme == "" {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "ws", "wss", "ftp":
		if u.Host == "" && u.Opaque != "" {
			u, err = url.Parse(u.Scheme + "://" + strings.TrimLeft(u.Opaque, "/"))
			if err != nil {
				return false
			}
		}
		return u.Host != ""
	}
	return true
}

// validators is the runtime's registry, by rule type.
var validators = map[string]validator{
	"minLength": {
		check: func(_ checker, v any, p map[string]any, _ *element) bool {
			s, ok := nonEmptyString(v)
			if !ok {
				return true
			}
			min, ok := num(p, "min")
			return ok && float64(jsLength(s)) >= min
		},
		message: func(c checker, p map[string]any, _ *element) string {
			return c.t("min_length", map[string]string{"min": arg(p, "min")})
		},
	},
	"maxLength": {
		check: func(_ checker, v any, p map[string]any, _ *element) bool {
			s, ok := v.(string)
			if !ok {
				return true
			}
			max, ok := num(p, "max")
			return ok && float64(jsLength(s)) <= max
		},
		message: func(c checker, p map[string]any, _ *element) string {
			return c.t("max_length", map[string]string{"max": arg(p, "max")})
		},
	},
	"pattern": {
		check: func(_ checker, v any, p map[string]any, _ *element) bool {
			s, ok := nonEmptyString(v)
			if !ok {
				return true
			}
			pattern, _ := str(p, "pattern")
			if jsLength(pattern) > maxPatternLength || jsLength(s) > maxPatternInputLength {
				return false
			}
			flags, _ := str(p, "flags")
			re, ok := compilePattern(pattern, flags)
			if !ok {
				return true
			}
			return re.MatchString(s)
		},
		message: func(c checker, _ map[string]any, _ *element) string { return c.t("invalid_format", nil) },
	},
	"email": {
		check: func(_ checker, v any, _ map[string]any, _ *element) bool {
			s, ok := nonEmptyString(v)
			return !ok || emailPattern.MatchString(s)
		},
		message: func(c checker, _ map[string]any, _ *element) string {
			return c.t("please_enter_a_valid_email_address", nil)
		},
	},
	"url": {
		check: func(_ checker, v any, _ map[string]any, _ *element) bool {
			s, ok := nonEmptyString(v)
			return !ok || isValidURL(s)
		},
		message: func(c checker, _ map[string]any, _ *element) string { return c.t("please_enter_a_valid_url", nil) },
	},
	"phone": {
		check: func(_ checker, v any, _ map[string]any, _ *element) bool {
			s, ok := nonEmptyString(v)
			return !ok || isValidPhone(s)
		},
		message: func(c checker, _ map[string]any, _ *element) string {
			return c.t("invalid_phone_with_example", nil)
		},
	},
	"minValue":      numberRule("min_value", "min", func(n, bound float64) bool { return n >= bound }),
	"maxValue":      numberRule("max_value", "max", func(n, bound float64) bool { return n <= bound }),
	"isGreaterThan": numberRule("is_greater_than", "min", func(n, bound float64) bool { return n > bound }),
	"isLessThan":    numberRule("is_less_than", "max", func(n, bound float64) bool { return n < bound }),
	"minSelections": {
		check: func(_ checker, v any, p map[string]any, _ *element) bool {
			if isEmpty(v) {
				return true
			}
			arr, ok := v.([]any)
			if !ok {
				return false
			}
			min, ok := num(p, "min")
			return ok && float64(countSelections(arr)) >= min
		},
		message: func(c checker, p map[string]any, _ *element) string {
			return c.t("min_selections", map[string]string{"min": arg(p, "min")})
		},
	},
	"maxSelections": {
		check: func(_ checker, v any, p map[string]any, _ *element) bool {
			arr, ok := v.([]any)
			if !ok {
				return true
			}
			max, ok := num(p, "max")
			return ok && float64(countSelections(arr)) <= max
		},
		message: func(c checker, p map[string]any, _ *element) string {
			return c.t("max_selections", map[string]string{"max": arg(p, "max")})
		},
	},
	"equals":       textRule("value_must_equal", func(s, value string, has bool) bool { return has && s == value }),
	"doesNotEqual": textRule("value_must_not_equal", func(s, value string, has bool) bool { return !has || s != value }),
	// includes(undefined) searches for the text "undefined".
	"contains":       textRule("value_must_contain", func(s, value string, has bool) bool { return strings.Contains(s, orUndefined(value, has)) }),
	"doesNotContain": textRule("value_must_not_contain", func(s, value string, has bool) bool { return !strings.Contains(s, orUndefined(value, has)) }),
	"isLaterThan": {
		check: func(c checker, v any, p map[string]any, _ *element) bool {
			s, ok := nonEmptyString(v)
			if !ok {
				return true
			}
			// A date answer with a time of day is held to the rule's calendar days by its day alone.
			s = answerDay(s)
			// Relative bounds are inclusive, fixed ones exclusive.
			if bound, ok := relativeBound(p, "relative"); ok {
				return s >= resolveRelativeDate(bound, timezoneGrace(c.now, -1))
			}
			date, ok := str(p, "date")
			return ok && s > date
		},
		message: func(c checker, p map[string]any, _ *element) string {
			// The relative check is inclusive but the copy reads "later than", so it names the last day still refused.
			date := arg(p, "date")
			if bound, ok := relativeBound(p, "relative"); ok {
				date = shiftISODate(resolveRelativeDate(bound, c.now), -1)
			}
			return c.t("is_later_than", map[string]string{"date": date})
		},
	},
	"isEarlierThan": {
		check: func(c checker, v any, p map[string]any, _ *element) bool {
			s, ok := nonEmptyString(v)
			if !ok {
				return true
			}
			// A date answer with a time of day is held to the rule's calendar days by its day alone.
			s = answerDay(s)
			if bound, ok := relativeBound(p, "relative"); ok {
				return s <= resolveRelativeDate(bound, timezoneGrace(c.now, 1))
			}
			date, ok := str(p, "date")
			return ok && s < date
		},
		message: func(c checker, p map[string]any, _ *element) string {
			date := arg(p, "date")
			if bound, ok := relativeBound(p, "relative"); ok {
				date = shiftISODate(resolveRelativeDate(bound, c.now), 1)
			}
			return c.t("is_earlier_than", map[string]string{"date": date})
		},
	},
	"isBetween": {
		check: func(c checker, v any, p map[string]any, _ *element) bool {
			s, ok := nonEmptyString(v)
			if !ok {
				return true
			}
			// A date answer with a time of day is held to the rule's calendar days by its day alone.
			s = answerDay(s)
			if start, end, ok := relativeRange(p, c.now, graceInside); ok {
				return s >= start && s <= end
			}
			start, ok1 := str(p, "startDate")
			end, ok2 := str(p, "endDate")
			return ok1 && ok2 && s > start && s < end
		},
		message: func(c checker, p map[string]any, _ *element) string {
			return c.t("is_between", rangeArgs(p, c.now))
		},
	},
	"isNotBetween": {
		check: func(c checker, v any, p map[string]any, _ *element) bool {
			s, ok := nonEmptyString(v)
			if !ok {
				return true
			}
			// A date answer with a time of day is held to the rule's calendar days by its day alone.
			s = answerDay(s)
			if start, end, ok := relativeRange(p, c.now, graceOutside); ok {
				return s < start || s > end
			}
			start, ok1 := str(p, "startDate")
			end, ok2 := str(p, "endDate")
			return (ok1 && s < start) || (ok2 && s > end)
		},
		message: func(c checker, p map[string]any, _ *element) string {
			return c.t("is_not_between", rangeArgs(p, c.now))
		},
	},
	"minRanked": {
		check: func(_ checker, v any, p map[string]any, e *element) bool {
			arr, ok := v.([]any)
			if !ok || len(arr) == 0 || e.Type != typeRanking {
				return true
			}
			min, ok := num(p, "min")
			return ok && float64(len(arr)) >= min
		},
		message: func(c checker, p map[string]any, _ *element) string {
			return c.t("minimum_options_ranked", map[string]string{"min": arg(p, "min")})
		},
	},
	"rankAll": {
		check: func(_ checker, v any, _ map[string]any, e *element) bool {
			if e.Type != typeRanking {
				return true
			}
			arr, ok := v.([]any)
			if !ok || len(arr) == 0 {
				return true
			}
			return len(arr) == len(e.Choices)
		},
		message: func(c checker, _ map[string]any, _ *element) string { return c.t("all_options_must_be_ranked", nil) },
	},
	"minRowsAnswered": {
		check: func(_ checker, v any, p map[string]any, e *element) bool {
			obj, ok := v.(map[string]any)
			if !ok || e.Type != typeMatrix {
				return true
			}
			min, ok := num(p, "min")
			return ok && float64(answeredRows(obj)) >= min
		},
		message: func(c checker, p map[string]any, _ *element) string {
			return c.t("minimum_rows_answered", map[string]string{"min": arg(p, "min")})
		},
	},
	"answerAllRows": {
		check: func(_ checker, v any, _ map[string]any, e *element) bool {
			if e.Type != typeMatrix {
				return true
			}
			obj, ok := v.(map[string]any)
			if !ok {
				return true
			}
			return answeredRows(obj) == len(e.Rows)
		},
		message: func(c checker, _ map[string]any, _ *element) string { return c.t("all_rows_must_be_answered", nil) },
	},
	"fileExtensionIs": {
		check: func(_ checker, v any, p map[string]any, e *element) bool {
			return fileExtensions(v, p, e, func(ext string, listed bool) bool { return ext != "" && listed })
		},
		message: func(c checker, p map[string]any, _ *element) string {
			return c.t("file_extension_must_be", map[string]string{"extension": extensionList(p)})
		},
	},
	"fileExtensionIsNot": {
		check: func(_ checker, v any, p map[string]any, e *element) bool {
			return fileExtensions(v, p, e, func(ext string, listed bool) bool { return ext == "" || !listed })
		},
		message: func(c checker, p map[string]any, _ *element) string {
			return c.t("file_extension_must_not_be", map[string]string{"extension": extensionList(p)})
		},
	},
}

// numberRule is a comparison of a numeric answer with a bound; an empty or non-numeric answer is left to the other checks.
func numberRule(key, param string, ok func(n, bound float64) bool) validator {
	return validator{
		check: func(_ checker, v any, p map[string]any, _ *element) bool {
			if isEmpty(v) {
				return true
			}
			n, isNum := numeric(v)
			if !isNum {
				return true
			}
			bound, has := num(p, param)
			return has && ok(n, bound)
		},
		message: func(c checker, p map[string]any, _ *element) string {
			return c.t(key, map[string]string{param: arg(p, param)})
		},
	}
}

// textRule is a comparison of a non-empty text answer with the rule's value; has is false when the rule carries none, which JavaScript compared as undefined.
func textRule(key string, ok func(s, value string, has bool) bool) validator {
	return validator{
		check: func(_ checker, v any, p map[string]any, _ *element) bool {
			s, isText := nonEmptyString(v)
			if !isText {
				return true
			}
			value, has := str(p, "value")
			return ok(s, value, has)
		},
		message: func(c checker, p map[string]any, _ *element) string {
			return c.t(key, map[string]string{"value": arg(p, "value")})
		},
	}
}

func orUndefined(value string, has bool) string {
	if !has {
		return "undefined"
	}
	return value
}

// normalizedExtensions are the rule's extensions, each lowercased with a leading dot.
func normalizedExtensions(p map[string]any) []string {
	list, _ := p["extensions"].([]any)
	var out []string
	for _, x := range list {
		s := strings.ToLower(jsvalue.String(x))
		if !strings.HasPrefix(s, ".") {
			s = "." + s
		}
		out = append(out, s)
	}
	return out
}

// extensionList is the rule's extensions as its message lists them: each with a leading dot, case kept.
func extensionList(p map[string]any) string {
	list, _ := p["extensions"].([]any)
	parts := make([]string, len(list))
	for i, x := range list {
		s := jsvalue.String(x)
		if !strings.HasPrefix(s, ".") {
			s = "." + s
		}
		parts[i] = s
	}
	return strings.Join(parts, ", ")
}

// fileExtensions checks every uploaded file's extension, read from its URL's last segment before any query: accept says whether a file with that extension ("" for none) passes, given whether the rule lists it.
func fileExtensions(v any, p map[string]any, e *element, accept func(ext string, listed bool) bool) bool {
	if e.Type != typeFileUpload {
		return true
	}
	arr, ok := v.([]any)
	if !ok || len(arr) == 0 {
		return true
	}
	listed := normalizedExtensions(p)
	for _, item := range arr {
		u, ok := item.(string)
		if !ok {
			continue
		}
		path, _, _ := strings.Cut(u, "?")
		name := path[strings.LastIndex(path, "/")+1:]
		ext := ""
		if strings.Contains(name, ".") {
			ext = "." + strings.ToLower(name[strings.LastIndex(name, ".")+1:])
		}
		if !accept(ext, ext != "" && slices.Contains(listed, ext)) {
			return false
		}
	}
	return true
}
