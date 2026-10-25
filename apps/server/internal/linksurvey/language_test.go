package linksurvey

import (
	"testing"

	"github.com/yldm-tech/forma/apps/server/internal/survey"
)

func lang(code string, alias string, isDefault, enabled bool) survey.SurveyLanguage {
	l := survey.SurveyLanguage{Language: survey.Language{Code: code}, Default: isDefault, Enabled: enabled}
	if alias != "" {
		l.Language.Alias = &alias
	}
	return l
}

// forma2's language.test.ts.
func TestResolveSurveyLanguageCode(t *testing.T) {
	languages := []survey.SurveyLanguage{lang("en-US", "", true, true), lang("de-DE", "", false, true), lang("pt-BR", "brasil", false, true), lang("fr-FR", "", false, false)}
	cases := map[string]string{
		"de-DE": "de-DE", "de-de": "de-DE",
		// A legacy or bare code resolves through its canonical tag.
		"de":     "de-DE",
		"brasil": "pt-BR",
		"it-IT":  "default", "not-a-language": "default", "": "default",
		// A disabled language, and the default language itself.
		"fr-FR": "default", "en-US": "default",
	}
	for in, want := range cases {
		if got := resolveSurveyLanguageCode(in, languages); got != want {
			t.Errorf("resolveSurveyLanguageCode(%q) = %q, want %q", in, got, want)
		}
	}
	// An exact code beats another language's alias.
	shadowed := []survey.SurveyLanguage{lang("en-US", "", true, true), lang("sv-SE", "de-DE", false, true), lang("de-DE", "", false, true)}
	if got := resolveSurveyLanguageCode("de-DE", shadowed); got != "de-DE" {
		t.Errorf("shadowed: %q", got)
	}
}

// A link without ?lang= opens in the browser's language when the survey has it (Onboarding Experience: en-US default, ja-JP, zh-Hans-CN, de-DE).
func TestSurveyLanguageCodeNegotiatesAcceptLanguage(t *testing.T) {
	languages := []survey.SurveyLanguage{
		lang("en-US", "", true, true), lang("ja-JP", "", false, true), lang("zh-Hans-CN", "", false, true),
		lang("de-DE", "", false, true), lang("fr-FR", "", false, false),
	}
	cases := []struct {
		name, langParam, acceptLanguage, want string
	}{
		{"a Japanese browser gets the Japanese translation", "", "ja,en-US;q=0.9,en;q=0.8", "ja-JP"},
		{"a regional tag the survey lacks falls to the broader one after it", "", "de-AT,de;q=0.9", "de-DE"},
		{"weights decide, not header order", "", "en;q=0.5,zh-CN;q=0.9", "zh-Hans-CN"},
		{"Traditional Chinese never takes the Simplified translation", "", "zh-TW", "default"},
		{"a disabled translation is skipped for the next preference", "", "fr-FR,ja;q=0.8", "ja-JP"},
		{"the default language preferred first stays the default", "", "en-US,ja;q=0.8", "default"},
		{"no shared language opens the default", "", "ko-KR,it;q=0.8", "default"},
		{"no header opens the default", "", "", "default"},
		{"a malformed header opens the default", "", ";;;q=x", "default"},
		{"an explicit ?lang= always wins", "en-US", "ja", "default"},
		{"an explicit ?lang= for a translation wins over the browser", "de-DE", "ja", "de-DE"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := surveyLanguageCode(tc.langParam, tc.acceptLanguage, languages); got != tc.want {
				t.Errorf("surveyLanguageCode(%q, %q) = %q, want %q", tc.langParam, tc.acceptLanguage, got, tc.want)
			}
		})
	}
	// A survey without languages has only its default.
	if got := surveyLanguageCode("", "ja", nil); got != "default" {
		t.Errorf("no languages: %q", got)
	}
}

