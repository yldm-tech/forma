package responsemail

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/yldm-tech/forma/apps/server/internal/platform/jsvalue"
)

// recallToken is forma2's RECALL_TOKEN_PATTERN.
var recallToken = regexp.MustCompile(`#recall:([A-Za-z0-9_-]+)/fallback:([^#]*)#`)

// htmlEscaper is forma2's escapeHtml: the five characters that end text in element content or a quoted attribute.
var htmlEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#39;")

// ParseRecall is forma2's parseRecallInfo without the slash form and the per-element date formats (no caller here passed them): every recall token in text replaced, in one left-to-right pass over the original text, by the value it recalls, a variable before an answer. A date answer is shown in locale's long form, a list joined by ", " without its empty entries, and a token with no value shows its fallback ("nbsp" in it read as a space). With escape, each value is HTML-escaped as it is spliced in, so a respondent's answer can never become markup in an email body. A recalled value is never scanned for tokens itself.
func ParseRecall(text string, values, variables map[string]any, locale string, escape bool) string {
	matches := recallToken.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		return text
	}
	var b strings.Builder
	cursor := 0
	for _, m := range matches {
		b.WriteString(text[cursor:m[0]])
		cursor = m[1]
		id := text[m[2]:m[3]]
		fallback := strings.ReplaceAll(text[m[4]:m[5]], "nbsp", " ")
		var value any
		var found bool
		if v, ok := variables[id]; ok {
			value, found = v, true
		} else if v, ok := values[id]; ok {
			value, found = v, true
			switch x := v.(type) {
			case string:
				if formatted, ok := formatStoredDate(x, locale); ok {
					value = formatted
				}
			case []any:
				parts := []string{}
				for _, item := range x {
					if truthy(item) {
						parts = append(parts, jsvalue.String(item))
					}
				}
				value = strings.Join(parts, ", ")
			}
		}
		if !found || value == nil || value == "" {
			value = fallback
		}
		s := stringifyRecallValue(value)
		if escape {
			s = htmlEscaper.Replace(s)
		}
		b.WriteString(s)
	}
	b.WriteString(text[cursor:])
	return b.String()
}

// stringifyRecallValue is forma2's stringifyRecallValue: a record's values joined like a list, so an address or matrix answer never reads "[object Object]".
func stringifyRecallValue(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return jsvalue.FormatNumber(x)
	case []any:
		parts := []string{}
		for _, item := range x {
			if truthy(item) {
				parts = append(parts, jsvalue.String(item))
			}
		}
		return strings.Join(parts, ", ")
	case map[string]any:
		parts := []string{}
		// Object.values follows insertion order, which a decoded map has lost; sorted keys are stable.
		for _, k := range slices.Sorted(maps.Keys(x)) {
			if truthy(x[k]) {
				parts = append(parts, jsvalue.String(x[k]))
			}
		}
		return strings.Join(parts, ", ")
	case bool:
		return strconv.FormatBool(x)
	}
	return ""
}

// truthy is JavaScript's Boolean(v) for a JSON value.
func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case float64:
		return x != 0
	case string:
		return x != ""
	}
	return true
}

var (
	isoStoredDate    = regexp.MustCompile(`^(\d{4})-(\d{1,2})-(\d{1,2})$`)
	legacyStoredDate = regexp.MustCompile(`^(\d{1,2})-(\d{1,2})-(\d{4})$`)
)

// formatStoredDate is forma2's formatStoredDateForDisplay without an element format: a stored date answer (YYYY-M-D, or the legacy D-M-YYYY) in the locale's long form with its weekday, formatDateWithOrdinal.
func formatStoredDate(value, locale string) (string, bool) {
	var y, mo, d int
	if m := isoStoredDate.FindStringSubmatch(value); m != nil {
		y, mo, d = atoi(m[1]), atoi(m[2]), atoi(m[3])
	} else if m := legacyStoredDate.FindStringSubmatch(value); m != nil {
		d, mo, y = atoi(m[1]), atoi(m[2]), atoi(m[3])
	} else {
		return "", false
	}
	t := time.Date(y, time.Month(mo), d, 0, 0, 0, 0, time.UTC)
	if t.Year() != y || int(t.Month()) != mo || t.Day() != d {
		return "", false
	}
	return longDate(t, locale), true
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

var (
	japaneseWeekdays = [...]string{"日曜日", "月曜日", "火曜日", "水曜日", "木曜日", "金曜日", "土曜日"}
	chineseWeekdays  = [...]string{"星期日", "星期一", "星期二", "星期三", "星期四", "星期五", "星期六"}
)

// longDate is Intl.DateTimeFormat(locale, {weekday: "long", year: "numeric", month: "long", day: "numeric"}) for the locales the app ships; any other locale is written as en-US.
func longDate(t time.Time, locale string) string {
	switch locale {
	case "ja-JP":
		return fmt.Sprintf("%d年%d月%d日%s", t.Year(), t.Month(), t.Day(), japaneseWeekdays[t.Weekday()])
	case "zh-Hans-CN":
		return fmt.Sprintf("%d年%d月%d日%s", t.Year(), t.Month(), t.Day(), chineseWeekdays[t.Weekday()])
	case "zh-Hant-TW":
		return fmt.Sprintf("%d年%d月%d日 %s", t.Year(), t.Month(), t.Day(), chineseWeekdays[t.Weekday()])
	}
	return t.Format("Monday, January 2, 2006")
}
