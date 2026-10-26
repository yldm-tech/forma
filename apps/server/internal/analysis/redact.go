package analysis

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// The contact details an AI summary removes from answers before they leave for the AI provider. The patterns are deliberately broad: a stray order number redacted as a phone costs nothing, a phone number sent to a third party cannot be taken back.
var (
	emailPattern = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9\-]+(?:\.[A-Za-z0-9\-]+)*\.[A-Za-z]{2,}`)
	// phoneCandidate is a run of digits of any script (full-width ones typed with a CJK input method included) with the separators phone numbers are written with, the dashes, dots, spaces and brackets of those input methods included; isPhone decides whether it is one. RE2's \d and \s are ASCII only, hence \p{Nd} and \p{Zs}.
	phoneCandidate = regexp.MustCompile(`[+＋]?[(（]?\p{Nd}[\p{Nd}\s\p{Zs}().．\-‐‑‒–—―−ー－﹣ｰ（）]{5,}\p{Nd}`)
	// isoDate and yearLastDate are the 8-digit dates phoneCandidate also matches (2026-10-26, 26.10.2026, 10-26-2026), after normalizePhone.
	isoDate      = regexp.MustCompile(`^(\d{4})[.\-](\d{2})[.\-](\d{2})$`)
	yearLastDate = regexp.MustCompile(`^(\d{2})[.\-](\d{2})[.\-](\d{4})$`)
	// phoneParts are the space-separated parts of a candidate.
	phoneParts = regexp.MustCompile(`[^\s\p{Zs}]+`)
	// phoneDashes are the dashes an input method may write between the groups of a phone number, the katakana prolonged sound mark among them.
	phoneDashes = strings.NewReplacer("‐", "-", "‑", "-", "‒", "-", "–", "-", "—", "-", "―", "-", "−", "-", "ー", "-", "﹣", "-")
)

const (
	redactedEmail = "[email]"
	redactedPhone = "[phone]"
	// phoneMinDigits and phoneMaxDigits bound the digits of one phone number (E.164 allows at most 15).
	phoneMinDigits = 8
	phoneMaxDigits = 15
)

// redactContactDetails replaces email addresses and phone numbers in text with [email] and [phone]. A phone number is a candidate with 8 to 15 digits that is not a date; a longer candidate is several numbers written next to each other, and every part of it but the dates in it is redacted.
func redactContactDetails(text string) string {
	text = emailPattern.ReplaceAllString(text, redactedEmail)
	return phoneCandidate.ReplaceAllStringFunc(text, redactPhoneCandidate)
}

func redactPhoneCandidate(m string) string {
	n := countDigits(m)
	switch {
	case n < phoneMinDigits:
		return m
	case n <= phoneMaxDigits:
		if isDate(m) {
			return m
		}
		return redactedPhone
	}
	// Too long for one number: keep the dates among its space-separated parts, and redact each run of the other parts that holds enough digits to be a number.
	var out strings.Builder
	last, runStart, runEnd := 0, -1, -1
	flush := func() {
		if runStart < 0 {
			return
		}
		out.WriteString(m[last:runStart])
		if run := m[runStart:runEnd]; countDigits(run) >= phoneMinDigits {
			out.WriteString(redactedPhone)
		} else {
			out.WriteString(run)
		}
		last, runStart = runEnd, -1
	}
	for _, f := range phoneParts.FindAllStringIndex(m, -1) {
		if isDate(m[f[0]:f[1]]) {
			flush()
			continue
		}
		if runStart < 0 {
			runStart = f[0]
		}
		runEnd = f[1]
	}
	flush()
	out.WriteString(m[last:])
	return out.String()
}

// normalizePhone folds full-width digits, brackets and spaces to their ASCII forms (NFKC) and every dash to '-'.
func normalizePhone(s string) string {
	return phoneDashes.Replace(norm.NFKC.String(s))
}

func countDigits(s string) int {
	n := 0
	for _, r := range s {
		if unicode.IsDigit(r) {
			n++
		}
	}
	return n
}

// isDate reports whether s is a plausible calendar date: a year of 1900 to 2099 with a month of 1 to 12 and a day of 1 to 31, the year written first or last (day or month first). 0120-12-3456 is not one.
func isDate(s string) bool {
	s = normalizePhone(strings.TrimSpace(s))
	atoi := func(v string) int { n, _ := strconv.Atoi(v); return n }
	month := func(n int) bool { return n >= 1 && n <= 12 }
	day := func(n int) bool { return n >= 1 && n <= 31 }
	if g := isoDate.FindStringSubmatch(s); g != nil {
		y := atoi(g[1])
		return y >= 1900 && y <= 2099 && month(atoi(g[2])) && day(atoi(g[3]))
	}
	if g := yearLastDate.FindStringSubmatch(s); g != nil {
		y, a, b := atoi(g[3]), atoi(g[1]), atoi(g[2])
		return y >= 1900 && y <= 2099 && (day(a) && month(b) || month(a) && day(b))
	}
	return false
}
