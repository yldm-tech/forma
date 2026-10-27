package analysis

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// The contact details an AI summary removes from answers before they leave for the AI provider. The patterns are deliberately broad: a stray order number redacted as a phone costs nothing, a phone number sent to a third party cannot be taken back.
var (
	// latinEmailPattern is an address in the Latin script, ASCII or with diacritics (jörg@bücher.de), the usual kind. It runs first so that such an address written into Japanese or Chinese text without spaces is redacted alone, not with the words around it, which unicodeEmailPattern would take for part of an internationalized address.
	latinEmailPattern = regexp.MustCompile(`[\p{Latin}\p{M}0-9._%+\-]+@[\p{Latin}\p{M}0-9\-]+(?:\.[\p{Latin}\p{M}0-9\-]+)*\.[\p{Latin}\p{M}]{2,}`)
	// unicodeEmailPattern is an internationalized address (EAI, IDN): letters, marks and digits of any script in the local part, the domain and the top-level domain, as in 张三@例子.公司. Without spaces around it, it also takes the words it is written between; that costs some context, never an address.
	unicodeEmailPattern = regexp.MustCompile(`[\p{L}\p{M}\p{N}._%+\-]+@[\p{L}\p{M}\p{N}\-]+(?:\.[\p{L}\p{M}\p{N}\-]+)*\.[\p{L}\p{M}]{2,}`)
	// phoneCandidate is a run of digits of any script (full-width ones typed with a CJK input method included) with the separators phone numbers are written with, the dashes, dots, spaces and brackets of those input methods included; redactPhoneCandidate decides whether it is one. RE2's \d and \s are ASCII only, hence \p{Nd} and \p{Zs}.
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
	text = redactFolded(text, latinEmailPattern)
	text = redactFolded(text, unicodeEmailPattern)
	return phoneCandidate.ReplaceAllStringFunc(text, redactPhoneCandidate)
}

// redactFolded replaces every address the email pattern finds in text folded with NFKC, rune by rune, with [email]. The fold finds the full-width ＠, letters and digits a Japanese or Chinese input method types (ｔａｒｏ＠ｅｘａｍｐｌｅ．ｃｏｍ) like their ASCII forms; the replacement is made in text as written, over every rune a match was folded from, so the rest of the answer, quoted back to the people who ran the survey, keeps its full-width forms. (phoneCandidate needs no fold: it matches full-width digits and separators as they are.)
func redactFolded(text string, pattern *regexp.Regexp) string {
	var b strings.Builder
	// from and to hold, for every byte of the folded text, the bounds in text of the rune it was folded from.
	from := make([]int, 0, len(text))
	to := make([]int, 0, len(text))
	for i := 0; i < len(text); {
		_, size := utf8.DecodeRuneInString(text[i:])
		f := norm.NFKC.String(text[i : i+size])
		b.WriteString(f)
		for range len(f) {
			from = append(from, i)
			to = append(to, i+size)
		}
		i += size
	}
	matches := pattern.FindAllStringIndex(b.String(), -1)
	if len(matches) == 0 {
		return text
	}
	var out strings.Builder
	last := 0
	for _, m := range matches {
		start, end := from[m[0]], to[m[1]-1]
		// Two matches folded from one rune are one address.
		if start >= last {
			out.WriteString(text[last:start])
			out.WriteString(redactedEmail)
		}
		last = max(last, end)
	}
	out.WriteString(text[last:])
	return out.String()
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
