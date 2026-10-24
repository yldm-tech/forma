package ratelimit

import (
	"github.com/gin-gonic/gin"

	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpapi"
)

// Identify picks the identifier a route is limited by. It returns an error when the request cannot be attributed, and that error is the response.
type Identify func(c *gin.Context) (string, error)

// ByClientIP limits by the hashed client address; it is the identifier for unauthenticated endpoints.
func ByClientIP(c *gin.Context) (string, error) {
	return IPIdentifier(c.Request.Context()), nil
}

// ByUser limits by the authenticated user. Mount it after the session middleware; an unauthenticated request is refused.
func ByUser(c *gin.Context) (string, error) {
	actor, ok := httpapi.ActorFrom(c.Request.Context())
	if !ok {
		return "", apperr.NotAuthenticated("Not authenticated")
	}
	return actor.UserID.String(), nil
}

// Middleware charges one unit of rule per request before the handler runs and answers 429 with Retry-After once the window is spent. Limits that depend on the request body (a weighted invite, a per-organization budget) belong in the service, through Limiter.ApplyN.
func Middleware(l *Limiter, rule Rule, identify Identify) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := identify(c)
		if err == nil {
			err = l.Apply(c.Request.Context(), rule, id)
		}
		if err != nil {
			httpapi.Fail(c, err)
			return
		}
		c.Next()
	}
}
