package mcp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/apikey"
	"github.com/yldm-tech/forma/apps/server/internal/auth"
	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/oauthserver"
	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpserver"
	"github.com/yldm-tech/forma/apps/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/v3api"
)

// queryCredentialParams are query parameters that would carry a credential in the URL, where proxies and logs keep it; forma2 refused them case-insensitively.
var queryCredentialParams = []string{"api_key", "apikey", "x-api-key", "access_token", "token", "authorization"}

const (
	methodAPIKey = "apiKey"
	methodOAuth  = "oauth"
)

// principal is an authenticated MCP caller, forma2's TMcpAuthInfo: an API key, or a user through an OAuth access token issued to a client.
type principal struct {
	actor authz.Actor
	// apiKey is set for an API key; userID for an OAuth token.
	apiKey   *apikey.Principal
	userID   uuid.UUID
	clientID string
	scopes   []string
	method   string
	// requestID is the id of the HTTP request; tool results and the operations they run carry it.
	requestID string
}

type principalKey struct{}

func withPrincipal(ctx context.Context, p *principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

func principalFrom(ctx context.Context) (*principal, bool) {
	p, ok := ctx.Value(principalKey{}).(*principal)
	return p, ok
}

// hasAll reports whether the caller holds every scope.
func (p *principal) hasAll(scopes []string) bool {
	for _, s := range scopes {
		if !slices.Contains(p.scopes, s) {
			return false
		}
	}
	return true
}

// hasAny reports whether the caller holds at least one of the scopes.
func (p *principal) hasAny(scopes []string) bool {
	return slices.ContainsFunc(scopes, func(s string) bool { return slices.Contains(p.scopes, s) })
}

// apiKeyScopes is forma2's getMcpScopes: every key may read; a key with write or manage on any workspace may also write. The workspace checks of every operation still apply.
func apiKeyScopes(p *apikey.Principal) []string {
	scopes := []string{"surveys:read", "workflows:read"}
	for _, g := range p.WorkspacePermissions {
		if g.Permission == "write" || g.Permission == "manage" {
			return append(scopes, "surveys:write", "workflows:write")
		}
	}
	return scopes
}

// authenticate is forma2's authenticateMcpRequest. It answers the request itself and returns false when the caller may not go on.
func (h *Handler) authenticate(c *gin.Context) (*principal, bool) {
	for key := range c.Request.URL.Query() {
		if slices.Contains(queryCredentialParams, strings.ToLower(key)) {
			h.log.WarnContext(c.Request.Context(), "MCP API key supplied in query parameters", "request_id", httpserver.RequestID(c), "status_code", http.StatusBadRequest)
			v3api.WriteProblem(c, v3api.BadRequest("API keys must be sent in headers, not query parameters", v3api.InvalidParam{Name: "query", Reason: "Send the API key with x-api-key or Authorization: Bearer."}))
			return nil, false
		}
	}
	if !h.originAllowed(c.GetHeader("Origin")) {
		h.log.WarnContext(c.Request.Context(), "MCP origin validation failed", "request_id", httpserver.RequestID(c), "status_code", http.StatusForbidden)
		v3api.WriteProblem(c, v3api.Forbidden("Cross-origin MCP requests are not allowed"))
		return nil, false
	}
	header := c.Request.Header
	if strings.TrimSpace(header.Get(apikey.HeaderAPIKey)) != "" {
		return h.authenticateAPIKey(c)
	}
	bearer := apikey.BearerToken(header)
	if bearer == "" {
		h.reject(c, "API key or OAuth access token required", "MCP authentication credentials missing")
		return nil, false
	}
	if apikey.KeyFromHeaders(header) != "" {
		return h.authenticateAPIKey(c)
	}
	return h.authenticateOAuth(c, bearer)
}

// originAllowed admits a request without an Origin, or from the MCP resource's own origin, which comes from WEBAPP_URL and never from forwarded headers.
func (h *Handler) originAllowed(origin string) bool {
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return false
	}
	return auth.URLOrigin(u) == h.urls.ResourceOrigin()
}

func (h *Handler) authenticateAPIKey(c *gin.Context) (*principal, bool) {
	ctx := c.Request.Context()
	key, err := h.keys.Authenticate(ctx, c.Request.Header, apikey.Options{})
	if err != nil {
		h.internalError(c, "MCP API authentication unexpected error", err)
		return nil, false
	}
	if key == nil {
		h.reject(c, "API key or OAuth access token required", "MCP API key authentication failed")
		return nil, false
	}
	if !h.charge(c, ratelimit.APIV3, key.APIKeyID.String(), "MCP API rate limit exceeded") {
		return nil, false
	}
	return &principal{actor: key.Actor(), apiKey: key, clientID: key.APIKeyID.String(), scopes: apiKeyScopes(key), method: methodAPIKey}, true
}

// acceptedAudience is the GHSA-p2fr-6hmx-4528 check: the token must name the MCP resource, and nothing but it and the authorization server's userinfo endpoint, which the server adds when openid is granted. A token that also names another resource server was minted for somebody else.
func (h *Handler) acceptedAudience(aud []string) bool {
	if !slices.Contains(aud, h.urls.Resource) {
		return false
	}
	for _, a := range aud {
		if a != h.urls.Resource && a != h.urls.UserInfo {
			return false
		}
	}
	return true
}

