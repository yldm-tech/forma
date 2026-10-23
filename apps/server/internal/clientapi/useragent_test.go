package clientapi

import (
	"testing"

	"github.com/yldm-tech/forma/server/internal/response"
)

// What ua-parser-js 2.0.10's rules give for the common browsers, read from its regexes.
func TestParseUserAgent(t *testing.T) {
	cases := map[string]response.UserAgent{
		browserUA: {Browser: "Chrome", OS: "macOS"},
		"Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1": {Browser: "Mobile Safari", OS: "iOS", Device: "mobile"},
		"Mozilla/5.0 (iPad; CPU OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1":          {Browser: "Mobile Safari", OS: "iOS", Device: "tablet"},
		"Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Mobile Safari/537.36":                   {Browser: "Mobile Chrome", OS: "Android", Device: "mobile"},
		"Mozilla/5.0 (Linux; Android 13; SM-X700) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36":                          {Browser: "Chrome", OS: "Android", Device: "tablet"},
		"Mozilla/5.0 (Linux; Android 13; SAMSUNG SM-S918B) AppleWebKit/537.36 (KHTML, like Gecko) SamsungBrowser/21.0 Chrome/110.0.5481.154 Mobile Safari/537.36": {
			Browser: "Samsung Internet", OS: "Android", Device: "mobile",
		},
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36 Edg/120.0.2210.91": {Browser: "Edge", OS: "Windows"},
		"Mozilla/5.0 (X11; Linux x86_64; rv:121.0) Gecko/20100101 Firefox/121.0":                                                            {Browser: "Firefox", OS: "Linux"},
		"Mozilla/5.0 (X11; Ubuntu; Linux x86_64; rv:121.0) Gecko/20100101 Firefox/121.0":                                                    {Browser: "Firefox", OS: "Ubuntu"},
		"Mozilla/5.0 (X11; CrOS x86_64 14541.0.0) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36":                    {Browser: "Chrome", OS: "Chrome OS"},
		"Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148 [FBAN/FBIOS;FBAV/450.0.0.38.108;FBBV/123]": {
			Browser: "Facebook", OS: "iOS", Device: "mobile",
		},
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15": {Browser: "Safari", OS: "macOS"},
		"": {},
	}
	for ua, want := range cases {
		if got := parseUserAgent(ua); got != want {
			t.Errorf("%q: %+v, want %+v", ua, got, want)
		}
	}
}
