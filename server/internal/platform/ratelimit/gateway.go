package ratelimit

import (
	"net/http"
	"regexp"
	"strings"
)

// GatewayAuth is how a request authenticated, which decides whether forma2's gateway policy set covers it.
type GatewayAuth int

const (
	GatewayAuthNone GatewayAuth = iota
	GatewayAuthAPIKey
	GatewayAuthSession
)

// The client API paths forma2's Envoy policy set limits per method (apps/web/modules/core/rate-limit/envoy-rate-limit-coverage.ts). The v2 environment and user routes are not among them: forma2 served them with the v1 handlers, which matched the v1 patterns only.
var gatewayClientRoutes = []struct {
	method  string
	pattern *regexp.Regexp
}{
	{http.MethodPost, regexp.MustCompile(`^/api/v1/client/[^/]+/storage$`)},
	{http.MethodGet, regexp.MustCompile(`^/api/v1/client/[^/]+/environment$`)},
	{http.MethodPost, regexp.MustCompile(`^/api/v1/client/[^/]+/responses$`)},
	{http.MethodPut, regexp.MustCompile(`^/api/v1/client/[^/]+/responses/[^/]+$`)},
	{http.MethodPost, regexp.MustCompile(`^/api/v1/client/[^/]+/displays$`)},
	{http.MethodPost, regexp.MustCompile(`^/api/v1/client/[^/]+/user$`)},
	{http.MethodPost, regexp.MustCompile(`^/api/v2/client/[^/]+/responses$`)},
	{http.MethodPut, regexp.MustCompile(`^/api/v2/client/[^/]+/responses/[^/]+$`)},
	{http.MethodPost, regexp.MustCompile(`^/api/v2/client/[^/]+/displays$`)},
	{http.MethodPost, regexp.MustCompile(`^/api/v2/client/[^/]+/storage$`)},
}

var gatewayStorageDelete = regexp.MustCompile(`^/storage/[^/]+/(public|private)/.+$`)

// CoveredByGateway is forma2's isRouteRateLimitedByEnvoy: whether the gateway policy set limits this request, so that with GATEWAY_RATE_LIMITING=1 the server skips its own limit for it. Unauthenticated: the sign-in callbacks and the client API routes above. With an API key: every v1 management route and the v1 webhook routes under /api/v1/webhooks/ (not the collection itself, which the policy's prefix does not match), and file deletion. A session is never covered.
func CoveredByGateway(method, path string, auth GatewayAuth) bool {
	method = strings.ToUpper(method)
	if method == http.MethodOptions {
		return false
	}
	switch auth {
	case GatewayAuthNone:
		if method == http.MethodPost && (path == "/api/auth/callback/credentials" || path == "/api/auth/callback/token") {
			return true
		}
		for _, r := range gatewayClientRoutes {
			if method == r.method && r.pattern.MatchString(path) {
				return true
			}
		}
	case GatewayAuthAPIKey:
		switch method {
		case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
			if strings.HasPrefix(path, "/api/v1/management/") || strings.HasPrefix(path, "/api/v1/webhooks/") {
				return true
			}
		}
		return method == http.MethodDelete && gatewayStorageDelete.MatchString(path)
	}
	return false
}
