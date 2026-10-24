package survey

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/yldm-tech/forma/apps/server/internal/langcode"
)

// defaultV3Language is forma2's DEFAULT_V3_SURVEY_LANGUAGE: a survey without languages is in en-US.
const defaultV3Language = "en-US"

var (
	v3LocalePattern     = regexp.MustCompile(`^[a-z]{2,3}(?:-[A-Z][a-z]{3})?-(?:[A-Z]{2}|\d{3})$`)
	v3ScriptOnlyPattern = regexp.MustCompile(`^[a-z]{2,3}-[A-Z][a-z]{3}$`)
	alnumSubtag         = regexp.MustCompile(`^[A-Za-z0-9]{1,8}$`)
	letters             = regexp.MustCompile(`^[A-Za-z]+$`)
	digits              = regexp.MustCompile(`^[0-9]+$`)
)

// canonicalTag is Intl.getCanonicalLocales for the tags these rules accept: "_" read as "-", the language lowercased, a four-letter script title-cased, a region uppercased. A tag that is not well formed is "".
func canonicalTag(value string) string {
	parts := strings.Split(strings.ReplaceAll(strings.TrimSpace(value), "_", "-"), "-")
	if len(parts) == 0 || !letters.MatchString(parts[0]) || len(parts[0]) < 2 || len(parts[0]) > 8 || len(parts[0]) == 4 {
		return ""
	}
	out := []string{strings.ToLower(parts[0])}
	for i, p := range parts[1:] {
		if !alnumSubtag.MatchString(p) {
			return ""
		}
		switch {
		case i == 0 && len(p) == 4 && letters.MatchString(p):
			out = append(out, strings.ToUpper(p[:1])+strings.ToLower(p[1:]))
		case len(p) == 2 && letters.MatchString(p), len(p) == 3 && digits.MatchString(p):
			out = append(out, strings.ToUpper(p))
		default:
			out = append(out, strings.ToLower(p))
		}
	}
	return strings.Join(out, "-")
}

// v3LocaleCode is forma2's normalizeV3SurveyLocaleCode: a region-qualified locale in canonical case, or "".
func v3LocaleCode(value string) string {
	if c := canonicalTag(value); v3LocalePattern.MatchString(c) {
		return c
	}
	return ""
}

// v3LanguageTag is forma2's normalizeV3SurveyLanguageTag: a region-qualified or script-only tag in canonical case, or "".
func v3LanguageTag(value string) string {
	if c := canonicalTag(value); v3LocalePattern.MatchString(c) || v3ScriptOnlyPattern.MatchString(c) {
		return c
	}
	return ""
}

func canonicalKey(value string) string { return strings.ToLower(langcode.Normalize(value)) }

// writeLanguageCode is forma2's normalizeV3SurveyWriteLanguageCode: a code naming one of the allowed (stored) languages, exactly or by canonical equivalence, is that stored code; otherwise a full locale, canonicalized; otherwise "".
func writeLanguageCode(value string, allowed []string) string {
	requested := strings.ToLower(strings.TrimSpace(value))
	requestedCanonical := canonicalKey(value)
	for _, a := range allowed {
		if requested == strings.ToLower(a) || (requestedCanonical != "" && requestedCanonical == canonicalKey(a)) {
			return a
		}
	}
	if locale := v3LocaleCode(value); locale != "" {
		if canonical := langcode.Normalize(locale); canonical != "" {
			return canonical
		}
		return locale
	}
	return ""
}

// v3Language is a survey language as v3 shows it.
type v3Language struct {
	Code    string  `json:"code"`
	Default bool    `json:"default"`
	Enabled bool    `json:"enabled"`
	Alias   *string `json:"alias,omitempty"`
}

func trimmedAlias(alias *string) *string {
	if alias == nil || strings.TrimSpace(*alias) == "" {
		return nil
	}
	a := strings.TrimSpace(*alias)
	return &a
}

// v3Languages is forma2's getV3SurveyLanguages: the survey's languages, or en-US alone when it has none.
func v3Languages(langs []SurveyLanguage) []v3Language {
	if len(langs) == 0 {
		return []v3Language{{Code: defaultV3Language, Default: true, Enabled: true}}
	}
	out := make([]v3Language, len(langs))
	for i, l := range langs {
		out[i] = v3Language{Code: l.Language.Code, Default: l.Default, Enabled: l.Enabled, Alias: trimmedAlias(l.Language.Alias)}
	}
	return out
}

