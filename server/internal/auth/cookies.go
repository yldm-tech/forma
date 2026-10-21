package auth

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/yldm-tech/forma/server/internal/platform/config"
)

// Cookie names keep forma2's "forma" prefix (Better Auth's cookiePrefix) and its __Secure- prefix over HTTPS.
const (
	sessionCookieName      = "forma.session_token"
	twoFactorCookieName    = "forma.two_factor"
	signupIntentCookieName = "forma.signup_intent"
)

// cookieSettings writes the three auth cookies with forma2's attributes: HttpOnly, SameSite=Lax, Path=/, and Secure when the app is served over HTTPS.
type cookieSettings struct {
	secure       bool
	session      string
	twoFactor    string
	signupIntent string
}

func newCookieSettings(cfg config.Config) cookieSettings {
	prefix := ""
	if cfg.SecureCookies() {
		prefix = "__Secure-"
	}
	return cookieSettings{
		secure:       cfg.SecureCookies(),
		session:      prefix + sessionCookieName,
		twoFactor:    prefix + twoFactorCookieName,
		signupIntent: prefix + signupIntentCookieName,
	}
}

func (s cookieSettings) set(c *gin.Context, name string, t issuedToken) {
	maxAge := int(t.MaxAge / time.Second)
	if maxAge <= 0 {
		maxAge = -1
	}
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     name,
		Value:    t.Value,
		Path:     "/",
		MaxAge:   maxAge,
		Secure:   s.secure,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

// clear deletes a cookie with the attributes it was set with, so the browser matches and drops it.
func (s cookieSettings) clear(c *gin.Context, name string) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Secure:   s.secure,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s cookieSettings) setSession(c *gin.Context, t issuedToken) {
	s.set(c, s.session, t)
}
