package langcode

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/yldm-tech/forma/apps/server/internal/platform/repotest"
)

// The tables are copies of packages/i18n-utils; these tests read the TypeScript source so the copies cannot drift.

var mapEntry = regexp.MustCompile(`^\s*"?([A-Za-z0-9-]+)"?: "([A-Za-z0-9-]+)",$`)

func readLines(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer func() { _ = f.Close() }()
	var lines []string
	s := bufio.NewScanner(f)
	for s.Scan() {
		lines = append(lines, s.Text())
	}
	if err := s.Err(); err != nil {
		t.Fatal(err)
	}
	return lines
}

// block returns the lines between the one starting with start and the next line equal to end.
func block(t *testing.T, lines []string, start, end string) []string {
	t.Helper()
	for i, l := range lines {
		if strings.HasPrefix(l, start) {
			for j := i + 1; j < len(lines); j++ {
				if lines[j] == end {
					return lines[i+1 : j]
				}
			}
		}
	}
	t.Fatalf("block %q not found", start)
	return nil
}

func TestCanonicalMapMatchesI18nUtils(t *testing.T) {
	lines := block(t, readLines(t, filepath.Join(repotest.Root(t), "packages/i18n-utils/src/canonical.ts")), "export const LANGUAGE_CANONICAL_MAP", "};")
	want := map[string]string{}
	var keys []string
	for _, l := range lines {
		m := mapEntry.FindStringSubmatch(l)
		if m == nil {
			t.Fatalf("unexpected line in LANGUAGE_CANONICAL_MAP: %q", l)
		}
		want[m[1]] = m[2]
		keys = append(keys, m[1])
	}
	// LegacyCodes lists aliases in the source map's order, which it rebuilds by sorting keys case-insensitively.
	if !slices.IsSortedFunc(keys, func(a, b string) int { return strings.Compare(strings.ToLower(a), strings.ToLower(b)) }) {
		t.Fatal("LANGUAGE_CANONICAL_MAP is no longer sorted case-insensitively; LegacyCodes would list aliases in another order")
	}
	if len(want) != len(canonicalMap) {
		t.Fatalf("canonicalMap has %d entries, i18n-utils %d", len(canonicalMap), len(want))
	}
	for k, v := range want {
		if canonicalMap[k] != v {
			t.Errorf("canonicalMap[%q] = %q, i18n-utils says %q", k, canonicalMap[k], v)
		}
	}
}

func TestCanonicalCodesMatchI18nUtils(t *testing.T) {
	lines := block(t, readLines(t, filepath.Join(repotest.Root(t), "packages/i18n-utils/src/canonical.ts")), "export const CANONICAL_LANGUAGE_CODES", "];")
	if len(lines) != len(canonicalCodes) {
		t.Fatalf("canonicalCodes has %d codes, i18n-utils %d", len(canonicalCodes), len(lines))
	}
	for _, l := range lines {
		if code := strings.Trim(strings.TrimSpace(l), `",`); !IsCanonical(code) {
			t.Errorf("%q is canonical in i18n-utils but not here", code)
		}
	}
	if IsCanonical("de") || IsCanonical("de-de") {
		t.Fatal("a legacy or miscased code passed as canonical")
	}
}

func TestRuntimeLanguagesMatchI18nUtils(t *testing.T) {
	lines := block(t, readLines(t, filepath.Join(repotest.Root(t), "packages/i18n-utils/src/survey-runtime-languages.ts")), "export const SURVEY_RUNTIME_LANGUAGE_CODES", "] as const;")
	var want []string
	for _, l := range lines {
		want = append(want, strings.Trim(strings.TrimSpace(l), `",`))
	}
	if !slices.Equal(want, surveyRuntimeLanguages) {
		t.Fatalf("surveyRuntimeLanguages = %v, i18n-utils says %v", surveyRuntimeLanguages, want)
	}
}

