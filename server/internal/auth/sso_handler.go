package auth

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/platform/httpapi"
)

// SSOHandler serves single sign-on: starting a sign-in and completing SSO recovery under /api/app/auth/sso, and the identity providers' callbacks at forma2's paths.
type SSOHandler struct {
	sso     *SSO
	cookies cookieSettings
}

func NewSSOHandler(cfg config.Config, sso *SSO) *SSOHandler {
	return &SSOHandler{sso: sso, cookies: newCookieSettings(cfg)}
}

// Register mounts the session-API routes on the /api/app group, which applies the error envelope and the CSRF check.
func (h *SSOHandler) Register(r gin.IRouter) {
	g := r.Group("/auth/sso")
	g.POST("/sign-in", h.signIn)
	g.POST("/recovery/complete", h.completeRecovery)
}

// RegisterCallbacks mounts the redirect URIs registered at the identity providers, which must stay byte for byte what forma2 served: /api/auth/callback/{provider} for every provider (Better Auth's route), and /api/auth/oauth2/callback/{azuread|openid}, the URI Azure AD and the generic provider have been registered with since forma v5.2.
func (h *SSOHandler) RegisterCallbacks(r gin.IRouter) {
	r.GET("/api/auth/callback/:provider", h.callback)
	r.GET("/api/auth/oauth2/callback/:provider", h.legacyCallback)
}

type ssoSignInRequest struct {
	Provider    string `json:"provider"`
	CallbackURL string `json:"callbackUrl"`
}

func (r *ssoSignInRequest) validate() error {
	var errs fieldErrors
	if r.Provider == "" {
		errs.add("provider", "required", "")
	}
	return errs.err()
}

type ssoSignInResponse struct {
	// URL is the identity provider's authorization page, where the SPA navigates.
	URL string `json:"url"`
}

// signIn starts a sign-in, forma2's POST /api/auth/sign-in/social. The response sets the state cookie the callback checks.
func (h *SSOHandler) signIn(c *gin.Context) {
	req := &ssoSignInRequest{}
	if !bind(c, req) {
		return
	}
	authURL, state, err := h.sso.start(c.Request.Context(), req.Provider, req.CallbackURL)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	h.cookies.set(c, h.cookies.oauthState, state)
	c.JSON(http.StatusOK, ssoSignInResponse{URL: authURL})
}

func (h *SSOHandler) callback(c *gin.Context) {
	h.finish(c, c.Param("provider"))
}

// legacyCallback serves the pinned paths of the providers forma2 pinned there; others were never served at it.
func (h *SSOHandler) legacyCallback(c *gin.Context) {
	provider := c.Param("provider")
	if provider != providerAzureAD && provider != providerOpenID {
		c.Status(http.StatusNotFound)
		return
	}
	h.finish(c, provider)
}

// finish completes the flow and redirects. The state cookie is single-use and cleared on every outcome; the URL, which carries the authorization code, is never logged.
func (h *SSOHandler) finish(c *gin.Context, provider string) {
	state, _ := c.Cookie(h.cookies.oauthState)
	res := h.sso.callback(c.Request.Context(), provider, c.Request.URL.Query(), state, c.GetHeader("Accept-Language"), clientMetaFrom(c))
	h.cookies.clear(c, h.cookies.oauthState)
	if res.session != nil {
		h.cookies.setSession(c, *res.session)
	}
	c.Header("Cache-Control", "no-store")
	c.Redirect(http.StatusFound, res.redirect)
}

type ssoRecoveryResponse struct {
	// RedirectURL is where the SPA navigates now that the browser is signed in.
	RedirectURL string `json:"redirectUrl"`
}

// completeRecovery redeems the emailed SSO recovery link. It is a POST behind the CSRF check, so only the SPA page the link opens can redeem it, never a cross-site request.
func (h *SSOHandler) completeRecovery(c *gin.Context) {
	req := &tokenRequest{}
	if !bind(c, req) {
		return
	}
	redirect, sess, err := h.sso.completeRecovery(c.Request.Context(), req.Token, clientMetaFrom(c))
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	h.cookies.setSession(c, sess)
	c.JSON(http.StatusOK, ssoRecoveryResponse{RedirectURL: redirect})
}
