package webui

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"

	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpserver"
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
		{"the app's root serves index", http.MethodGet, "/app/", http.StatusOK, indexHTML, "no-cache"},
		{"the app's base without a slash serves index", http.MethodGet, "/app", http.StatusOK, indexHTML, "no-cache"},
		{"client route falls back to index", http.MethodGet, "/app/workspaces/abc/surveys", http.StatusOK, indexHTML, "no-cache"},
		{"fingerprinted asset is immutable", http.MethodGet, "/app/assets/app-1a2b.js", http.StatusOK, "console.log(1)", "public, max-age=31536000, immutable"},
		{"other static file revalidates", http.MethodGet, "/app/robots.txt", http.StatusOK, "User-agent", "no-cache"},
		{"missing asset is 404, not index", http.MethodGet, "/app/assets/app-old.js", http.StatusNotFound, "", ""},
		{"assets are not served at the root", http.MethodGet, "/assets/app-1a2b.js", http.StatusNotFound, "", ""},
		{"a file outside the root list is not served at the root", http.MethodGet, "/robots.txt", http.StatusNotFound, "", ""},
		{"index is not served at the root", http.MethodGet, "/index.html", http.StatusNotFound, "", ""},
		{"an unknown root path is 404, not the app", http.MethodGet, "/pricing", http.StatusNotFound, "", ""},
		{"a path that only starts like the base is not the app", http.MethodGet, "/apple", http.StatusNotFound, "", ""},
		{"unknown API route is JSON 404", http.MethodGet, "/api/v3/nope", http.StatusNotFound, `"code":"not_found"`, ""},
		{"POST to a page is rejected", http.MethodPost, "/app/workspaces", http.StatusMethodNotAllowed, "", ""},
		{"path traversal stays inside the build", http.MethodGet, "/app/../../etc/passwd", http.StatusOK, indexHTML, "no-cache"},
		{"path traversal at the root finds nothing", http.MethodGet, "/../../etc/passwd", http.StatusNotFound, "", ""},
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

// The admin pages forma2 and this SPA served at the root answer with a permanent redirect to the same path under /app, keeping the query and the method; the root leads to the app with a redirect browsers do not cache for good.
func TestRedirectsToTheApp(t *testing.T) {
	r := newEngine(t, fstest.MapFS{"index.html": {Data: []byte(indexHTML)}})

	cases := []struct {
		method, path string
		code         int
		location     string
	}{
		{http.MethodGet, "/", http.StatusFound, "/app/"},
		{http.MethodGet, "/?utm_source=x", http.StatusFound, "/app/?utm_source=x"},
		{http.MethodGet, "/auth/login?callbackUrl=https%3A%2F%2Fforma.example%2Fworkspaces%2Fw1", http.StatusPermanentRedirect, "/app/auth/login?callbackUrl=https%3A%2F%2Fforma.example%2Fworkspaces%2Fw1"},
		{http.MethodGet, "/workspaces/w1/surveys/s1/summary", http.StatusPermanentRedirect, "/app/workspaces/w1/surveys/s1/summary"},
		{http.MethodGet, "/workspaces/w1/", http.StatusPermanentRedirect, "/app/workspaces/w1/"},
		{http.MethodGet, "/invite?token=abc", http.StatusPermanentRedirect, "/app/invite?token=abc"},
		{http.MethodGet, "/environments/e1/surveys", http.StatusPermanentRedirect, "/app/environments/e1/surveys"},
		{http.MethodGet, "/i/workspaces?ref=mail", http.StatusPermanentRedirect, "/app/i/workspaces?ref=mail"},
		{http.MethodGet, "/setup", http.StatusPermanentRedirect, "/app/setup"},
		{http.MethodGet, "/billing-confirmation?organizationId=o1&checkout_success=1", http.StatusPermanentRedirect, "/app/billing-confirmation?organizationId=o1&checkout_success=1"},
		{http.MethodGet, "/verify-email-change?token=t", http.StatusPermanentRedirect, "/app/verify-email-change?token=t"},
		{http.MethodGet, "/workspaces/caf%C3%A9%2Fx", http.StatusPermanentRedirect, "/app/workspaces/caf%C3%A9%2Fx"},
		{http.MethodHead, "/account/settings/profile", http.StatusPermanentRedirect, "/app/account/settings/profile"},
		{http.MethodPost, "/organizations/o1/settings", http.StatusPermanentRedirect, "/app/organizations/o1/settings"},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			rec := get(r, tc.method, tc.path)
			if rec.Code != tc.code || rec.Header().Get("Location") != tc.location {
				t.Fatalf("%d %s, want %d %s", rec.Code, rec.Header().Get("Location"), tc.code, tc.location)
			}
		})
	}
	for _, first := range LegacyAdminPaths {
		if rec := get(r, http.MethodGet, "/"+first+"/x?y=1"); rec.Code != http.StatusPermanentRedirect || rec.Header().Get("Location") != "/app/"+first+"/x?y=1" {
			t.Errorf("/%s/x?y=1 = %d %s", first, rec.Code, rec.Header().Get("Location"))
		}
	}
}

