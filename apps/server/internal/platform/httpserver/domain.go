package httpserver

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
)

// The routes of forma2's public domain (apps/web/lib/middleware/route-config.ts): the survey pages and the client API live only there, health and public storage on both domains.
var (
	publicOnlyRoutes = []*regexp.Regexp{
		regexp.MustCompile(`^/s/[^/]+`),
		regexp.MustCompile(`^/c/[^/]+`),
		regexp.MustCompile(`^/p/[^/]+`),
		regexp.MustCompile(`^/api/v[12]/client/`),
	}
	commonRoutes = []*regexp.Regexp{
		regexp.MustCompile(`^/health$`),
		regexp.MustCompile(`^/storage/[^/]+/public/`),
	}
	// unroutedPaths are the static paths both domains serve: those forma2's proxy matcher never saw (Vite's fingerprinted assets under the app's base path, /app/assets, in place of forma2's /_next/static, the SDK bundles, the other public folders and the root files), and the rest of forma2's apps/web/public, which survey pages on the public domain (image backgrounds, favicons) and emails (the logo and rating icons) load.
	unroutedPaths = regexp.MustCompile(`^/(?:(?:app/assets|_next/static|_next/image|js|css|images|fonts|icons|public|animated-bgs|image-backgrounds|favicon|star-icons|smiley-icons|sample-csv)(?:/|$)|(?:favicon\.ico|sitemap\.xml|robots\.txt|logo-transparent\.png)$)`)
)

func matchesAny(path string, patterns []*regexp.Regexp) bool {
	for _, p := range patterns {
		if p.MatchString(path) {
			return true
		}
	}
	return false
}

// PublicDomainGate is forma2's domain-aware routing. With PUBLIC_URL set, a request to its host may reach only the survey pages, the client API, health and public storage, and the admin domain may reach everything but the survey pages and the client API; anything else is an empty 404. Without PUBLIC_URL every route is served on every host.
func PublicDomainGate(cfg config.Config) gin.HandlerFunc {
	if cfg.PublicURL == nil {
		return func(c *gin.Context) { c.Next() }
	}
	publicHost := cfg.PublicURL.Host
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		if unroutedPaths.MatchString(path) {
			c.Next()
			return
		}
		allowed := !matchesAny(path, publicOnlyRoutes)
		if strings.EqualFold(c.Request.Host, publicHost) {
			allowed = matchesAny(path, publicOnlyRoutes) || matchesAny(path, commonRoutes)
		}
		if !allowed {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		c.Next()
	}
}
