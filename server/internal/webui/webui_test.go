package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"
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
		"favicon.ico":        {Data: []byte("ico")},
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
		{"other static file revalidates", http.MethodGet, "/favicon.ico", http.StatusOK, "ico", "no-cache"},
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
