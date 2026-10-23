package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"

	"github.com/yldm-tech/forma/server/internal/platform/config"
)

const indexHTML = "<!doctype html><title>Forma</title>"

func newEngine(t *testing.T, files fstest.MapFS) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	h, err := NewFromFS(files)
	if err != nil {
		t.Fatalf("NewFromFS: %v", err)
	}
	r := gin.New()
	h.Register(r)
	return r
}

func get(r http.Handler, method, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func TestServe(t *testing.T) {
	r := newEngine(t, fstest.MapFS{
		"index.html":         {Data: []byte(indexHTML)},
		"assets/app-1a2b.js": {Data: []byte("console.log(1)")},
		"robots.txt":         {Data: []byte("User-agent: *")},
	})

	cases := []struct {
		name, method, path string
		wantCode           int
		wantBody           string
		wantCache          string
	}{
		{"root serves index", http.MethodGet, "/", http.StatusOK, indexHTML, "no-cache"},
		{"client route falls back to index", http.MethodGet, "/workspaces/abc/surveys", http.StatusOK, indexHTML, "no-cache"},
		{"fingerprinted asset is immutable", http.MethodGet, "/assets/app-1a2b.js", http.StatusOK, "console.log(1)", "public, max-age=31536000, immutable"},
		{"other static file revalidates", http.MethodGet, "/robots.txt", http.StatusOK, "User-agent", "no-cache"},
		{"missing asset is 404, not index", http.MethodGet, "/assets/app-old.js", http.StatusNotFound, "", ""},
		{"unknown API route is JSON 404", http.MethodGet, "/api/v3/nope", http.StatusNotFound, `"code":"not_found"`, ""},
		{"POST to a page is rejected", http.MethodPost, "/workspaces", http.StatusMethodNotAllowed, "", ""},
		{"path traversal stays inside the build", http.MethodGet, "/../../etc/passwd", http.StatusOK, indexHTML, "no-cache"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := get(r, tc.method, tc.path)
			if rec.Code != tc.wantCode {
				t.Fatalf("code = %d, want %d (body %q)", rec.Code, tc.wantCode, rec.Body.String())
			}
			if tc.wantBody != "" && !strings.Contains(rec.Body.String(), tc.wantBody) {
				t.Errorf("body = %q, want it to contain %q", rec.Body.String(), tc.wantBody)
			}
			if tc.wantCache != "" && rec.Header().Get("Cache-Control") != tc.wantCache {
				t.Errorf("Cache-Control = %q, want %q", rec.Header().Get("Cache-Control"), tc.wantCache)
			}
		})
	}
}

func TestServeSDKBundles(t *testing.T) {
	r := newEngine(t, fstest.MapFS{
		"index.html":         {Data: []byte(indexHTML)},
		"js/surveys.umd.cjs": {Data: []byte("window.formaSurveys={}")},
		"js/forma.umd.cjs":   {Data: []byte("window.forma={}")},
		"js/surveys.umd.js":  {Data: []byte("window.formaSurveys={}")},
		"js/forma.d.ts":      {Data: []byte("export {}")},
	})

	for _, p := range []string{"/js/surveys.umd.cjs", "/js/forma.umd.cjs", "/js/surveys.umd.js"} {
		t.Run(p, func(t *testing.T) {
			rec := get(r, http.MethodGet, p)
			if rec.Code != http.StatusOK {
				t.Fatalf("code = %d, want 200", rec.Code)
			}
			want := map[string]string{
				"Content-Type":                "application/javascript; charset=UTF-8",
				"Cache-Control":               "public, max-age=3600, s-maxage=2592000, stale-while-revalidate=3600, stale-if-error=86400",
				"Access-Control-Allow-Origin": "*",
				"Vary":                        "Accept-Encoding",
			}
			for k, v := range want {
				if got := rec.Header().Get(k); got != v {
					t.Errorf("%s = %q, want %q", k, got, v)
				}
			}
		})
	}

	t.Run("type declarations keep their own content type", func(t *testing.T) {
		rec := get(r, http.MethodGet, "/js/forma.d.ts")
		if strings.HasPrefix(rec.Header().Get("Content-Type"), "application/javascript") {
			t.Fatalf("Content-Type = %q, want no JavaScript override for .d.ts", rec.Header().Get("Content-Type"))
		}
	})
}

