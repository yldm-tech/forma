package oauthserver

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	jose "github.com/go-jose/go-jose/v4"
	"github.com/google/uuid"
	"github.com/zitadel/oidc/v3/pkg/oidc"
	"github.com/zitadel/oidc/v3/pkg/op"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/server/internal/platform/ratelimit"
)

// basePath is where the authorization server is mounted; its issuer is the configured auth base URL with the same path.
const basePath = "/api/auth"

// metadataCacheControl is how long discovery documents may be cached, forma2's.
const metadataCacheControl = "public, max-age=15, stale-while-revalidate=15, stale-if-error=86400"

// consentPath is the SPA page that asks the user to approve an authorization request, forma2's consentPage.
const consentPath = "/account/authorize"

// Handler serves the authorization server under /api/auth: op's protocol endpoints, dynamic client registration, the step between authorization and consent, and the discovery documents.
type Handler struct {
	urls     URLs
	repo     *repository
	storage  *storage
	provider *op.Provider
	sessions Sessions
	// confirmation signs the state of RP-initiated logout's confirmation page.
	confirmation logoutConfirmation
	limiter      *ratelimit.Limiter
	log          *slog.Logger
	now          func() time.Time
}

func NewHandler(cfg config.Config, db *gorm.DB, keys *Keys, sessions Sessions, limiter *ratelimit.Limiter, log *slog.Logger) (*Handler, error) {
	urls := NewURLs(cfg)
	repo := &repository{db: db}
	format := newTokenFormat()
	s := &storage{repo: repo, keys: keys, format: format, urls: urls, log: log, now: time.Now}
	opts := []op.Option{
		op.WithCrypto(format),
		op.WithCustomEndpoints(op.NewEndpoint("oauth2/authorize"), op.NewEndpoint("oauth2/token"), op.NewEndpoint("oauth2/userinfo"), op.NewEndpoint("oauth2/revoke"), op.NewEndpoint("oauth2/end-session"), op.NewEndpoint("jwks")),
		op.WithCustomIntrospectionEndpoint(op.NewEndpoint("oauth2/introspect")),
		op.WithAccessTokenVerifierOpts(op.WithSupportedAccessTokenSigningAlgorithms(string(jose.EdDSA))),
	}
	if strings.HasPrefix(urls.Issuer, "http://") {
		opts = append(opts, op.WithAllowInsecure())
	}
	provider, err := op.NewProvider(&op.Config{
		CodeMethodS256:        true,
		AuthMethodPost:        true,
		GrantTypeRefreshToken: true,
		SupportedScopes:       Scopes,
	}, s, op.StaticIssuer(urls.Issuer), opts...)
	if err != nil {
		return nil, fmt.Errorf("create oauth provider: %w", err)
	}
	return &Handler{urls: urls, repo: repo, storage: s, provider: provider, sessions: sessions, confirmation: newLogoutConfirmation(cfg, urls), limiter: limiter, log: log, now: time.Now}, nil
}

// Register mounts the authorization server. The SSO callbacks under /api/auth/callback and /api/auth/oauth2/callback are the auth module's and stay separate.
func (h *Handler) Register(r gin.IRouter) {
	provider := gin.WrapH(http.StripPrefix(basePath, h.provider))
	g := r.Group(basePath)
	g.GET("/oauth2/authorize", h.captureForm, provider)
	g.POST("/oauth2/authorize", h.captureForm, provider)
	g.GET("/oauth2/authorize/callback", h.sessions.Optional(), h.requireApprover, provider)
	g.POST("/oauth2/token", h.limit(ratelimit.BAOAuthToken), h.captureForm, h.checkTokenResource, provider)
	g.POST("/oauth2/introspect", h.limit(ratelimit.BAOAuthIntrospect), clientSecretPostAsBasic, provider)
	g.POST("/oauth2/revoke", h.limit(ratelimit.BAOAuthRevoke), provider)
	g.GET("/oauth2/userinfo", provider)
	g.POST("/oauth2/userinfo", provider)
	g.GET("/jwks", provider)
	// op answers the CORS preflights of the endpoints a browser-based client calls directly.
	for _, path := range []string{"/oauth2/token", "/oauth2/introspect", "/oauth2/revoke", "/oauth2/userinfo", "/jwks"} {
		g.OPTIONS(path, provider)
	}
	g.POST("/oauth2/register", h.limit(ratelimit.BAOAuthRegister), h.register)
	g.GET(endSessionPath, httpapi.ErrorHandler(h.log), h.sessions.Optional(), h.endSession)
	g.POST(endSessionPath, httpapi.ErrorHandler(h.log), h.sessions.Optional(), h.endSession)
	g.POST(endSessionConfirmPath, httpapi.ErrorHandler(h.log), h.sessions.Optional(), h.confirmEndSession)
	g.GET(loginPath, httpapi.ErrorHandler(h.log), h.sessions.Optional(), h.continueAuthorization)
	g.GET("/.well-known/oauth-authorization-server", h.metadata)
	g.GET("/.well-known/openid-configuration", h.metadata)
	// RFC 8414's path-inserted locations, for any issuer path.
	r.GET("/.well-known/oauth-authorization-server", h.metadata)
	r.GET("/.well-known/oauth-authorization-server/*issuer", h.metadata)
	r.GET("/.well-known/openid-configuration", h.metadata)
	r.GET("/.well-known/openid-configuration/*issuer", h.metadata)
}

