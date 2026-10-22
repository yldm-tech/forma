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
	check := CSRFCheck(cfg)
	return func(c *gin.Context) {
		if e := check(c.Request); e != nil {
			Abort(c, e)
			return
		}
		c.Next()
	}
}

// CSRFCheck is the rule CSRF applies, for a surface that renders its own error format (the v3 API checks it on session-authenticated mutations only): nil when the request may proceed.
func CSRFCheck(cfg config.Config) func(r *http.Request) *apperr.Error {
	allowed := map[string]bool{origin(cfg.WebAppURL): true}
	if cfg.PublicURL != nil {
		allowed[origin(cfg.PublicURL)] = true
	}
	return func(r *http.Request) *apperr.Error {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			return nil
		}
		if o := r.Header.Get("Origin"); o == "" || !allowed[strings.ToLower(o)] {
			return &apperr.Error{Status: http.StatusForbidden, Code: CodeCSRF, Message: "Cross-origin request rejected"}
		}
		if r.Header.Get(CSRFHeader) == "" {
			return &apperr.Error{Status: http.StatusForbidden, Code: CodeCSRF, Message: "Missing " + CSRFHeader + " header"}
		}
		return nil
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