// Every top-level entry of apps/web/public is either served at the root or deliberately left to /app, and every root path names something the build has; the SDK bundles under js/ are copied in by the package builds and are not committed.
func TestRootPublicPathsCoverThePublicDirectory(t *testing.T) {
	entries, err := os.ReadDir(filepath.FromSlash("../../../web/public"))
	if err != nil {
		t.Fatal(err)
	}
	present := map[string]bool{"js": true}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		present[e.Name()] = true
		if !slices.Contains(RootPublicPaths, e.Name()) {
			t.Errorf("apps/web/public/%s is served under /app only; add it to RootPublicPaths (and the ingress) if anything outside the SPA loads it", e.Name())
		}
	}
	for _, p := range RootPublicPaths {
		if !present[p] {
			t.Errorf("RootPublicPaths lists %s, which apps/web/public does not have", p)
		}
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

// The SPA's document may compile WebAssembly, which its HEIC conversion needs; the files beside it, the SDK bundles first, keep forma2's policy.
func TestOnlyTheDocumentAllowsWebAssembly(t *testing.T) {
	h, err := NewFromFS(fstest.MapFS{
		"index.html":         {Data: []byte(indexHTML)},
		"assets/app-1a2b.js": {Data: []byte("console.log(1)")},
		"js/surveys.umd.cjs": {Data: []byte("window.formaSurveys={}")},
	})
	if err != nil {
		t.Fatal(err)
	}
	r := httpserver.NewEngine(config.Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	h.Register(r)
	for path, want := range map[string]bool{"/app/": true, "/app/workspaces/1/surveys/2/edit": true, "/app/assets/app-1a2b.js": false, "/js/surveys.umd.cjs": false, "/api/v3/nope": false} {
		csp := get(r, http.MethodGet, path).Header().Get("Content-Security-Policy")
		if !strings.HasPrefix(csp, "default-src 'self'; ") {
			t.Fatalf("%s: Content-Security-Policy = %q, want forma2's policy", path, csp)
		}
		if got := strings.Contains(csp, "script-src 'self' 'unsafe-inline' 'wasm-unsafe-eval' https:;"); got != want {
			t.Errorf("%s: Content-Security-Policy = %q, allows WebAssembly: %v, want %v", path, csp, got, want)
		}
	}
}

// Behind the server's middleware the SPA's document and the root redirect say no-transform, so Cloudflare does not inject its Web Analytics beacon into them; the bundles and API answers keep their caching as it was, so Cloudflare still compresses them.
func TestOnlyHTMLIsNoTransform(t *testing.T) {
	h, err := NewFromFS(fstest.MapFS{
		"index.html":         {Data: []byte(indexHTML)},
		"assets/app-1a2b.js": {Data: []byte("console.log(1)")},
		"js/surveys.umd.cjs": {Data: []byte("window.formaSurveys={}")},
	})
	if err != nil {
		t.Fatal(err)
	}
	r := httpserver.NewEngine(config.Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	h.Register(r)
	for path, want := range map[string]string{
		"/app/":                   "no-cache, no-transform",
		"/app/workspaces/1":       "no-cache, no-transform",
		"/":                       "no-store, no-transform",
		"/app/assets/app-1a2b.js": "public, max-age=31536000, immutable",
		"/js/surveys.umd.cjs":     "public, max-age=3600, s-maxage=2592000, stale-while-revalidate=3600, stale-if-error=86400",
	} {
		if got := get(r, http.MethodGet, path).Header().Get("Cache-Control"); got != want {
			t.Errorf("%s: Cache-Control = %q, want %q", path, got, want)
		}
	}
	api := get(r, http.MethodGet, "/api/v3/nope")
	if !strings.HasPrefix(api.Header().Get("Content-Type"), "application/") || strings.Contains(api.Header().Get("Cache-Control"), "no-transform") {
		t.Errorf("/api/v3/nope: Content-Type %q, Cache-Control %q", api.Header().Get("Content-Type"), api.Header().Get("Cache-Control"))
	}
}

func TestServeWithoutBuild(t *testing.T) {
	r := newEngine(t, fstest.MapFS{".gitkeep": {}})

	if rec := get(r, http.MethodGet, "/app/"); rec.Code != http.StatusNotFound {
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
	for _, p := range []string{"/survey.html", "/app/survey.html"} {
		if rec := get(newEngine(t, files), http.MethodGet, p); rec.Code != http.StatusNotFound {
			t.Fatalf("%s answered %d", p, rec.Code)
		}
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

// Behind Compress, the SDK bundles, the SPA's assets and its document are gzipped for a client that accepts gzip, under the same names and with the same cache headers as uncompressed.
func TestBuildIsCompressed(t *testing.T) {
	bundle := []byte(strings.Repeat("window.formaSurveys={};", 100))
	h, err := NewFromFS(fstest.MapFS{
		"index.html":             {Data: []byte(indexHTML + strings.Repeat("<p>Forma</p>", 100))},
		"assets/survey-1a2b.js":  {Data: bundle},
		"assets/survey-1a2b.css": {Data: []byte(strings.Repeat("body{margin:0}", 100))},
		"js/surveys.umd.js":      {Data: bundle},
		"js/surveys.umd.cjs":     {Data: bundle},
	})
	if err != nil {
		t.Fatal(err)
	}
	r := httpserver.NewEngine(config.Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	h.Register(r)
	server := httpserver.Compress(r)

	sdk := "public, max-age=3600, s-maxage=2592000, stale-while-revalidate=3600, stale-if-error=86400"
	for path, cacheControl := range map[string]string{
		"/js/surveys.umd.js":          sdk,
		"/js/surveys.umd.cjs":         sdk,
		"/app/assets/survey-1a2b.js":  "public, max-age=31536000, immutable",
		"/app/assets/survey-1a2b.css": "public, max-age=31536000, immutable",
		"/app/":                       "no-cache, no-transform",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Accept-Encoding", "gzip, deflate, br, zstd")
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || rec.Header().Get("Content-Encoding") != "gzip" {
			t.Errorf("%s: code %d, Content-Encoding %q, want 200 gzip", path, rec.Code, rec.Header().Get("Content-Encoding"))
		}
		if got := rec.Header().Get("Cache-Control"); got != cacheControl {
			t.Errorf("%s: Cache-Control = %q, want %q", path, got, cacheControl)
		}
		if got := rec.Header().Get("Vary"); got != "Accept-Encoding" {
			t.Errorf("%s: Vary = %q, want Accept-Encoding", path, got)
		}
	}
}
