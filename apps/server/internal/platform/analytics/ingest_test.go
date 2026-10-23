package analytics

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestIngestProxy: /ingest/static/* goes to the asset host and anything else to the API host, with the path below /ingest and the query kept, and without this origin's cookies or credentials either way.
func TestIngestProxy(t *testing.T) {
	type seen struct{ host, path, query, cookie, auth string }
	got := map[string]seen{}
	upstream := func(name string) *url.URL {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got[name] = seen{r.Host, r.URL.Path, r.URL.RawQuery, r.Header.Get("Cookie"), r.Header.Get("Authorization")}
			http.SetCookie(w, &http.Cookie{Name: "ph", Value: "x"})
			_, _ = io.WriteString(w, name)
		}))
		t.Cleanup(srv.Close)
		u, _ := url.Parse(srv.URL)
		return u
	}
	api, assets := upstream("api"), upstream("assets")
	previousAPI, previousAssets := ingestAPI, ingestAssets
	ingestAPI, ingestAssets = api, assets
	t.Cleanup(func() { ingestAPI, ingestAssets = previousAPI, previousAssets })

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Any(IngestPath+"/*path", IngestProxy())
	// A real server: the proxy watches for the client going away, which a recorder cannot report.
	app := httptest.NewServer(r)
	t.Cleanup(app.Close)
	for _, tc := range []struct{ method, path, upstream, wantPath string }{
		{http.MethodPost, "/ingest/e/?ip=1&ver=1.2", "api", "/e/"},
		{http.MethodGet, "/ingest/static/array.js", "assets", "/static/array.js"},
		{http.MethodPost, "/ingest/flags/", "api", "/flags/"},
	} {
		req, _ := http.NewRequestWithContext(t.Context(), tc.method, app.URL+tc.path, nil)
		req.Header.Set("Cookie", "__Secure-better-auth.session_token=secret")
		req.Header.Set("Authorization", "Bearer secret")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if string(body) != tc.upstream || res.Header.Get("Set-Cookie") != "" {
			t.Errorf("%s answered %q with Set-Cookie %q, want %s's answer without cookies", tc.path, body, res.Header.Get("Set-Cookie"), tc.upstream)
		}
		s := got[tc.upstream]
		if s.path != tc.wantPath || s.query != req.URL.RawQuery || s.cookie != "" || s.auth != "" {
			t.Errorf("%s reached %s as %+v, want path %s and no credentials", tc.path, tc.upstream, s, tc.wantPath)
		}
	}
}
