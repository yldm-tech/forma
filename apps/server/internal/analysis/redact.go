package analysis

import (
	"regexp"
	"strings"
)

// The contact details an AI summary removes from answers before they leave for the AI provider. The patterns are deliberately broad: a stray order number redacted as a phone costs nothing, a phone number sent to a third party cannot be taken back.
var (
	emailPattern = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9\-]+(?:\.[A-Za-z0-9\-]+)*\.[A-Za-z]{2,}`)
	// phoneCandidate is a run of digits with the separators phone numbers are written with; phoneDigits decides whether it is one.
	phoneCandidate = regexp.MustCompile(`\+?\(?\d[\d\s().\-]{5,}\d`)
	// dateLike is a date written with dots or dashes (2026-10-26, 26.10.2026), which phoneCandidate also matches.
	dateLike = regexp.MustCompile(`^\d{1,4}[.\-]\d{1,2}[.\-]\d{1,4}$`)
)

const (
	redactedEmail = "[email]"
	redactedPhone = "[phone]"
)

// redactContactDetails replaces email addresses and phone numbers in text with [email] and [phone]. A phone number is a candidate with 8 to 15 digits that is not a date.
func redactContactDetails(text string) string {
	text = emailPattern.ReplaceAllString(text, redactedEmail)
	return phoneCandidate.ReplaceAllStringFunc(text, func(m string) string {
		digits := 0
		for _, r := range m {
			if r >= '0' && r <= '9' {
				digits++
			}
		}
		if digits < 8 || digits > 15 || dateLike.MatchString(strings.TrimSpace(m)) {
			return m
		}
		return redactedPhone
	})
}
