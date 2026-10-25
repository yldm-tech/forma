package linksurvey

import (
	"strings"

	"golang.org/x/text/language"

	"github.com/yldm-tech/forma/apps/server/internal/langcode"
	"github.com/yldm-tech/forma/apps/server/internal/survey"
)

// defaultLanguage is forma2's "default": the survey's own default language.
const defaultLanguage = "default"

// resolveSurveyLanguageCode is forma2's resolveSurveyLanguageCode (modules/survey/link/lib/language.ts): a ?lang= value to one of the survey's stored language codes, matched first by exact code, then by alias, then by canonical tag, all case-insensitively. A value naming no language, a disabled one or the default one is "default".
func resolveSurveyLanguageCode(lang string, languages []survey.SurveyLanguage) string {
	selected := matchSurveyLanguage(lang, languages)
	if selected == nil || selected.Default || !selected.Enabled {
		return defaultLanguage
	}
	return selected.Language.Code
}

// negotiateSurveyLanguageCode picks the survey language for a visitor who asked for none (no ?lang=): the browser's Accept-Language tags, best weighted first. A tag is first matched as resolveSurveyLanguageCode matches ?lang= (exact code, alias, canonical tag) against the languages a visitor can be shown: the default language and the enabled translations. A tag naming none of them outright falls back to a close match, another region of the same language and script (de-AT reads de-DE, a bare "ar" reads ar-SA), unless a later tag of the same language and script names one of them outright, which is then chosen in its place (de-AT,en,de-CH reads de-CH). The first tag that matches decides, so a regional fallback for the preferred language beats an exact match for a later one (de-AT,en reads de-DE). Another language never stands in, even one CLDR rates close (Afrikaans for Dutch, Kazakh for Russian), and Simplified and Traditional Chinese are different scripts that never stand in for each other. With no match the survey opens in its default language.
func negotiateSurveyLanguageCode(acceptLanguage string, languages []survey.SurveyLanguage) string {
	if acceptLanguage == "" || len(languages) == 0 {
		return defaultLanguage
	}
	tags, _, err := language.ParseAcceptLanguage(acceptLanguage)
	if err != nil {
		return defaultLanguage
	}
	outright := make([]*survey.SurveyLanguage, len(tags))
	for i, tag := range tags {
		if selected := matchSurveyLanguage(tag.String(), languages); selected != nil && (selected.Default || selected.Enabled) {
			outright[i] = selected
		}
	}
	matcher, supported, offered := closeLanguageMatcher(languages)
	for i, tag := range tags {
		selected := outright[i]
		if selected == nil {
			selected = laterOutrightMatch(tags, outright, i)
		}
		if selected == nil {
			selected = closeSurveyLanguage(matcher, supported, offered, tag)
		}
		if selected == nil {
			continue
		}
		if selected.Default {
			return defaultLanguage
		}
		return selected.Language.Code
	}
	return defaultLanguage
}

// laterOutrightMatch is the survey language that the first tag after tags[i] of the same language and script matched outright, which beats a close match for tags[i]; nil when no such tag matched. The script keeps a later bare "zh" (Simplified) from replacing zh-HK (Traditional).
func laterOutrightMatch(tags []language.Tag, outright []*survey.SurveyLanguage, i int) *survey.SurveyLanguage {
	for j := i + 1; j < len(tags); j++ {
		if outright[j] != nil && sameLanguageAndScript(tags[i], tags[j]) {
			return outright[j]
		}
	}
	return nil
}

// sameLanguageAndScript reports whether a and b are the same language in the same script, regions aside. Both are canonicalized first, so that aliases of one language (no and nb, tl and fil) compare equal, and the script is the explicit or the likely one (zh-HK is Hant, zh is Hans). A tag without a language subtag (the "*" wildcard, und-US) names no language, so it is never the same as another tag, whatever language x/text would guess for it.
func sameLanguageAndScript(a, b language.Tag) bool {
	a, _ = language.All.Canonicalize(a)
	b, _ = language.All.Canonicalize(b)
	aBase, aConfidence := a.Base()
	bBase, bConfidence := b.Base()
	if aConfidence != language.Exact || bConfidence != language.Exact {
		return false
	}
	aScript, _ := a.Script()
	bScript, _ := b.Script()
	return aBase == bBase && aScript == bScript
}

// closeLanguageMatcher is a CLDR matcher over the languages a visitor can be shown (the default and the enabled translations), with their parsed tags and the languages themselves in the matcher's order. A stored code that is not a valid BCP-47 tag is left out.
func closeLanguageMatcher(languages []survey.SurveyLanguage) (language.Matcher, []language.Tag, []*survey.SurveyLanguage) {
	var supported []language.Tag
	var offered []*survey.SurveyLanguage
	for i := range languages {
		l := &languages[i]
		if !l.Default && !l.Enabled {
			continue
		}
		tag, err := language.Parse(l.Language.Code)
		if err != nil {
			continue
		}
		supported = append(supported, tag)
		offered = append(offered, l)
	}
	if len(supported) == 0 {
		return nil, nil, nil
	}
	return language.NewMatcher(supported), supported, offered
}