// A browser tag that names none of the survey's languages outright still opens a regional variant of its language (R-09): de-AT reads de-DE, a bare "ar" reads ar-SA. Only a close match counts, so Traditional and Simplified Chinese never stand in for each other, and ?lang= stays exact.
func TestSurveyLanguageCodeFallsBackToTheSameLanguage(t *testing.T) {
	languages := []survey.SurveyLanguage{
		lang("en-US", "", true, true), lang("de-DE", "", false, true), lang("de-CH", "", false, true), lang("es-MX", "", false, true),
		lang("ar-SA", "", false, true), lang("pt-PT", "", false, true), lang("fr-CA", "", false, false), lang("zh-Hans-CN", "", false, true),
	}
	cases := []struct {
		name, langParam, acceptLanguage, want string
	}{
		{"a regional tag the survey lacks opens another region of it", "", "de-AT", "de-DE"},
		{"a bare language opens its only region", "", "es", "es-MX"},
		{"Arabic without a region opens ar-SA, not the default", "", "ar", "ar-SA"},
		{"another region of Arabic opens ar-SA", "", "ar-EG", "ar-SA"},
		{"Portuguese opens pt-PT", "", "pt", "pt-PT"},
		{"the preferred language's region beats an exact match for a later language", "", "de-AT,en;q=0.5", "de-DE"},
		{"a later tag naming a region of the same language outright beats the guess", "", "de-AT,de-CH;q=0.9", "de-CH"},
		{"the default's region is still the default", "", "en-GB,de;q=0.5", "default"},
		{"a close match for a disabled translation does not count", "", "fr,es;q=0.5", "es-MX"},
		{"Traditional Chinese does not take the Simplified translation", "", "zh-TW", "default"},
		{"Hong Kong Chinese does not take the Simplified translation", "", "zh-HK,de;q=0.5", "de-DE"},
		{"zh-Hant does not take the Simplified translation", "", "zh-Hant", "default"},
		{"Singapore Chinese is Simplified", "", "zh-SG", "zh-Hans-CN"},
		{"an unrelated language still opens the default", "", "ko,it;q=0.8", "default"},
		{"a wildcard opens the default", "", "*", "default"},
		{"?lang= does not fall back to another region", "ar", "ar-SA", "default"},
		{"?lang= for a missing region stays the default", "de-AT", "de-DE", "default"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := surveyLanguageCode(tc.langParam, tc.acceptLanguage, languages); got != tc.want {
				t.Errorf("surveyLanguageCode(%q, %q) = %q, want %q", tc.langParam, tc.acceptLanguage, got, tc.want)
			}
		})
	}
	// A Traditional-only survey takes Hong Kong Chinese but not Simplified.
	hant := []survey.SurveyLanguage{lang("en-US", "", true, true), lang("zh-Hant-TW", "", false, true)}
	if got := surveyLanguageCode("", "zh-HK", hant); got != "zh-Hant-TW" {
		t.Errorf("zh-HK on a Traditional survey: %q", got)
	}
	if got := surveyLanguageCode("", "zh-CN", hant); got != "default" {
		t.Errorf("zh-CN on a Traditional survey: %q", got)
	}
	// A survey whose default is not English opens its default for a close match to it.
	jaDefault := []survey.SurveyLanguage{lang("ja-JP", "", true, true), lang("en-US", "", false, true)}
	if got := surveyLanguageCode("", "en-GB", jaDefault); got != "en-US" {
		t.Errorf("en-GB on a Japanese survey: %q", got)
	}
}

// The close match is another region of the visitor's own language only. CLDR also rates some cross-language fallbacks High (Afrikaans reads Dutch, Kazakh reads Russian); those must not stand in for the visitor's language, least of all over a language the visitor listed later.
func TestSurveyLanguageCodeFallbackKeepsTheLanguageAndScript(t *testing.T) {
	cases := []struct {
		name           string
		languages      []survey.SurveyLanguage
		acceptLanguage string
		want           string
	}{
		{"Afrikaans does not read Dutch over a listed English", []survey.SurveyLanguage{lang("en-US", "", true, true), lang("nl-NL", "", false, true)}, "af-ZA,af;q=0.9,en-ZA;q=0.8,en;q=0.7", "default"},
		{"Kazakh does not read Russian over a listed English", []survey.SurveyLanguage{lang("en-US", "", true, true), lang("ru-RU", "", false, true)}, "kk-KZ,kk;q=0.9,en-US;q=0.8,en;q=0.7", "default"},
		{"Belarusian does not read Russian", []survey.SurveyLanguage{lang("en-US", "", true, true), lang("ru-RU", "", false, true)}, "be-BY,en;q=0.9", "default"},
		{"Basque does not read Spanish", []survey.SurveyLanguage{lang("en-US", "", true, true), lang("es-ES", "", false, true)}, "eu-ES,eu;q=0.9,en;q=0.8", "default"},
		{"Galician alone does not read Spanish", []survey.SurveyLanguage{lang("en-US", "", true, true), lang("es-ES", "", false, true)}, "gl", "default"},
		{"Luxembourgish does not read German", []survey.SurveyLanguage{lang("en-US", "", true, true), lang("de-DE", "", false, true)}, "lb,en;q=0.9", "default"},
		{"Welsh does not read English over a Japanese default", []survey.SurveyLanguage{lang("ja-JP", "", true, true), lang("en-US", "", false, true)}, "cy", "default"},
		{"Nynorsk does not read Bokmal", []survey.SurveyLanguage{lang("en-US", "", true, true), lang("nb-NO", "", false, true)}, "nn", "default"},
		{"the Norwegian macrolanguage still reads Bokmal", []survey.SurveyLanguage{lang("en-US", "", true, true), lang("nb-NO", "", false, true)}, "no", "nb-NO"},
		{"Tagalog still reads Filipino", []survey.SurveyLanguage{lang("en-US", "", true, true), lang("fil-PH", "", false, true)}, "tl", "fil-PH"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := surveyLanguageCode("", tc.acceptLanguage, tc.languages); got != tc.want {
				t.Errorf("surveyLanguageCode(%q) = %q, want %q", tc.acceptLanguage, got, tc.want)
			}
		})
	}
}

