package httpapi

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/platform/config"
)

// CSRFHeader must be present, with any value, on every state-changing /api/app request. A cross-site form cannot set a custom header, and a cross-site fetch that sets one needs a CORS preflight the server never grants, so the header alone proves the request came from script on an allowed origin. The SPA's API client always sends it.
const CSRFHeader = "X-Requested-With"

// CodeCSRF is the error code of a rejected cross-site request.
const CodeCSRF = "csrf_rejected"

// CSRF guards cookie-authenticated mutations. Safe methods pass; every other method needs an Origin equal to WEBAPP_URL's or PUBLIC_URL's origin and the CSRFHeader. forma2 relied on the Origin check alone (Next server actions, Better Auth trustedOrigins); the header requirement is added defense in depth.
func CSRF(cfg config.Config) gin.HandlerFunc {
	allowed := map[string]bool{origin(cfg.WebAppURL): true}
	if cfg.PublicURL != nil {
		allowed[origin(cfg.PublicURL)] = true
	}
	return func(c *gin.Context) {
		switch c.Request.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			c.Next()
			return
		}
		if o := c.GetHeader("Origin"); o == "" || !allowed[strings.ToLower(o)] {
			Abort(c, &apperr.Error{Status: http.StatusForbidden, Code: CodeCSRF, Message: "Cross-origin request rejected"})
			return
		}
		if c.GetHeader(CSRFHeader) == "" {
			Abort(c, &apperr.Error{Status: http.StatusForbidden, Code: CodeCSRF, Message: "Missing " + CSRFHeader + " header"})
			return
		}
		c.Next()
	}
}

// origin is the serialized origin a browser sends in the Origin header: scheme://host[:port], lowercase, default ports omitted.
func origin(u *url.URL) string {
	host := strings.ToLower(u.Hostname())
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	port := u.Port()
	scheme := strings.ToLower(u.Scheme)
	if port == "" || (scheme == "http" && port == "80") || (scheme == "https" && port == "443") {
		return scheme + "://" + host
	}
	return scheme + "://" + host + ":" + port
}