// The files copied from forma2's apps/web/public keep the cache headers forma2's next.config.mjs gave them, and a content type the distroless image cannot look up in a system table.
func TestServePublicAssets(t *testing.T) {
	r := newEngine(t, fstest.MapFS{
		"index.html":                                         {Data: []byte(indexHTML)},
		"animated-bgs/4K/1_4k.mp4":                           {Data: []byte("\x00\x00\x00\x20ftypisom")},
		"animated-bgs/Thumbnails/1_Thumb.mp4":                {Data: []byte("\x00\x00\x00\x20ftypisom")},
		"image-backgrounds/dogs.webp":                        {Data: []byte("RIFF\x00\x00\x00\x00WEBPVP8 ")},
		"logo-transparent.png":                               {Data: []byte("\x89PNG\r\n\x1a\n")},
		"favicon.ico":                                        {Data: []byte("\x00\x00\x01\x00")},
		"favicon/apple-touch-icon.png":                       {Data: []byte("\x89PNG\r\n\x1a\n")},
		"favicon/safari-pinned-tab.svg":                      {Data: []byte("<svg></svg>")},
		"favicon/site.webmanifest":                           {Data: []byte("{}")},
		"sample-csv/forma-organization-members-template.csv": {Data: []byte("Full Name,Email Address,Role")},
		"star-icons/star.png":                                {Data: []byte("\x89PNG\r\n\x1a\n")},
	})

	const (
		favicon    = "public, max-age=2592000, s-maxage=31536000, immutable"
		animated   = "public, max-age=604800, s-maxage=31536000, immutable"
		background = "public, max-age=86400, s-maxage=2592000, stale-while-revalidate=86400"
		csv        = "public, max-age=3600, s-maxage=86400, stale-while-revalidate=3600"
	)
	cases := []struct {
		path string
		want map[string]string
	}{
		{"/animated-bgs/4K/1_4k.mp4", map[string]string{"Cache-Control": animated, "Content-Type": "video/mp4", "Access-Control-Allow-Origin": "*", "Accept-Ranges": "bytes"}},
		{"/animated-bgs/Thumbnails/1_Thumb.mp4", map[string]string{"Cache-Control": animated, "Content-Type": "video/mp4"}},
		{"/image-backgrounds/dogs.webp", map[string]string{"Cache-Control": background, "Content-Type": "image/webp", "Access-Control-Allow-Origin": "*", "Vary": "Accept-Encoding"}},
		{"/logo-transparent.png", map[string]string{"Cache-Control": "no-cache", "Content-Type": "image/png"}},
		{"/star-icons/star.png", map[string]string{"Cache-Control": "no-cache", "Content-Type": "image/png"}},
		{"/favicon.ico", map[string]string{"Cache-Control": favicon, "Content-Type": "image/x-icon", "Access-Control-Allow-Origin": "*"}},
		{"/favicon/apple-touch-icon.png", map[string]string{"Cache-Control": favicon, "Content-Type": "image/png", "Access-Control-Allow-Origin": "*"}},
		{"/favicon/safari-pinned-tab.svg", map[string]string{"Cache-Control": favicon, "Content-Type": "image/svg+xml"}},
		{"/favicon/site.webmanifest", map[string]string{"Cache-Control": favicon, "Content-Type": "application/manifest+json"}},
		{"/sample-csv/forma-organization-members-template.csv", map[string]string{"Cache-Control": csv, "Content-Type": "text/csv", "Access-Control-Allow-Origin": "*"}},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			rec := get(r, http.MethodGet, tc.path)
			if rec.Code != http.StatusOK {
				t.Fatalf("code = %d, want 200", rec.Code)
			}
			for k, v := range tc.want {
				if got := rec.Header().Get(k); got != v {
					t.Errorf("%s = %q, want %q", k, got, v)
				}
			}
		})
	}

	t.Run("a video is served in ranges", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/animated-bgs/4K/1_4k.mp4", nil)
		req.Header.Set("Range", "bytes=4-7")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusPartialContent || rec.Body.String() != "ftyp" {
			t.Fatalf("code = %d, body = %q, want 206 with the requested bytes", rec.Code, rec.Body.String())
		}
	})

	t.Run("a missing background is 404, not the app", func(t *testing.T) {
		if rec := get(r, http.MethodGet, "/image-backgrounds/missing.webp"); rec.Code != http.StatusNotFound {
			t.Fatalf("code = %d, want 404", rec.Code)
		}
	})
}

func TestServeWithoutBuild(t *testing.T) {
	r := newEngine(t, fstest.MapFS{".gitkeep": {}})

	if rec := get(r, http.MethodGet, "/"); rec.Code != http.StatusNotFound {
		t.Fatalf("code = %d, want 404 when no index.html is embedded", rec.Code)
	}
}

// The survey page bundle's HTML is a template the public survey pages fill in; it is handed to them and never served as it is.
func TestSurveyShell(t *testing.T) {
	files := fstest.MapFS{
		"index.html":  {Data: []byte(indexHTML)},
		"survey.html": {Data: []byte("<!doctype html><!--forma:head--><!--forma:state-->")},
	}
	h, err := NewFromFS(files)
	if err != nil {
		t.Fatal(err)
	}
	if string(h.SurveyShell()) != "<!doctype html><!--forma:head--><!--forma:state-->" {
		t.Fatalf("SurveyShell = %q", h.SurveyShell())
	}
	if rec := get(newEngine(t, files), http.MethodGet, "/survey.html"); rec.Code != http.StatusNotFound {
		t.Fatalf("/survey.html answered %d", rec.Code)
	}
	without, err := NewFromFS(fstest.MapFS{"index.html": {Data: []byte(indexHTML)}})
	if err != nil || without.SurveyShell() != nil {
		t.Fatalf("a build without the survey page: %q, %v", without.SurveyShell(), err)
	}
}

func TestRuntimeConfigIsWrittenIntoThePage(t *testing.T) {
	index := []byte("<html><head><title>Forma</title></head><body></body></html>")
	if got := withRuntimeConfig(index, config.Config{}); string(got) != string(index) {
		t.Errorf("page without Sentry or PostHog = %s, want it unchanged", got)
	}
	var cfg config.Config
	cfg.Telemetry.SentryDSN = "https://public@o0.ingest.sentry.io/1?x=</script><script>alert(1)</script>"
	cfg.Telemetry.SentryEnvironment = "staging"
	cfg.Telemetry.PostHogKey = "phc_key"
	got := string(withRuntimeConfig(index, cfg))
	for _, want := range []string{
		`<head><title>Forma</title><script id="forma-runtime-config">window.__formaSentryClientConfig={"dsn":"https://public@o0.ingest.sentry.io/1?x=\u003c/script\u003e\u003cscript\u003ealert(1)\u003c/script\u003e","environment":"staging"};`,
		`window.__formaPostHogConfig={"key":"phc_key","apiHost":"/ingest","uiHost":"https://eu.i.posthog.com"};</script></head>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("page = %s, want it to contain %s", got, want)
		}
	}
	if strings.Count(got, "</script>") != 1 {
		t.Errorf("a configured value closed the script element: %s", got)
	}
}
