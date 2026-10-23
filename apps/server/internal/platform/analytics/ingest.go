package analytics

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
)

// IngestPath is where the browser sends PostHog's requests, forma2's /ingest rewrite: first-party, so content blockers that drop third-party analytics hosts do not drop them.
const IngestPath = "/ingest"

var (
	ingestAPI    = &url.URL{Scheme: "https", Host: "eu.i.posthog.com"}
	ingestAssets = &url.URL{Scheme: "https", Host: "eu-assets.i.posthog.com"}
)

// IngestProxy forwards /ingest/static/* to PostHog's asset host and every other /ingest/* path to its API host. The browser's cookies and credentials for this origin are not passed on, and PostHog cannot set any here.
func IngestProxy() gin.HandlerFunc {
	proxy := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			rest := strings.TrimPrefix(r.In.URL.Path, IngestPath)
			target := ingestAPI
			if strings.HasPrefix(rest, "/static/") {
				target = ingestAssets
			}
			r.SetURL(target)
			r.Out.URL.Path, r.Out.URL.RawPath = rest, ""
			r.Out.Host = target.Host
			r.Out.Header.Del("Cookie")
			r.Out.Header.Del("Authorization")
			r.SetXForwarded()
		},
		ModifyResponse: func(res *http.Response) error {
			res.Header.Del("Set-Cookie")
			return nil
		},
	}
	return func(c *gin.Context) {
		proxy.ServeHTTP(c.Writer, c.Request)
	}
}
