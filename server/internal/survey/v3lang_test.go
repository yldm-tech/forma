package survey

import (
	"testing"
)

func TestLanguageCodes(t *testing.T) {
	canonical := map[string]string{
		"en-us": "en-US", "EN_us": "en-US", "zh-hans-cn": "zh-Hans-CN", "de": "de", "es-419": "es-419", "x": "", "en--US": "", "1a-US": "",
	}
	for in, want := range canonical {
		if got := canonicalTag(in); got != want {
			t.Errorf("canonicalTag(%q) = %q, want %q", in, got, want)
		}
	}
	// A new language is canonicalized; a stored one is kept as stored, matched by canonical equivalence.
	write := []struct {
		in      string
		allowed []string
		want    string
	}{
		{"de-de", nil, "de-DE"},
		{"de-AT", nil, "de-AT"},
		{"zh-CN", nil, "zh-Hans-CN"},
		{"de", nil, ""},
		{"zh-Hans", nil, ""},
		{"de", []string{"de"}, "de"},
		{"DE-de", []string{"de-DE"}, "de-DE"},
		{"zh-CN", []string{"zh-Hans-CN"}, "zh-Hans-CN"},
		{"default", nil, ""},
	}
	for _, tc := range write {
		if got := writeLanguageCode(tc.in, tc.allowed); got != tc.want {
			t.Errorf("writeLanguageCode(%q, %v) = %q, want %q", tc.in, tc.allowed, got, tc.want)
		}
	}
}

// The cases of forma2's resolveV3SurveyLanguageCode tests (apps/web/app/api/v3/surveys/language.test.ts).
func TestResolveLanguage(t *testing.T) {
	alias := "english"
	langs := []v3Language{{Code: "en-US", Default: true, Enabled: true, Alias: &alias}, {Code: "en-GB", Enabled: true}, {Code: "de-DE", Enabled: true}, {Code: "zh-Hans-CN", Enabled: true}}
	ok := map[string]string{"en-US": "en-US", "EN-gb": "en-GB", "english": "en-US", "de": "de-DE", "de_DE": "de-DE", "zh-CN": "zh-Hans-CN", "zh-Hans": "zh-Hans-CN"}
	for in, want := range ok {
		if got, err := resolveLanguage(in, langs); err != nil || got != want {
			t.Errorf("resolveLanguage(%q) = %q, %+v; want %q", in, got, err, want)
		}
	}
	failures := map[string]languageError{
		"en":    {Message: "Language 'en' is ambiguous for this survey. Matching languages: en-US, en-GB", Code: "en"},
		"fr":    {Message: "Language 'fr' is not configured for this survey", Code: "fr"},
		"fr-FR": {Message: "Language 'fr-FR' is not configured for this survey", Code: "fr-FR"},
		"!!":    {Message: "Language '!!' is not a valid locale code or configured language alias"},
	}
	for in, want := range failures {
		if _, err := resolveLanguage(in, langs); err == nil || *err != want {
			t.Errorf("resolveLanguage(%q) = %+v, want %+v", in, err, want)
		}
	}
	got, message := parseLanguageQuery([]string{"de-DE,de-de", "en-US"})
	if message != "" || len(got) != 2 || got[0] != "de-DE" || got[1] != "en-US" {
		t.Errorf("parseLanguageQuery = %v %q", got, message)
	}
	if _, message := parseLanguageQuery([]string{"de-DE,"}); message != "Language selector must contain valid comma-separated language selectors" {
		t.Errorf("empty selector: %q", message)
	}
}