// v3DefaultLanguage is forma2's getV3SurveyDefaultLanguage.
func v3DefaultLanguage(langs []SurveyLanguage) string {
	for _, l := range langs {
		if l.Default {
			return l.Language.Code
		}
	}
	return defaultV3Language
}

// languageError is a lang selector that names no single language of the survey; Code is the normalized code forma2 reported as the identifier.
type languageError struct {
	Message string
	Code    string
}

var (
	subtagSeparator = regexp.MustCompile(`[-_]`)
	baseLanguage    = regexp.MustCompile(`^[a-z]{2,3}$`)
)

// languageBase is forma2's getLanguageBase: the base language subtag (de-AT is de), or "".
func languageBase(code string) string {
	base := subtagSeparator.Split(strings.ToLower(strings.TrimSpace(code)), 2)[0]
	if baseLanguage.MatchString(base) {
		return base
	}
	return ""
}

var languageOnlySelector = regexp.MustCompile(`(?i)^[a-z]{2,3}$`)

// resolveLanguage is forma2's resolveV3SurveyLanguageCode: an exact code or alias wins; a bare language matching several regions is ambiguous; then canonical equivalence; then a bare language's single match. It returns the stored code.
func resolveLanguage(requested string, languages []v3Language) (string, *languageError) {
	value := strings.TrimSpace(requested)
	key := strings.ToLower(value)
	normalized := v3LanguageTag(value)
	base := languageBase(value)
	bare := languageOnlySelector.MatchString(value) && base != ""
	ambiguous := func(codes []string) *languageError {
		code := normalized
		if code == "" {
			code = value
		}
		return &languageError{Message: fmt.Sprintf("Language '%s' is ambiguous for this survey. Matching languages: %s", value, strings.Join(codes, ", ")), Code: code}
	}
	unique := func(ls []v3Language) []string {
		seen := map[string]bool{}
		var out []string
		for _, l := range ls {
			if !seen[l.Code] {
				seen[l.Code] = true
				out = append(out, l.Code)
			}
		}
		return out
	}
	var matches []v3Language
	for _, l := range languages {
		if strings.ToLower(l.Code) == key || (l.Alias != nil && strings.ToLower(*l.Alias) == key) {
			matches = append(matches, l)
		}
	}
	if len(matches) == 0 && bare {
		var byBase []v3Language
		for _, l := range languages {
			if languageBase(l.Code) == base {
				byBase = append(byBase, l)
			}
		}
		if codes := unique(byBase); len(codes) > 1 {
			return "", ambiguous(codes)
		}
	}
	if len(matches) == 0 {
		target := normalized
		if target == "" {
			target = value
		}
		if requestedCanonical := canonicalKey(target); requestedCanonical != "" {
			for _, l := range languages {
				if canonicalKey(l.Code) == requestedCanonical || (l.Alias != nil && canonicalKey(*l.Alias) == requestedCanonical) {
					matches = append(matches, l)
				}
			}
		}
	}
	if len(matches) == 0 && bare {
		for _, l := range languages {
			if languageBase(l.Code) == base {
				matches = append(matches, l)
			}
		}
	}
	codes := unique(matches)
	switch {
	case len(codes) == 1:
		return codes[0], nil
	case len(codes) > 1:
		return "", ambiguous(codes)
	case normalized == "" && bare:
		return "", &languageError{Message: fmt.Sprintf("Language '%s' is not configured for this survey", value), Code: value}
	case normalized == "":
		return "", &languageError{Message: fmt.Sprintf("Language '%s' is not a valid locale code or configured language alias", value)}
	}
	return "", &languageError{Message: fmt.Sprintf("Language '%s' is not configured for this survey", normalized), Code: normalized}
}

// parseLanguageQuery is forma2's parseV3SurveyLanguageQuery: repeated or comma-separated selectors, none empty, deduplicated by normalized tag.
func parseLanguageQuery(values []string) ([]string, string) {
	var requested []string
	for _, v := range values {
		for _, part := range strings.Split(v, ",") {
			requested = append(requested, strings.TrimSpace(part))
		}
	}
	seen := map[string]bool{}
	var out []string
	for _, r := range requested {
		if r == "" {
			return nil, "Language selector must contain valid comma-separated language selectors"
		}
		key := strings.ToLower(r)
		if tag := v3LanguageTag(r); tag != "" {
			key = strings.ToLower(tag)
		}
		if !seen[key] {
			seen[key] = true
			out = append(out, r)
		}
	}
	return out, ""
}