// limit charges rule to the client address and answers 429 the way Better Auth's limiter did.
func (h *Handler) limit(rule ratelimit.Rule) gin.HandlerFunc {
	return func(c *gin.Context) {
		err := h.limiter.ApplyIP(c.Request.Context(), rule)
		if err == nil {
			c.Next()
			return
		}
		e, ok := apperr.As(err)
		if !ok || e.Status != http.StatusTooManyRequests {
			h.log.ErrorContext(c.Request.Context(), "OAuth rate limit check failed", "err", err)
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
			return
		}
		if e.RetryAfter > 0 {
			c.Header("X-Retry-After", strconv.Itoa(e.RetryAfter))
			c.Header("Retry-After", strconv.Itoa(e.RetryAfter))
		}
		c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"message": "Too many requests. Please try again later."})
	}
}

// captureForm hands the request's parameters to the storage, which reads the resource indicator op does not parse.
func (h *Handler) captureForm(c *gin.Context) {
	if err := c.Request.ParseForm(); err != nil {
		oauthError(c, http.StatusBadRequest, "invalid_request", "cannot parse form")
		return
	}
	c.Request = c.Request.WithContext(withForm(c.Request.Context(), c.Request.Form))
	c.Next()
}

// clientSecretPostAsBasic lets a confidential client authenticate to introspection with client_secret_post, as forma2 advertised and accepted: op reads only HTTP Basic there, so form credentials are moved into the Authorization header.
func clientSecretPostAsBasic(c *gin.Context) {
	if err := c.Request.ParseForm(); err != nil {
		oauthError(c, http.StatusBadRequest, "invalid_request", "cannot parse form")
		return
	}
	id, secret := c.Request.PostForm.Get("client_id"), c.Request.PostForm.Get("client_secret")
	if c.GetHeader("Authorization") == "" && id != "" && secret != "" {
		c.Request.SetBasicAuth(url.QueryEscape(id), url.QueryEscape(secret))
	}
	c.Next()
}

// checkTokenResource refuses a token request whose resource indicator names a resource the client may not be issued tokens for (invalid_target, RFC 8707). Tokens are only ever issued for the resource approved at authorization, so a matching indicator changes nothing.
func (h *Handler) checkTokenResource(c *gin.Context) {
	resources := c.Request.PostForm["resource"]
	if len(resources) == 0 {
		c.Next()
		return
	}
	clientID, _, ok := c.Request.BasicAuth()
	if ok {
		clientID, _ = url.QueryUnescape(clientID)
	} else {
		clientID = c.Request.PostForm.Get("client_id")
	}
	client, err := h.repo.client(c.Request.Context(), clientID)
	if err != nil {
		h.log.ErrorContext(c.Request.Context(), "Failed to load OAuth client", "err", err)
		oauthError(c, http.StatusInternalServerError, "server_error", "")
		return
	}
	for _, resource := range resources {
		if client == nil || !client.allowsResource(resource) {
			oauthError(c, http.StatusBadRequest, "invalid_target", "The requested resource is invalid, unknown, or not allowed for this client.")
			return
		}
	}
	c.Next()
}

func oauthError(c *gin.Context, status int, errorType, description string) {
	body := gin.H{"error": errorType}
	if description != "" {
		body["error_description"] = description
	}
	c.Header("Cache-Control", "no-store")
	c.AbortWithStatusJSON(status, body)
}

