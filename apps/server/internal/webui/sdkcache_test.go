package webui

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpserver"
)

const runtimeBundle = "window.formaSurveys={}"

// contentHash is the hash the handler gives a file: the first 8 bytes of its SHA-256, in hex.
func contentHash(data string) string {
	sum := sha256.Sum256([]byte(data))
	return hex.EncodeToString(sum[:8])
}

func sdkFiles() fstest.MapFS {
	return fstest.MapFS{
		"index.html":         {Data: []byte(indexHTML)},
		"js/surveys.umd.js":  {Data: []byte(runtimeBundle)},
		"js/surveys.umd.cjs": {Data: []byte(runtimeBundle)},
		"js/forma.umd.js":    {Data: []byte("window.forma={}")},
		"js/surveys-locales/de-DE.0123456789ab.json":      {Data: []byte(`{"common":{"required":"Pflichtfeld"}}`)},
		"js/surveys-locales/zh-Hant-TW.ba9876543210.json": {Data: []byte(`{"common":{"required":"必填"}}`)},
	}
}

func getWith(r http.Handler, path string, header ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// A browser that kept /js/surveys.umd.js past its hour asks again with the ETag it got; an unchanged bundle is a 304 without a body instead of the whole runtime again.
func TestSDKBundlesRevalidateWithTheirETag(t *testing.T) {
	r := newEngine(t, sdkFiles())
	want := `W/"` + contentHash(runtimeBundle) + `"`
	for _, p := range []string{"/js/surveys.umd.js", "/js/surveys.umd.cjs"} {
		first := get(r, http.MethodGet, p)
		if first.Code != http.StatusOK || first.Header().Get("ETag") != want {
			t.Fatalf("%s: code %d ETag %q, want 200 %s", p, first.Code, first.Header().Get("ETag"), want)
		}
		again := getWith(r, p, "If-None-Match", want)
		if again.Code != http.StatusNotModified || again.Body.Len() != 0 {
			t.Fatalf("%s revalidated: code %d with %d bytes, want 304 without a body", p, again.Code, again.Body.Len())
		}
		if got := again.Header().Get("Cache-Control"); got != "public, max-age=3600, s-maxage=600, stale-while-revalidate=3600, stale-if-error=86400" {
			t.Errorf("%s revalidated: Cache-Control %q", p, got)
		}
		if changed := getWith(r, p, "If-None-Match", `W/"0000000000000000"`); changed.Code != http.StatusOK || changed.Body.String() != runtimeBundle {
			t.Errorf("%s with a stale ETag: code %d, want 200 with the bundle", p, changed.Code)
		}
	}
	if other := get(r, http.MethodGet, "/js/forma.umd.js").Header().Get("ETag"); other == want || other == "" {
		t.Errorf("the SDK's ETag %q must be its own content's", other)
	}
}

// Behind the gzip middleware the ETag still holds, and a revalidation is still a 304.
func TestSDKBundleETagSurvivesCompression(t *testing.T) {
	big := make([]byte, 0, 4096)
	for len(big) < 4000 {
		big = append(big, runtimeBundle...)
	}
	h, err := NewFromFS(fstest.MapFS{"index.html": {Data: []byte(indexHTML)}, "js/surveys.umd.js": {Data: big}})
	if err != nil {
		t.Fatal(err)
	}
	engine := httpserver.NewEngine(config.Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	h.Register(engine)
	server := httpserver.Compress(engine)
	sum := sha256.Sum256(big)
	want := `W/"` + hex.EncodeToString(sum[:8]) + `"`

	rec := getWith(server, "/js/surveys.umd.js", "Accept-Encoding", "gzip")
	if rec.Header().Get("Content-Encoding") != "gzip" || rec.Header().Get("ETag") != want {
		t.Fatalf("Content-Encoding %q ETag %q, want gzip %s", rec.Header().Get("Content-Encoding"), rec.Header().Get("ETag"), want)
	}
	if again := getWith(server, "/js/surveys.umd.js", "Accept-Encoding", "gzip", "If-None-Match", want); again.Code != http.StatusNotModified {
		t.Fatalf("revalidation: code %d, want 304", again.Code)
	}
}

// The public survey pages load the runtime by a URL pinned to its content, which is cached for good; a pin naming other content (a page rendered by a server with another build, mid-deploy) is answered but never stored.
func TestPinnedSDKURLs(t *testing.T) {
	files := sdkFiles()
	h, err := NewFromFS(files)
	if err != nil {
		t.Fatal(err)
	}
	pinned := h.VersionedPath("js/surveys.umd.js")
	if want := "/js/surveys.umd.js?hash=" + contentHash(runtimeBundle); pinned != want {
		t.Fatalf("VersionedPath = %q, want %q", pinned, want)
	}
	if got := h.VersionedPath("js/missing.js"); got != "/js/missing.js" {
		t.Errorf("VersionedPath of a file the build lacks = %q, want the plain path", got)
	}

	r := newEngine(t, files)
	for path, cacheControl := range map[string]string{
		pinned: "public, max-age=31536000, immutable",
		"/js/surveys.umd.js?hash=0000000000000000": "no-store",
		"/js/surveys.umd.js?hash=":                 "no-store",
		// Another parameter, such as a customer's own cache buster on an embed snippet, leaves the bundle's usual caching alone.
		"/js/surveys.umd.js?v=3": "public, max-age=3600, s-maxage=600, stale-while-revalidate=3600, stale-if-error=86400",
	} {
		rec := get(r, http.MethodGet, path)
		if rec.Code != http.StatusOK || rec.Body.String() != runtimeBundle {
			t.Errorf("%s: code %d body %q", path, rec.Code, rec.Body.String())
		}
		if got := rec.Header().Get("Cache-Control"); got != cacheControl {
			t.Errorf("%s: Cache-Control %q, want %q", path, got, cacheControl)
		}
		if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
			t.Errorf("%s: no CORS header", path)
		}
	}
}

// The runtime's translations are named by their content, so they are cached for good, and customer pages fetch them cross-origin.
func TestSurveyLocaleFiles(t *testing.T) {
	h, err := NewFromFS(sdkFiles())
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"de-DE":      "/js/surveys-locales/de-DE.0123456789ab.json",
		"zh-Hant-TW": "/js/surveys-locales/zh-Hant-TW.ba9876543210.json",
	}
	if got := h.SurveyLocalePaths(); !maps.Equal(got, want) {
		t.Fatalf("SurveyLocalePaths = %v, want %v", got, want)
	}

	r := newEngine(t, sdkFiles())
	rec := get(r, http.MethodGet, want["de-DE"])
	if rec.Code != http.StatusOK || rec.Body.String() != `{"common":{"required":"Pflichtfeld"}}` {
		t.Fatalf("code %d body %q", rec.Code, rec.Body.String())
	}
	for k, v := range map[string]string{
		"Cache-Control":               "public, max-age=31536000, immutable",
		"Access-Control-Allow-Origin": "*",
		"Content-Type":                "application/json",
	} {
		if got := rec.Header().Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}

	if without, err := NewFromFS(fstest.MapFS{"index.html": {Data: []byte(indexHTML)}}); err != nil || len(without.SurveyLocalePaths()) != 0 || without.VersionedPath("js/surveys.umd.js") != "/js/surveys.umd.js" {
		t.Fatalf("a build without js/: %v", err)
	}
}

