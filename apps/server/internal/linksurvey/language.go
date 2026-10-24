package linksurvey

import (
	"strings"

	"github.com/yldm-tech/forma/apps/server/internal/langcode"
	"github.com/yldm-tech/forma/apps/server/internal/survey"
)

// defaultLanguage is forma2's "default": the survey's own default language.
const defaultLanguage = "default"

// resolveSurveyLanguageCode is forma2's resolveSurveyLanguageCode (modules/survey/link/lib/language.ts): a ?lang= value to one of the survey's stored language codes, matched first by exact code, then by alias, then by canonical tag, all case-insensitively. A value naming no language, a disabled one or the default one is "default".
func resolveSurveyLanguageCode(lang string, languages []survey.SurveyLanguage) string {
	if lang == "" {
		return defaultLanguage
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
	if selected == nil || selected.Default || !selected.Enabled {
		return defaultLanguage
	}
	return selected.Language.Code
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
