// Package oauthserver is the OAuth 2.1 authorization server MCP clients sign in through, forma2's Better Auth oauth-provider plugin (apps/web/modules/auth/lib/mcp-oauth-provider-options.ts): dynamic client registration, the authorization code flow with PKCE, the user's consent, refresh tokens bound to the approved resource and scopes, revocation, introspection, userinfo, and the discovery documents. The protocol endpoints are zitadel/oidc's op; registration, consent and the token format are here.
package oauthserver

import (
	"net/url"
	"strings"

	"github.com/yldm-tech/forma/server/internal/auth"
	"github.com/yldm-tech/forma/server/internal/platform/config"
)

// Scopes is everything the authorization server grants, forma2's MCP_OAUTH_SCOPES. Every registered client may request all of them.
var Scopes = []string{"openid", "profile", "email", "offline_access", "surveys:read", "surveys:write", "workflows:read", "workflows:write"}

// ResourceScopes are the scopes that reach MCP tools, forma2's MCP_RESOURCE_SCOPES. A token must hold at least one to use /api/mcp at all.
var ResourceScopes = []string{"surveys:read", "surveys:write", "workflows:read", "workflows:write"}

// ProtectedResourceScopes are the scopes the protected resource metadata advertises and the 401 challenge names, forma2's MCP_PROTECTED_RESOURCE_SCOPES: the resource scopes and offline_access, which MCP clients must register to be issued refresh tokens.
var ProtectedResourceScopes = []string{"surveys:read", "surveys:write", "workflows:read", "workflows:write", "offline_access"}

// URLs are the public URLs of the authorization server and the MCP resource, forma2's oauth-urls.ts. They derive from WEBAPP_URL and BETTER_AUTH_URL (else NEXTAUTH_URL), never from PUBLIC_URL or the request's headers.
type URLs struct {
	// Issuer is <BETTER_AUTH_URL, else NEXTAUTH_URL, else WEBAPP_URL>/api/auth; the authorization server's endpoints live under it.
	Issuer string
	// Resource is <WEBAPP_URL>/api/mcp, the only resource tokens are issued for and the audience /api/mcp requires.
	Resource string
	// ProtectedResourceMetadata is where MCP clients discover the authorization server (RFC 9728).
	ProtectedResourceMetadata string
	// UserInfo is the authorization server's userinfo endpoint, the one audience besides Resource an access token carries (when openid is granted).
	UserInfo string
	// JWKS is where the signing keys are published.
	JWKS string
	// WebApp is WEBAPP_URL without query, fragment or trailing slashes; the consent page lives under it.
	WebApp string
}

func NewURLs(cfg config.Config) URLs {
	issuer := auth.IssuerURL(cfg)
	return URLs{
		Issuer:                    issuer,
		Resource:                  appendPath(cfg.WebAppURL, "/api/mcp"),
		ProtectedResourceMetadata: appendPath(cfg.WebAppURL, "/.well-known/oauth-protected-resource/api/mcp"),
		UserInfo:                  issuer + "/oauth2/userinfo",
		JWKS:                      issuer + "/jwks",
		WebApp:                    auth.URLOrigin(cfg.WebAppURL) + strings.TrimRight(cfg.WebAppURL.Path, "/"),
	}
}

// ResourceOrigin is the origin of the MCP resource; a browser may call /api/mcp only from it.
func (u URLs) ResourceOrigin() string {
	parsed, _ := url.Parse(u.Resource)
	return auth.URLOrigin(parsed)
}

// appendPath is forma2's appendPath: the base's origin and path, without trailing slashes, with path appended unless the base already ends with it.
func appendPath(base *url.URL, path string) string {
	p := strings.TrimRight(base.Path, "/")
	if !strings.HasSuffix(p, path) {
		p += path
	}
	return auth.URLOrigin(base) + p
}