func (h *Handler) authenticateOAuth(c *gin.Context, token string) (*principal, bool) {
	ctx := c.Request.Context()
	claims, err := h.verifier.Verify(ctx, token)
	if err != nil {
		// The reason stays in the log, by its source only: expired, forged, foreign and malformed tokens get the same answer.
		source := "token_verification"
		if errors.Is(err, oauthserver.ErrKeysUnavailable) {
			source = "jwks_fetch"
		}
		h.reject(c, "", "MCP OAuth authentication failed", "failure_source", source)
		return nil, false
	}
	clientID := claims.AuthorizedParty
	if clientID == "" {
		clientID = claims.ClientID
	}
	if !h.acceptedAudience(claims.Audience) {
		h.reject(c, "", "MCP OAuth token audience is not bound to this resource server", "client_id", clientID)
		return nil, false
	}
	if claims.Subject == "" {
		h.reject(c, "User OAuth access token required", "MCP OAuth token has no user subject")
		return nil, false
	}
	if clientID == "" {
		clientID = "unknown"
	}
	userID, err := uuid.Parse(claims.Subject)
	active := false
	if err == nil {
		if active, err = h.userActive(ctx, userID); err != nil {
			h.internalError(c, "MCP API authentication unexpected error", err)
			return nil, false
		}
	}
	if !active {
		h.reject(c, "", "MCP OAuth token user is inactive", "client_id", clientID)
		return nil, false
	}
	scopes := strings.Fields(claims.Scope)
	p := &principal{actor: authz.User(userID), userID: userID, clientID: clientID, scopes: scopes, method: methodOAuth}
	// Authenticating needs one resource scope; which tools the token reaches is decided per tool. offline_access alone grants nothing.
	if !p.hasAny(oauthserver.ResourceScopes) {
		h.log.WarnContext(ctx, "MCP OAuth token missing every MCP scope", "request_id", httpserver.RequestID(c), "status_code", http.StatusForbidden, "client_id", clientID)
		c.Header("WWW-Authenticate", h.insufficientScopeChallenge(oauthserver.ResourceScopes))
		v3api.WriteProblem(c, v3api.Forbidden("OAuth token does not include the required MCP scope"))
		return nil, false
	}
	if !h.charge(c, ratelimit.APIV3, "oauth:"+userID.String()+":"+clientID, "MCP OAuth rate limit exceeded") {
		return nil, false
	}
	return p, true
}

func (h *Handler) userActive(ctx context.Context, id uuid.UUID) (bool, error) {
	var active []bool
	if err := h.db.WithContext(ctx).Raw(`SELECT is_active FROM users WHERE id = ?`, id).Scan(&active).Error; err != nil {
		return false, fmt.Errorf("load user: %w", err)
	}
	return len(active) == 1 && active[0], nil
}

// charge applies a limit to the caller and answers 429 when it is spent.
func (h *Handler) charge(c *gin.Context, rule ratelimit.Rule, key, logMessage string) bool {
	err := h.limiter.Apply(c.Request.Context(), rule, key)
	if err == nil {
		return true
	}
	if e, ok := apperr.As(err); ok && e.Status == http.StatusTooManyRequests {
		h.log.WarnContext(c.Request.Context(), logMessage, "request_id", httpserver.RequestID(c), "status_code", http.StatusTooManyRequests)
		v3api.WriteProblem(c, v3api.TooManyRequests(e.Message, e.RetryAfter))
		return false
	}
	h.internalError(c, "MCP API authentication unexpected error", err)
	return false
}

// reject is forma2's rejectUnauthenticatedMcpRequest: it charges the per-address limit first, so a bad credential is not free to retry, then answers 401 with the challenge that points clients at the protected resource metadata. detail defaults to the one answer every failed OAuth token gets.
func (h *Handler) reject(c *gin.Context, detail, logMessage string, attrs ...any) {
	if detail == "" {
		detail = "Invalid OAuth access token"
	}
	if !h.charge(c, ratelimit.APIMCPAuth, ratelimit.IPIdentifier(c.Request.Context()), "MCP unauthenticated rate limit exceeded") {
		return
	}
	h.log.WarnContext(c.Request.Context(), logMessage, append(attrs, "request_id", httpserver.RequestID(c), "status_code", http.StatusUnauthorized)...)
	c.Header("WWW-Authenticate", fmt.Sprintf(`Bearer resource_metadata="%s", scope="%s"`, h.urls.ProtectedResourceMetadata, strings.Join(oauthserver.ProtectedResourceScopes, " ")))
	v3api.WriteProblem(c, v3api.Unauthorized(detail))
}

// insufficientScopeChallenge is the RFC 6750 challenge naming the scopes a token lacks.
func (h *Handler) insufficientScopeChallenge(scopes []string) string {
	return strings.Join([]string{
		`Bearer error="insufficient_scope"`,
		fmt.Sprintf(`scope="%s"`, strings.Join(scopes, " ")),
		fmt.Sprintf(`resource_metadata="%s"`, h.urls.ProtectedResourceMetadata),
		`error_description="The OAuth access token does not include the required MCP scope."`,
	}, ", ")
}

func (h *Handler) internalError(c *gin.Context, message string, err error) {
	h.log.ErrorContext(c.Request.Context(), message, "request_id", httpserver.RequestID(c), "status_code", http.StatusInternalServerError, "err", err)
	v3api.WriteProblem(c, v3api.Internal())
}
