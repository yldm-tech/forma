package oauthserver

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"html"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	jose "github.com/go-jose/go-jose/v4"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/auth"
	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/platform/httpapi"
)

// endSessionPath is RP-initiated logout (OpenID Connect RP-Initiated Logout 1.0), forma2's Better Auth /oauth2/end-session, which the discovery document advertises.
const endSessionPath = "/oauth2/end-session"

// endSessionConfirmPath is where the confirmation page's form posts, Better Auth's /oauth2/end-session/confirm.
const endSessionConfirmPath = endSessionPath + "/confirm"

// logoutConfirmationTTL is how long a confirmation page may be submitted, Better Auth's LOGOUT_CONFIRMATION_TTL_SECONDS.
const logoutConfirmationTTL = 300 * time.Second

// logoutConfirmationCookieName is Better Auth's: the session cookie's name with ".oauth_logout_confirmation" appended.
const logoutConfirmationCookieName = "forma.session_token.oauth_logout_confirmation"

// Sessions is what the authorization server needs of the credential module's sessions: the middleware that resolves the browser's session, and ending it.
type Sessions interface {
	httpapi.SessionAuth
	// End deletes the session and clears the browser's session cookie.
	End(c *gin.Context, sessionID uuid.UUID) error
}

var _ Sessions = (*auth.Sessions)(nil)

// logoutConfirmation signs the state the confirmation page hands its form through a cookie, Better Auth's signed logout confirmation cookie.
type logoutConfirmation struct {
	name   string
	path   string
	secure bool
	key    []byte
}

func newLogoutConfirmation(cfg config.Config, urls URLs) logoutConfirmation {
	name := logoutConfirmationCookieName
	if cfg.SecureCookies() {
		name = "__Secure-" + name
	}
	path := endSessionConfirmPath
	if u, err := url.Parse(urls.Issuer + endSessionConfirmPath); err == nil {
		path = u.Path
	}
	// The signing key is derived from the encryption key, as the other tokens of an instance without forma2's secrets are, so it needs no configuration of its own.
	mac := hmac.New(sha256.New, cfg.EncryptionKey)
	mac.Write([]byte("forma.oauth-logout-confirmation.v1"))
	return logoutConfirmation{name: name, path: path, secure: cfg.SecureCookies(), key: mac.Sum(nil)}
}

// confirmationState is what a confirmation page was shown for: the browser session it may end, when there was one. forma2's clients could not initiate logout, so the state never carried a client's post-logout redirect.
type confirmationState struct {
	SessionID string `json:"sessionId,omitempty"`
	ExpiresAt int64  `json:"expiresAt"`
}