// continueAuthorization is the step op sends the browser to once it accepted an authorization request. A signed-in user who already approved the client for these scopes and this resource goes straight back to the client; anyone else goes to the consent page, which signs them in first. prompt=none fails instead of showing anything.
func (h *Handler) continueAuthorization(c *gin.Context) {
	ctx := c.Request.Context()
	id, err := uuid.Parse(c.Query("id"))
	if err != nil {
		c.Redirect(http.StatusFound, h.urls.WebApp+consentPath)
		return
	}
	req, err := h.repo.authRequest(ctx, id)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	if req == nil || req.Done() {
		c.Redirect(http.StatusFound, h.consentURL(id, nil))
		return
	}
	actor, signedIn := httpapi.ActorFrom(ctx)
	approved := false
	if signedIn && !slices.Contains(req.prompt, oidc.PromptConsent) && !slices.Contains(req.prompt, oidc.PromptLogin) {
		if approved, err = h.covered(c, actor.UserID, req); err != nil {
			httpapi.Fail(c, err)
			return
		}
	}
	switch {
	case approved:
		if _, err := h.repo.bindAuthRequest(ctx, req.id, actor.UserID, h.now()); err != nil {
			httpapi.Fail(c, err)
			return
		}
		c.Redirect(http.StatusFound, h.callbackURL(req.id))
	case slices.Contains(req.prompt, oidc.PromptNone):
		errorType := "login_required"
		if signedIn {
			errorType = "consent_required"
		}
		if err := h.repo.deleteAuthRequest(ctx, req.id); err != nil {
			httpapi.Fail(c, err)
			return
		}
		c.Redirect(http.StatusFound, errorRedirect(req, errorType))
	default:
		c.Redirect(http.StatusFound, h.consentURL(id, req))
	}
}

// requireApprover lets only the session that approved a request collect its code: op's callback issues the code to whoever presents the request id, which the client that started the flow also knows, so without this check it could take the code of a request someone else approved for it. A request not yet approved goes on to op, which refuses it.
func (h *Handler) requireApprover(c *gin.Context) {
	id, err := uuid.Parse(c.Query("id"))
	if err != nil {
		c.Next()
		return
	}
	req, err := h.repo.authRequest(c.Request.Context(), id)
	if err != nil {
		h.log.ErrorContext(c.Request.Context(), "Failed to load OAuth authorization request", "err", err)
		oauthError(c, http.StatusInternalServerError, "server_error", "")
		return
	}
	if req == nil || !req.Done() {
		c.Next()
		return
	}
	actor, signedIn := httpapi.ActorFrom(c.Request.Context())
	if !signedIn || actor.UserID != *req.userID {
		oauthError(c, http.StatusForbidden, "access_denied", "The authorization request was approved in another session.")
		return
	}
	c.Next()
}

// covered reports whether the user's consent for the client already includes every scope and the resource of the request.
func (h *Handler) covered(c *gin.Context, userID uuid.UUID, req *AuthRequest) (bool, error) {
	consent, err := h.repo.consent(c.Request.Context(), userID, req.clientID)
	if err != nil || consent == nil {
		return false, err
	}
	for _, scope := range req.scopes {
		if !slices.Contains(consent.Scopes, scope) {
			return false, nil
		}
	}
	return slices.Contains(consent.Resources, req.resource), nil
}

// consentURL is the consent page for a request, with the client, scopes and redirect URI it shows (forma2's page read client_id, scope and redirect_uri) and the request it answers.
func (h *Handler) consentURL(id uuid.UUID, req *AuthRequest) string {
	q := url.Values{"request_id": {id.String()}}
	if req != nil {
		q.Set("client_id", req.clientID)
		q.Set("scope", strings.Join(req.scopes, " "))
		q.Set("redirect_uri", req.redirectURI)
	}
	return h.urls.WebApp + consentPath + "?" + q.Encode()
}

// callbackURL is op's step that issues the code of an approved request and redirects to the client.
func (h *Handler) callbackURL(id uuid.UUID) string {
	return h.urls.Issuer + "/oauth2/authorize/callback?id=" + url.QueryEscape(id.String())
}