// A runtime that a browser or CDN cached under its fixed name from an earlier build asks for its locale files by that build's hashes. They are answered with this build's file for the same language, never stored, rather than a 404 that would render the survey's runtime strings in English.
func TestSurveyLocaleFileByAnotherBuildsHash(t *testing.T) {
	r := newEngine(t, sdkFiles())
	for _, p := range []string{
		"/js/surveys-locales/de-DE.000000000000.json",
		"/js/surveys-locales/de-DE.json",
		"/app/js/surveys-locales/de-DE.ffffffffffff.json",
	} {
		rec := get(r, http.MethodGet, p)
		if rec.Code != http.StatusOK || rec.Body.String() != `{"common":{"required":"Pflichtfeld"}}` {
			t.Fatalf("%s: code %d body %q, want this build's de-DE", p, rec.Code, rec.Body.String())
		}
		for k, v := range map[string]string{
			"Cache-Control":               "no-store",
			"Access-Control-Allow-Origin": "*",
			"Content-Type":                "application/json",
		} {
			if got := rec.Header().Get(k); got != v {
				t.Errorf("%s: %s = %q, want %q", p, k, got, v)
			}
		}
	}
	for _, p := range []string{
		// A language this build does not ship, and names that are not a locale file.
		"/js/surveys-locales/fr-FR.000000000000.json",
		"/js/surveys-locales/de-DE.000000000000.js",
		"/js/surveys-locales/x/de-DE.000000000000.json",
		"/js/de-DE.000000000000.json",
	} {
		if rec := get(r, http.MethodGet, p); rec.Code != http.StatusNotFound {
			t.Errorf("%s: code %d, want 404", p, rec.Code)
		}
	}
}
