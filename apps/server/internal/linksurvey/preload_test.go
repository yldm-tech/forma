package linksurvey

import (
	"net/http"
	"strings"
	"testing"

	"github.com/yldm-tech/forma/apps/server/internal/survey"
)

// The language the respondent runtime renders a page in, as its getI18nLanguage reads the code the page passes it.
func TestRuntimeLanguage(t *testing.T) {
	languages := []survey.SurveyLanguage{lang("de-DE", "", true, true), lang("ja-JP", "", false, true)}
	for _, tc := range []struct {
		code      string
		languages []survey.SurveyLanguage
		want      string
	}{
		{"default", languages, "de-DE"},
		{"ja-JP", languages, "ja-JP"},
		// A survey without languages renders in the runtime's own default.
		{"default", nil, "en"},
	} {
		if got := runtimeLanguage(tc.code, tc.languages); got != tc.want {
			t.Errorf("runtimeLanguage(%q) = %q, want %q", tc.code, got, tc.want)
		}
	}
}

// The translation file the page preloads must be the one the runtime then fetches, or the browser downloads it twice. The runtime switches to the bundle i18next's getBestMatchFromCodes picks among its supported languages, in their (alphabetical) order.
func TestRuntimeLocalePath(t *testing.T) {
	paths := map[string]string{
		"de-DE":      "/js/surveys-locales/de-DE.1.json",
		"pt-BR":      "/js/surveys-locales/pt-BR.1.json",
		"zh-Hans-CN": "/js/surveys-locales/zh-Hans-CN.1.json",
		"zh-Hant-TW": "/js/surveys-locales/zh-Hant-TW.1.json",
	}
	for code, want := range map[string]string{
		"de-DE":      "/js/surveys-locales/de-DE.1.json",
		"DE-de":      "/js/surveys-locales/de-DE.1.json",
		"de":         "/js/surveys-locales/de-DE.1.json",
		"de-AT":      "/js/surveys-locales/de-DE.1.json",
		"pt-PT":      "/js/surveys-locales/pt-BR.1.json",
		"zh-Hant-TW": "/js/surveys-locales/zh-Hant-TW.1.json",
		// i18next matches a tag that is not a bundle by its primary language alone, so the runtime renders these in the first Chinese bundle; the preload follows it.
		"zh-TW": "/js/surveys-locales/zh-Hans-CN.1.json",
		// English is in the runtime itself, and a language without a bundle renders in English.
		"en":    "",
		"en-GB": "",
		"it-IT": "",
		"":      "",
	} {
		got, ok := runtimeLocalePath(paths, code)
		if got != want || ok != (want != "") {
			t.Errorf("runtimeLocalePath(%q) = %q, %v, want %q", code, got, ok, want)
		}
	}
}

// A survey shown in a language the runtime fetches has that file preloaded beside the runtime, CORS-mode as the runtime's own fetch, so the respondent does not wait for one more round trip after the runtime starts.
func TestSurveyPagePreloadsTheRuntimeLanguage(t *testing.T) {
	e := newEnv(t)
	ws := e.workspace()
	id := e.survey(ws)
	e.language(ws, id, "en-US", "", true, true)
	e.language(ws, id, "de-DE", "", false, true)
	german := `<link rel="preload" href="http://localhost:3999/js/surveys-locales/de-DE.0123456789ab.json" as="fetch" crossorigin="anonymous">`

	for target, want := range map[string]string{
		"/s/" + id.String() + "?lang=de-DE": german,
		"/s/" + id.String():                 "",
	} {
		p := e.page(target, "Accept-Language", "en-US")
		if p.Status != http.StatusOK || p.view() != viewSurvey {
			t.Fatalf("%s: status %d view %q", target, p.Status, p.view())
		}
		if want != "" && !strings.Contains(p.Body, want) {
			t.Errorf("%s: no preload of the German translations in %s", target, p.Body)
		}
		if want == "" && strings.Contains(p.Body, "surveys-locales") {
			t.Errorf("%s: an English page preloads a translation file", target)
		}
	}

	// Negotiated from the browser's languages, the same way.
	if p := e.page("/s/"+id.String(), "Accept-Language", "de-AT,de;q=0.9"); !strings.Contains(p.Body, german) {
		t.Errorf("Accept-Language de-AT: no preload of the German translations")
	}
}