// errorRedirect is the client's redirect URI carrying an authorization error (RFC 6749 §4.1.2.1).
func errorRedirect(req *AuthRequest, errorType string) string {
	u, err := url.Parse(req.redirectURI)
	if err != nil {
		return req.redirectURI
	}
	q := u.Query()
	q.Set("error", errorType)
	if req.state != "" {
		q.Set("state", req.state)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// discovery is the authorization server metadata (RFC 8414), also served as the OpenID configuration. It is the document forma2's Better Auth served, minus what this server does not implement: the client_credentials grant, private_key_jwt client authentication (and the signing algorithms only it uses), DPoP, back-channel logout, the claims request parameter, the iss authorization response parameter, and the claims it never issues (sid, picture, given_name, family_name).
type discovery struct {
	ScopesSupported                           []string `json:"scopes_supported"`
	Issuer                                    string   `json:"issuer"`
	AuthorizationEndpoint                     string   `json:"authorization_endpoint"`
	TokenEndpoint                             string   `json:"token_endpoint"`
	JWKSURI                                   string   `json:"jwks_uri"`
	RegistrationEndpoint                      string   `json:"registration_endpoint"`
	IntrospectionEndpoint                     string   `json:"introspection_endpoint"`
	RevocationEndpoint                        string   `json:"revocation_endpoint"`
	ResponseTypesSupported                    []string `json:"response_types_supported"`
	ResponseModesSupported                    []string `json:"response_modes_supported"`
	GrantTypesSupported                       []string `json:"grant_types_supported"`
	TokenEndpointAuthMethodsSupported         []string `json:"token_endpoint_auth_methods_supported"`
	IntrospectionEndpointAuthMethodsSupported []string `json:"introspection_endpoint_auth_methods_supported"`
	RevocationEndpointAuthMethodsSupported    []string `json:"revocation_endpoint_auth_methods_supported"`
	CodeChallengeMethodsSupported             []string `json:"code_challenge_methods_supported"`
	ClaimsSupported                           []string `json:"claims_supported"`
	UserinfoEndpoint                          string   `json:"userinfo_endpoint"`
	SubjectTypesSupported                     []string `json:"subject_types_supported"`
	ACRValuesSupported                        []string `json:"acr_values_supported"`
	IDTokenSigningAlgValuesSupported          []string `json:"id_token_signing_alg_values_supported"`
	EndSessionEndpoint                        string   `json:"end_session_endpoint"`
	RequestParameterSupported                 bool     `json:"request_parameter_supported"`
	RequestURIParameterSupported              bool     `json:"request_uri_parameter_supported"`
	PromptValuesSupported                     []string `json:"prompt_values_supported"`
}

// clientSecretMethods are the client authentication methods of the introspection and revocation endpoints; a public client may revoke its own tokens too, which forma2 did not advertise.
var clientSecretMethods = []string{authMethodBasic, authMethodPost}

// metadata serves the discovery document at every location MCP clients look for it.
func (h *Handler) metadata(c *gin.Context) {
	issuer := h.urls.Issuer
	c.Header("Cache-Control", metadataCacheControl)
	c.JSON(http.StatusOK, &discovery{
		ScopesSupported:                   Scopes,
		Issuer:                            issuer,
		AuthorizationEndpoint:             issuer + "/oauth2/authorize",
		TokenEndpoint:                     issuer + "/oauth2/token",
		JWKSURI:                           h.urls.JWKS,
		RegistrationEndpoint:              issuer + "/oauth2/register",
		IntrospectionEndpoint:             issuer + "/oauth2/introspect",
		RevocationEndpoint:                issuer + "/oauth2/revoke",
		ResponseTypesSupported:            []string{string(oidc.ResponseTypeCode)},
		ResponseModesSupported:            []string{string(oidc.ResponseModeQuery)},
		GrantTypesSupported:               []string{grantTypeCode, grantTypeRefresh},
		TokenEndpointAuthMethodsSupported: []string{authMethodNone, authMethodBasic, authMethodPost},

		IntrospectionEndpointAuthMethodsSupported: clientSecretMethods,
		RevocationEndpointAuthMethodsSupported:    clientSecretMethods,
		CodeChallengeMethodsSupported:             []string{string(oidc.CodeChallengeMethodS256)},
		ClaimsSupported:                           []string{"sub", "iss", "aud", "exp", "iat", "scope", "azp", "name", "email", "email_verified"},
		UserinfoEndpoint:                          h.urls.UserInfo,
		SubjectTypesSupported:                     []string{"public"},
		ACRValuesSupported:                        []string{"0"},
		IDTokenSigningAlgValuesSupported:          []string{string(jose.EdDSA)},
		EndSessionEndpoint:                        issuer + endSessionPath,
		PromptValuesSupported:                     []string{oidc.PromptLogin, oidc.PromptConsent, "create", oidc.PromptSelectAccount, oidc.PromptNone},
	})
}