func (l logoutConfirmation) sign(payload []byte) string {
	mac := hmac.New(sha256.New, l.key)
	mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (l logoutConfirmation) cookie(c *gin.Context, value string, maxAge int) {
	//nolint:gosec // Secure follows WEBAPP_URL's scheme, as the session cookie's does: browsers reject Secure cookies on a plain-HTTP development instance.
	http.SetCookie(c.Writer, &http.Cookie{Name: l.name, Value: value, Path: l.path, MaxAge: maxAge, Secure: l.secure, HttpOnly: true, SameSite: http.SameSiteLaxMode})
}

func (l logoutConfirmation) set(c *gin.Context, sessionID uuid.UUID, now time.Time) {
	state := confirmationState{ExpiresAt: now.Add(logoutConfirmationTTL).UnixMilli()}
	if sessionID != uuid.Nil {
		state.SessionID = sessionID.String()
	}
	payload, _ := json.Marshal(state)
	l.cookie(c, base64.RawURLEncoding.EncodeToString(payload)+"."+l.sign(payload), int(logoutConfirmationTTL/time.Second))
}

// read is the request's confirmation state; nil when it is missing, forged or malformed.
func (l logoutConfirmation) read(c *gin.Context) *confirmationState {
	value, err := c.Cookie(l.name)
	if err != nil {
		return nil
	}
	encoded, signature, ok := strings.Cut(value, ".")
	if !ok {
		return nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || !hmac.Equal([]byte(signature), []byte(l.sign(payload))) {
		return nil
	}
	var state confirmationState
	if json.Unmarshal(payload, &state) != nil || state.ExpiresAt == 0 {
		return nil
	}
	return &state
}

func (l logoutConfirmation) clear(c *gin.Context) {
	l.cookie(c, "", -1)
}

// currentSession is the browser session the Optional middleware resolved, if any.
func currentSession(c *gin.Context) (httpapi.Actor, bool) {
	return httpapi.ActorFrom(c.Request.Context())
}

// endSession is Better Auth's rpInitiatedLogoutEndpoint for the clients forma2 could have: Better Auth let a client initiate logout only when it was registered with enable_end_session, which dynamic registration never set, so a request that names a client, directly or through its id_token_hint, is refused with Better Auth's errors. A request naming no client, or a hint naming no client known here while a browser is signed in, asks the browser to confirm: a confirmation page whose form posts to endSessionConfirmPath. A caller that is not a browser navigation cannot confirm, so it is refused.
func (h *Handler) endSession(c *gin.Context) {
	if err := c.Request.ParseForm(); err != nil {
		h.logoutError(c, http.StatusBadRequest, "invalid_request", "The logout request cannot be parsed")
		return
	}
	actor, signedIn := currentSession(c)
	clientID := c.Request.Form.Get("client_id")
	hint := c.Request.Form.Get("id_token_hint")
	_, hasHint := c.Request.Form["id_token_hint"]
	if !hasHint && clientID == "" {
		h.confirmationRequired(c, actor, signedIn)
		return
	}
	if clientID == "" {
		clientID = hintClientID(hint)
	}
	var logoutClient *Client
	if clientID != "" {
		var err error
		if logoutClient, err = h.repo.client(c.Request.Context(), clientID); err != nil {
			h.fail(c, err)
			return
		}
	}
	switch {
	case logoutClient == nil && hasHint && signedIn && browserNavigation(c.Request):
		h.confirmationRequired(c, actor, signedIn)
	case logoutClient == nil:
		h.logoutError(c, http.StatusBadRequest, "invalid_client", "The logout client does not exist")
	case logoutClient.disabled:
		h.logoutError(c, http.StatusBadRequest, "invalid_client", "The logout client is disabled")
	default:
		h.logoutError(c, http.StatusUnauthorized, "invalid_client", "The client is not allowed to initiate logout")
	}
}

// confirmationRequired is Better Auth's: a browser navigation gets the confirmation page and a signed state naming its session; anything else is refused, since only the user can confirm.
func (h *Handler) confirmationRequired(c *gin.Context, actor httpapi.Actor, signedIn bool) {
	if !browserNavigation(c.Request) {
		if !signedIn {
			h.logoutError(c, http.StatusBadRequest, "invalid_request", "No active session is available for logout")
			return
		}
		h.logoutError(c, http.StatusBadRequest, "invalid_request", "User confirmation is required to complete logout")
		return
	}
	sessionID := uuid.Nil
	if signedIn {
		sessionID = actor.SessionID
	}
	h.confirmation.set(c, sessionID, h.now())
	action := html.EscapeString(h.urls.Issuer + endSessionConfirmPath)
	logoutPage(c, http.StatusOK, "Confirm logout", `<main><h1>Confirm logout</h1><p>Do you want to log out of this account?</p><form method="post" data-oidc-logout-confirmation action="`+action+`"><button type="submit" name="action" value="confirm">Confirm logout</button></form></main>`)
}

// confirmEndSession is Better Auth's rpInitiatedLogoutConfirmationEndpoint: the confirmation form's post, from this server's own page (its Origin is checked and the signed state is a SameSite=Lax cookie scoped to this path, so another site can neither forge nor replay it). It ends the session the page was shown for and clears the session cookie.
func (h *Handler) confirmEndSession(c *gin.Context) {
	if !h.trustedOrigin(c.Request) {
		h.logoutError(c, http.StatusForbidden, "invalid_request", "Invalid origin")
		return
	}
	if err := c.Request.ParseForm(); err != nil || c.Request.PostForm.Get("action") != "confirm" {
		h.logoutError(c, http.StatusBadRequest, "invalid_request", "The logout request cannot be parsed")
		return
	}
	state := h.confirmation.read(c)
	actor, signedIn := currentSession(c)
	if state == nil || state.ExpiresAt <= h.now().UnixMilli() {
		h.logoutError(c, http.StatusBadRequest, "invalid_request", "The logout confirmation is invalid or expired")
		return
	}
	if !signedIn {
		h.confirmation.clear(c)
		if browserNavigation(c.Request) {
			loggedOutPage(c)
			return
		}
		h.logoutError(c, http.StatusBadRequest, "invalid_request", "No active session is available for logout")
		return
	}
	if state.SessionID != "" && state.SessionID != actor.SessionID.String() {
		h.logoutError(c, http.StatusBadRequest, "invalid_request", "The logout confirmation is invalid or expired")
		return
	}
	if err := h.sessions.End(c, actor.SessionID); err != nil {
		h.log.ErrorContext(c.Request.Context(), "Failed to delete the logout session", "err", err)
		oauthError(c, http.StatusInternalServerError, "server_error", "Unable to complete logout")
		return
	}
	h.confirmation.clear(c)
	if browserNavigation(c.Request) {
		loggedOutPage(c)
		return
	}
	// Better Auth answered a confirmed logout that was not a browser navigation with an empty body.
	c.Header("Cache-Control", "no-store")
	c.Status(http.StatusOK)
}

// trustedOrigin is Better Auth's origin check of a cookie-carrying POST: the Origin must be the authorization server's own or the web app's.
func (h *Handler) trustedOrigin(r *http.Request) bool {
	o := strings.ToLower(r.Header.Get("Origin"))
	if o == "" || o == "null" {
		return false
	}
	for _, trusted := range []string{h.urls.Issuer, h.urls.WebApp} {
		if u, err := url.Parse(trusted); err == nil && o == auth.URLOrigin(u) {
			return true
		}
	}
	return false
}

// hintClientID is the client an id_token_hint was issued to, Better Auth's getHintClientId: a string aud, else azp, else the only audience. The token is only read to name the client; nothing is granted on it.
func hintClientID(hint string) string {
	jws, err := jose.ParseSigned(hint, []jose.SignatureAlgorithm{jose.EdDSA, jose.RS256, jose.ES256})
	if err != nil {
		return ""
	}
	var claims struct {
		Audience json.RawMessage `json:"aud"`
		AZP      string          `json:"azp"`
	}
	if json.Unmarshal(jws.UnsafePayloadWithoutVerification(), &claims) != nil {
		return ""
	}
	var single string
	if json.Unmarshal(claims.Audience, &single) == nil && single != "" {
		return single
	}
	if claims.AZP != "" {
		return claims.AZP
	}
	var list []string
	if json.Unmarshal(claims.Audience, &list) == nil && len(list) == 1 {
		return list[0]
	}
	return ""
}

// browserNavigation is Better Auth's isBrowserNavigation: a top-level navigation, or a request accepting HTML that is not a script's fetch.
func browserNavigation(r *http.Request) bool {
	mode := r.Header.Get("Sec-Fetch-Mode")
	if mode != "" && mode != "navigate" {
		return false
	}
	accept := r.Header.Get("Accept")
	return mode == "navigate" || strings.Contains(accept, "text/html") || strings.Contains(accept, "application/xhtml+xml")
}

// logoutPage is Better Auth's logoutPage: a minimal HTML document that is never cached, framed or sniffed.
func logoutPage(c *gin.Context, status int, title, body string) {
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Security-Policy", "default-src 'none'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
	page := `<!doctype html><html><head><meta charset="utf-8"><title>` + html.EscapeString(title) + `</title></head><body>` + body + `</body></html>`
	c.Data(status, "text/html; charset=utf-8", []byte(page))
	c.Abort()
}

// loggedOutPage is Better Auth's logoutSuccessPage without a post-logout redirect.
func loggedOutPage(c *gin.Context) {
	logoutPage(c, http.StatusOK, "Logged out", `<main><p data-oidc-logout-state="logged-out">Logged out.</p></main>`)
}

// logoutError is Better Auth's logoutProtocolError: an error page for a browser, an OAuth error body for anything else.
func (h *Handler) logoutError(c *gin.Context, status int, errorType, description string) {
	if !browserNavigation(c.Request) {
		oauthError(c, status, errorType, description)
		return
	}
	logoutPage(c, status, "Logout error", `<main><h1>Logout error</h1><p data-oidc-logout-state="error">`+html.EscapeString(description)+`</p></main>`)
}
