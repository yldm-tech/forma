package auth

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
)

// Better Auth's link endpoints, which the password reset, email verification and account deletion mails forma2 sent before the cutover point at: /api/auth/reset-password/{token}?callbackURL=..., /api/auth/verify-email?token=...&callbackURL=... and /api/auth/delete-user/callback?token=...&callbackURL=.... This server does not redeem Better Auth's tokens, so such a link can only have expired: each one leads to the SPA page its mail belonged to without a token, which is that page's "this link is no longer valid" state, from where a new link is requested.
const (
	legacyResetPasswordPath = "/api/auth/reset-password/:token" //nolint:gosec // a route pattern, not a credential
	legacyVerifyEmailPath   = "/api/auth/verify-email"
	legacyDeleteUserPath    = "/api/auth/delete-user/callback"
)

// registerLegacyLinks mounts the Better Auth link endpoints. The token and the callback URL in the query are dropped, so neither reaches the page or its logs.
func registerLegacyLinks(r gin.IRouter) {
	r.GET(legacyResetPasswordPath, redirectTo(resetPasswordPath))
	r.GET(legacyVerifyEmailPath, redirectTo(verifyEmailPath))
	r.GET(legacyDeleteUserPath, redirectTo(deleteAccountPath))
}

// redirectTo sends the browser to the SPA page at path, under the app's base path.
func redirectTo(path string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Redirect(http.StatusFound, config.AppBasePath+path)
	}
}
