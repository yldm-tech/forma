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