// The cases of packages/i18n-utils/src/canonical.test.ts.
func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"de": "de-DE", "fr": "fr-FR", "en": "en-US", "pt": "pt-BR", "ar": "ar-EG",
		"en-US": "en-US", "pt-BR": "pt-BR", "pt-PT": "pt-PT", "de-AT": "de-AT",
		"zh-Hans": "zh-Hans-CN", "zh-Hant": "zh-Hant-TW", "zh-CN": "zh-Hans-CN", "zh-TW": "zh-Hant-TW", "zh-HK": "zh-Hant-HK",
		"en-us": "en-US", "PT_br": "pt-BR", "PT-BR": "pt-BR", "ZH-hans-cn": "zh-Hans-CN", "  de  ": "de-DE",
		"tl": "fil-PH", "TL": "fil-PH", "Tl": "fil-PH", "tw": "ak-GH", "TW": "ak-GH",
		// Outside the table, a tag that names its region resolves by itself.
		"de-LU": "de-LU", "es-419": "es-419", "sr-Latn-RS": "sr-Latn-RS",
		// forma2 resolved these through CLDR likely subtags, which this package does not carry.
		"nso": "", "zh-SG": "",
		"": "", "   ": "", "!!!": "", "en--US": "",
	}
	for in, want := range cases {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

// The cases of packages/i18n-utils/src/survey-runtime-languages.test.ts.
func TestLanguageDefaultTag(t *testing.T) {
	cases := map[string]string{
		"de-AT": "de-DE", "es-MX": "es-ES", "en-GB": "en-US", "pt-PT": "pt-BR",
		"zh-Hant": "zh-Hant-TW", "zh-TW": "zh-Hant-TW", "zh-HK": "zh-Hant-TW", "zh-CN": "zh-Hans-CN",
		"": "", "!!!": "", "DE_de": "",
	}
	for in, want := range cases {
		if got := languageDefaultTag(in); got != want {
			t.Errorf("languageDefaultTag(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRuntimeBundle(t *testing.T) {
	cases := map[string]string{
		"de-DE": "de-DE", "zh-Hant-TW": "zh-Hant-TW",
		"DE-de": "de-DE", "zh-hant-tw": "zh-Hant-TW",
		"de": "de-DE", "ar": "ar-EG", "zh-CN": "zh-Hans-CN",
		"de-AT": "de-DE", "es-MX": "es-ES", "en-GB": "en-US", "pt-PT": "pt-BR", "ar-SA": "ar-EG",
		"fa-IR": "", "aa-ET": "",
		"  fr-FR  ": "fr-FR", "km-KH": "km-KH", "ne-NP": "ne-NP",
		"": "", "   ": "", "!!!": "",
	}
	for in, want := range cases {
		if got := RuntimeBundle(in); got != want {
			t.Errorf("RuntimeBundle(%q) = %q, want %q", in, got, want)
		}
		if IsSurveyRuntimeLanguage(in) != (want != "") {
			t.Errorf("IsSurveyRuntimeLanguage(%q) disagrees with RuntimeBundle", in)
		}
	}
}

// forma2's toLegacyLanguageCodes cases (apps/web/lib/i18n/i18n.test.ts), plus aliases that only the canonical map surfaces.
func TestLegacyCodes(t *testing.T) {
	cases := map[string][]string{
		"de-DE": {"de"}, "fil-PH": {"tl"}, "ak-GH": {"ak", "tw"}, "zh-Hant-TW": {"zh-Hant", "zh-TW"}, "zh-Hans-CN": {"zh", "zh-CN", "zh-Hans"},
		"de-AT": nil, "xx-YY": nil,
	}
	for code, want := range cases {
		if got := LegacyCodes(code); !slices.Equal(got, want) {
			t.Errorf("LegacyCodes(%q) = %v, want %v", code, got, want)
		}
	}
	aliases := LegacyCodes("de-DE")
	aliases[0] = "MUTATED"
	if got := LegacyCodes("de-DE"); !slices.Equal(got, []string{"de"}) {
		t.Fatalf("a caller's change leaked into the table: %v", got)
	}
}
