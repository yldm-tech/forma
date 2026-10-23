package oauthserver

import (
	"encoding/json"
	"html"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	jose "github.com/go-jose/go-jose/v4"
)

// endSessionPath is RP-initiated logout (OpenID Connect RP-Initiated Logout 1.0), forma2's Better Auth /oauth2/end-session, which the discovery document advertises.
const endSessionPath = "/oauth2/end-session"

// endSession answers RP-initiated logout as forma2 did for every client it could have: Better Auth let a client initiate logout only when it was registered with enable_end_session, which dynamic registration never set, so a request that names a client, directly or through its id_token_hint, is refused with Better Auth's errors. A request naming no client asked Better Auth to confirm signing the browser out; Forma's sign-out lives in the web app, so a browser is sent there and any other caller gets Better Auth's answer for a request without a session to end.
func (h *Handler) endSession(c *gin.Context) {
	if err := c.Request.ParseForm(); err != nil {
		h.logoutError(c, http.StatusBadRequest, "invalid_request", "The logout request cannot be parsed")
		return
	}
	clientID := c.Request.Form.Get("client_id")
	hint := c.Request.Form.Get("id_token_hint")
	if clientID == "" && hint != "" {
		clientID = hintClientID(hint)
	}
	if clientID == "" {
		if hint == "" && browserNavigation(c.Request) {
			c.Redirect(http.StatusFound, h.urls.WebApp+"/")
			return
		}
		if hint == "" {
			h.logoutError(c, http.StatusBadRequest, "invalid_request", "No active session is available for logout")
			return
		}
		h.logoutError(c, http.StatusBadRequest, "invalid_client", "The logout client does not exist")
		return
	}
	client, err := h.repo.client(c.Request.Context(), clientID)
	if err != nil {
		h.fail(c, err)
		return
	}
	switch {
	case client == nil:
		h.logoutError(c, http.StatusBadRequest, "invalid_client", "The logout client does not exist")
	case client.disabled:
		h.logoutError(c, http.StatusBadRequest, "invalid_client", "The logout client is disabled")
	default:
		h.logoutError(c, http.StatusUnauthorized, "invalid_client", "The client is not allowed to initiate logout")
	}
}

// hintClientID is the client an id_token_hint was issued to, Better Auth's getHintClientId: a string aud, else azp, else the only audience. The token is only read to name the client in the refusal; nothing is granted on it.
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

// logoutError is Better Auth's logoutProtocolError: an error page for a browser, an OAuth error body for anything else.
func (h *Handler) logoutError(c *gin.Context, status int, errorType, description string) {
	if !browserNavigation(c.Request) {
		oauthError(c, status, errorType, description)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	c.Header("Content-Security-Policy", "default-src 'none'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
	page := `<!doctype html><html><head><meta charset="utf-8"><title>Logout error</title></head><body><main><h1>Logout error</h1><p data-oidc-logout-state="error">` + html.EscapeString(description) + `</p></main></body></html>`
	c.Data(status, "text/html; charset=utf-8", []byte(page))
	c.Abort()
}
