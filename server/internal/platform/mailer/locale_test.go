package mailer

import "testing"

// forma2's locale.test.ts.
func TestMatchLocale(t *testing.T) {
	cases := map[string]string{
		"":                       "en-US",
		"ja-JP":                  "ja-JP",
		"ja":                     "ja-JP",
		"fr-FR,ja-JP;q=0.9":      "ja-JP",
		"fr-FR, ja-JP;q=0.9":     "ja-JP",
		"de;q=0.5, ja;q=0.8":     "ja-JP",
		"zh-TW":                  "zh-Hant-TW",
		"zh-HK":                  "zh-Hant-TW",
		"zh-CN":                  "zh-Hans-CN",
		"zh-SG":                  "zh-Hans-CN",
		"zh":                     "zh-Hans-CN",
		"zh-Hant":                "zh-Hant-TW",
		"ja;q=0, en;q=0.1":       "en-US",
		"*":                      "en-US",
		"fr-FR,de-DE":            "en-US",
		"en-GB,ja;q=0.9":         "en-US",
		"ja-JP;q=0.5, zh-TW;q=1": "zh-Hant-TW",
	}
	for header, want := range cases {
		if got := MatchLocale(header); got != want {
			t.Errorf("MatchLocale(%q) = %q, want %q", header, got, want)
		}
	}
}