// closeSurveyLanguage is the offered language the matcher finds close to tag: at least High confidence, and the same language and script, any region. CLDR also rates some cross-language fallbacks High (af to nl, kk to ru, gl to es), so the confidence alone is not enough. A Low match (another script, as zh-TW against zh-Hans-CN), another language, or none is nil.
func closeSurveyLanguage(matcher language.Matcher, supported []language.Tag, offered []*survey.SurveyLanguage, tag language.Tag) *survey.SurveyLanguage {
	if matcher == nil || tag == language.Und {
		return nil
	}
	_, index, confidence := matcher.Match(tag)
	if confidence < language.High || !sameLanguageAndScript(tag, supported[index]) {
		return nil
	}
	return offered[index]
}

// surveyLanguageCode is the survey language a page renders in: an explicit ?lang= always wins (resolveSurveyLanguageCode), otherwise the browser's languages are negotiated against the survey's.
func surveyLanguageCode(langParam, acceptLanguage string, languages []survey.SurveyLanguage) string {
	if langParam != "" {
		return resolveSurveyLanguageCode(langParam, languages)
	}
	return negotiateSurveyLanguageCode(acceptLanguage, languages)
}

// metadataLanguage is the language basicSurveyMetadata titles the page in: the ?lang= value as forma2 passed it, or, without one, the language negotiated from the browser.
func metadataLanguage(langParam, acceptLanguage string, languages []survey.SurveyLanguage) string {
	if langParam != "" {
		return langParam
	}
	return negotiateSurveyLanguageCode(acceptLanguage, languages)
}

// matchSurveyLanguage is the survey language lang names, by exact code, then alias, then canonical tag, all case-insensitively, whatever its state; nil when it names none.
func matchSurveyLanguage(lang string, languages []survey.SurveyLanguage) *survey.SurveyLanguage {
	if lang == "" {
		return nil
	}
	lower := strings.ToLower(lang)
	var selected *survey.SurveyLanguage
	for i := range languages {
		if strings.ToLower(languages[i].Language.Code) == lower {
			selected = &languages[i]
			break
		}
	}
	if selected == nil {
		for i := range languages {
			if a := languages[i].Language.Alias; a != nil && strings.ToLower(*a) == lower {
				selected = &languages[i]
				break
			}
		}
	}
	if selected == nil {
		if canonical := langcode.Normalize(lang); canonical != "" {
			for i := range languages {
				if langcode.Normalize(languages[i].Language.Code) == canonical {
					selected = &languages[i]
					break
				}
			}
		}
	}
	return selected
}

// metadataLanguageCode is getBasicSurveyMetadata's narrower rule (modules/survey/link/lib/metadata-utils.ts): the survey language whose code or alias equals lang case-insensitively, when it is enabled and not the default. Unlike resolveSurveyLanguageCode it has no canonical fallback.
func metadataLanguageCode(lang string, languages []survey.SurveyLanguage) string {
	if lang == "" || lang == defaultLanguage {
		return defaultLanguage
	}
	lower := strings.ToLower(lang)
	for _, l := range languages {
		if strings.ToLower(l.Language.Code) == lower || (l.Language.Alias != nil && strings.ToLower(*l.Language.Alias) == lower) {
			if l.Default || !l.Enabled {
				return defaultLanguage
			}
			return l.Language.Code
		}
	}
	return defaultLanguage
}

// userLocales are forma2's ZUserLocale options, the locales the web app can be set to.
var userLocales = []string{"de-DE", "en-US", "es-ES", "fr-FR", "hu-HU", "ja-JP", "nl-NL", "pt-BR", "pt-PT", "ro-RO", "ru-RU", "sv-SE", "tr-TR", "zh-Hans-CN", "zh-Hant-TW"}

// surveyLanguageAliases is forma2's SURVEY_LANGUAGE_ALIASES: survey language codes that do not name a web app locale outright.
var surveyLanguageAliases = map[string]string{
	"en": "en-US", "de": "de-DE", "es": "es-ES", "fr": "fr-FR", "hu": "hu-HU", "ja": "ja-JP", "nl": "nl-NL", "pt": "pt-BR",
	"ro": "ro-RO", "ru": "ru-RU", "sv": "sv-SE", "tr": "tr-TR", "zh": "zh-Hans-CN", "zh-hans": "zh-Hans-CN", "zh-hant": "zh-Hant-TW",
}

// resolveWebAppLocale is forma2's resolveWebAppLocale: the web app locale a survey language's chrome is translated in, or "" when the app has no translation for it. "default" means the survey's default language.
func resolveWebAppLocale(languageCode string, languages []survey.SurveyLanguage) string {
	code := languageCode
	if languageCode == defaultLanguage {
		code = ""
		for _, l := range languages {
			if l.Default {
				code = l.Language.Code
				break
			}
		}
		if code == "" {
			return ""
		}
	}
	lower := strings.ToLower(code)
	for _, locale := range userLocales {
		if strings.ToLower(locale) == lower {
			return locale
		}
	}
	if locale, ok := surveyLanguageAliases[lower]; ok {
		return locale
	}
	base, _, _ := strings.Cut(lower, "-")
	return surveyLanguageAliases[base]
}

// gateLocale is forma2's getGateLocale: the locale of the screens in front of a survey (PIN, email verification) and of the verification email. An explicit ?lang= that resolves to a language the app is translated into wins; everything else keeps the Accept-Language locale.
func gateLocale(langParam, languageCode string, languages []survey.SurveyLanguage, fallback string) string {
	if langParam == "" {
		return fallback
	}
	if locale := resolveWebAppLocale(languageCode, languages); locale != "" {
		return locale
	}
	return fallback
}
