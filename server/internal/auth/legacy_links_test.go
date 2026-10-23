package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// The links in mail forma2 sent through Better Auth land on the SPA page of the same purpose, in its expired-link state: without the token or the callback URL they carried.
func TestLegacyBetterAuthLinks(t *testing.T) {
	t.Parallel()
	r := gin.New()
	registerLegacyLinks(r)
	for link, want := range map[string]string{
		"/api/auth/reset-password/tok123?callbackURL=https%3A%2F%2Fapp.example%2Fauth%2Fforgot-password%2Freset": "/auth/forgot-password/reset",
		"/api/auth/verify-email?token=eyJhbGciOi.x.y&callbackURL=%2F":                                            "/auth/verify-email",
		"/api/auth/delete-user/callback?token=tok123&callbackURL=%2Fauth%2Flogin":                                "/auth/delete-account",
	} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, link, nil))
		if rec.Code != http.StatusFound || rec.Header().Get("Location") != want {
			t.Errorf("%s: %d to %q, want 302 to %q", link, rec.Code, rec.Header().Get("Location"), want)
		}
	}
}