// A later tag of the preferred language replaces the close match only when it is the same script, and when it does, it is chosen in the preferred language's place rather than letting a language in between win.
func TestSurveyLanguageCodeLaterTagOfThePreferredLanguage(t *testing.T) {
	chinese := []survey.SurveyLanguage{lang("en-US", "", true, true), lang("zh-Hans-CN", "", false, true), lang("zh-Hant-TW", "", false, true)}
	german := []survey.SurveyLanguage{lang("en-US", "", true, true), lang("de-DE", "", false, true), lang("de-CH", "", false, true)}
	cases := []struct {
		name           string
		languages      []survey.SurveyLanguage
		acceptLanguage string
		want           string
	}{
		{"Hong Kong Chinese reads Traditional", chinese, "zh-HK", "zh-Hant-TW"},
		{"a later bare zh does not turn Hong Kong Chinese Simplified", chinese, "zh-HK,zh;q=0.9", "zh-Hant-TW"},
		{"a later bare zh and English do not turn Hong Kong Chinese Simplified", chinese, "zh-HK,zh;q=0.9,en;q=0.8", "zh-Hant-TW"},
		{"a later zh-CN does not turn Macau Chinese Simplified", chinese, "zh-MO,zh-CN;q=0.5", "zh-Hant-TW"},
		{"a later region of the preferred language beats a language in between", german, "de-AT,en;q=0.9,de-CH;q=0.5", "de-CH"},
		{"a later bare tag of the preferred language beats a language in between", german, "de-AT,en;q=0.9,de;q=0.5", "de-DE"},
		{"the close match alone still beats a later language", german, "de-AT,en;q=0.9", "de-DE"},
		{"a wildcard is no language, so a later English does not jump ahead of German", german, "*,de;q=0.9,en;q=0.5", "de-DE"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := surveyLanguageCode("", tc.acceptLanguage, tc.languages); got != tc.want {
				t.Errorf("surveyLanguageCode(%q) = %q, want %q", tc.acceptLanguage, got, tc.want)
			}
		})
	}
}

// forma2's metadata-utils.test.ts language cases: code or alias, case-insensitively, without the canonical fallback.
func TestMetadataLanguageCode(t *testing.T) {
	languages := []survey.SurveyLanguage{lang("en", "", true, true), lang("de", "deutsch", false, true), lang("fr", "", false, false)}
	cases := map[string]string{"de": "de", "DE": "de", "Deutsch": "de", "fr": "default", "en": "default", "de-DE": "default", "default": "default", "": "default"}
	for in, want := range cases {
		if got := metadataLanguageCode(in, languages); got != want {
			t.Errorf("metadataLanguageCode(%q) = %q, want %q", in, got, want)
		}
	}
}

// forma2's utils.test.ts (resolveWebAppLocale, getWebAppLocale, getGateLocale).
func TestResolveWebAppLocale(t *testing.T) {
	cases := map[string]string{
		"he": "", "xx": "", "default": "",
		"pt-PT": "pt-PT", "pt": "pt-BR", "pt-BR": "pt-BR", "es-MX": "es-ES", "en": "en-US", "de": "de-DE",
		"zh-Hant": "zh-Hant-TW", "zh-Hans": "zh-Hans-CN", "zh": "zh-Hans-CN",
		"DE-de": "de-DE", "zh-hant-tw": "zh-Hant-TW",
	}
	for in, want := range cases {
		if got := resolveWebAppLocale(in, nil); got != want {
			t.Errorf("resolveWebAppLocale(%q) = %q, want %q", in, got, want)
		}
	}
	if got := resolveWebAppLocale("default", []survey.SurveyLanguage{lang("de", "", true, true)}); got != "de-DE" {
		t.Errorf("default through the survey's default language: %q", got)
	}
}

func TestGateLocale(t *testing.T) {
	languages := []survey.SurveyLanguage{lang("en", "", true, true), lang("de", "", false, true)}
	cases := []struct {
		langParam, languageCode, fallback, want string
	}{
		{"de-DE", "de", "en-US", "de-DE"},
		{"", "default", "fr-FR", "fr-FR"},
		{"he", "he", "fr-FR", "fr-FR"},
		// An unknown or disabled ?lang= renders the survey's default language, so the gate reads that.
		{"it-IT", "default", "fr-FR", "en-US"},
	}
	for _, tc := range cases {
		if got := gateLocale(tc.langParam, tc.languageCode, languages, tc.fallback); got != tc.want {
			t.Errorf("gateLocale(%q, %q, %q) = %q, want %q", tc.langParam, tc.languageCode, tc.fallback, got, tc.want)
		}
	}
}
