// Package langcode is forma2's language-tag handling (packages/i18n-utils): the canonical region-tagged BCP-47 form of a language code, and the languages the survey runtime ships strings for.
package langcode

import (
	"strings"
)

// surveyRuntimeLanguages is SURVEY_RUNTIME_LANGUAGE_CODES (packages/i18n-utils/src/survey-runtime-languages.ts): the bundles in packages/surveys/locales, the only languages a survey's default language can usefully be set to. TestRuntimeLanguagesMatchI18nUtils keeps the two identical.
var surveyRuntimeLanguages = []string{
	"ar-EG", "da-DK", "de-DE", "en-US", "es-ES", "et-EE", "fr-FR", "hi-IN", "hu-HU", "id-ID", "it-IT", "ja-JP", "km-KH",
	"ne-NP", "nl-NL", "pt-BR", "ro-RO", "ru-RU", "sv-SE", "tr-TR", "ur-PK", "uz-UZ", "vi-VN", "zh-Hans-CN", "zh-Hant-TW",
}

var (
	canonicalByLower = lowerKeys(canonicalMap)
	runtimeByLower   = func() map[string]string {
		m := make(map[string]string, len(surveyRuntimeLanguages))
		for _, code := range surveyRuntimeLanguages {
			m[strings.ToLower(code)] = code
		}
		return m
	}()
)

func lowerKeys(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[strings.ToLower(k)] = v
	}
	return out
}

// Normalize is forma2's normalizeLanguageCode: any incoming code (a legacy bare code, mixed case, "_" separators) to its canonical region-tagged tag, or "" when it cannot be resolved. The static table is consulted first, case-insensitively.
//
// forma2 fell back to CLDR likely subtags (Intl.Locale.maximize) for a code outside the table. Without that data the fallback here resolves only a well-formed tag that names its region itself; a code outside the table without a region, or a Chinese tag without its script, is unresolvable.
func Normalize(input string) string {
	cleaned := strings.ReplaceAll(strings.TrimSpace(input), "_", "-")
	if cleaned == "" {
		return ""
	}
	if canonical, ok := canonicalMap[cleaned]; ok {
		return canonical
	}
	if canonical, ok := canonicalByLower[strings.ToLower(cleaned)]; ok {
		return canonical
	}
	t, ok := parseTag(cleaned)
	if !ok || t.region == "" {
		return ""
	}
	// Chinese keeps its script: Simplified and Traditional are different languages.
	if t.language == "zh" && t.script == "" {
		return ""
	}
	parts := []string{t.language}
	if t.script != "" {
		parts = append(parts, t.script)
	}
	return strings.Join(append(parts, t.region), "-")
}

// RuntimeBundle is forma2's resolveSurveyRuntimeBundle: the bundle the survey runtime would serve a language from, or "" when it ships none. The code itself, its canonical form, or its language's default tag (de-AT renders de-DE strings) may name a bundle; the runtime's final English fallback deliberately does not count.
func RuntimeBundle(code string) string {
	trimmed := strings.TrimSpace(code)
	if trimmed == "" {
		return ""
	}
	if bundle, ok := runtimeByLower[strings.ToLower(trimmed)]; ok {
		return bundle
	}
	if canonical := Normalize(trimmed); canonical != "" {
		if bundle, ok := runtimeByLower[strings.ToLower(canonical)]; ok {
			return bundle
		}
	}
	if defaultTag := languageDefaultTag(trimmed); defaultTag != "" {
		return runtimeByLower[strings.ToLower(defaultTag)]
	}
	return ""
}

// IsSurveyRuntimeLanguage reports whether the survey runtime has strings for code.
func IsSurveyRuntimeLanguage(code string) bool {
	return RuntimeBundle(code) != ""
}

// languageDefaultTag is forma2's resolveSurveyLanguageDefaultTag: the canonical tag of the language a code belongs to, region dropped (de-AT becomes de-DE, pt-PT pt-BR) while the script is kept, recovered from the canonical form when only the region carried it (zh-TW is Traditional).
func languageDefaultTag(code string) string {
	// Parsed as given: "_" is not a BCP-47 separator, so "DE_de" has no language default tag.
	t, ok := parseTag(code)
	if !ok {
		return ""
	}
	script := t.script
	if script == "" {
		if canonical, ok := parseTag(Normalize(code)); ok {
			script = canonical.script
		}
	}
	if script == "" {
		return Normalize(t.language)
	}
	return Normalize(t.language + "-" + script)
}

// tag is the leading subtags of a BCP-47 language tag, in canonical case.
type tag struct {
	language, script, region string
}

// parseTag reads language[-script][-region] from a well-formed tag; later subtags (variants, extensions) must be well-formed and are ignored.
func parseTag(s string) (tag, bool) {
	subtags := strings.Split(s, "-")
	for _, st := range subtags {
		if st == "" || len(st) > 8 || !isAlnum(st) {
			return tag{}, false
		}
	}
	var t tag
	lang := subtags[0]
	if !isAlpha(lang) || len(lang) < 2 || len(lang) == 4 {
		return tag{}, false
	}
	t.language = strings.ToLower(lang)
	rest := subtags[1:]
	if len(rest) > 0 && len(rest[0]) == 4 && isAlpha(rest[0]) {
		t.script = strings.ToUpper(rest[0][:1]) + strings.ToLower(rest[0][1:])
		rest = rest[1:]
	}
	if len(rest) > 0 && ((len(rest[0]) == 2 && isAlpha(rest[0])) || (len(rest[0]) == 3 && isDigits(rest[0]))) {
		t.region = strings.ToUpper(rest[0])
	}
	return t, true
}

func isAlpha(s string) bool {
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') {
			return false
		}
	}
	return true
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func isAlnum(s string) bool {
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}
