package httpapi

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// Actor is who a request acts as. It lives in the request context so services, which take a context.Context rather than a *gin.Context, can attribute audit events without a parameter on every call.
type Actor struct {
	UserID    uuid.UUID
	SessionID uuid.UUID
}

type actorKey struct{}

// SetActor records the authenticated actor for the rest of the request. Only a SessionAuth implementation calls it.
func SetActor(c *gin.Context, a Actor) {
	c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), actorKey{}, a))
}

// ActorFrom returns the request's actor, or false when the request is unauthenticated.
func ActorFrom(ctx context.Context) (Actor, bool) {
	a, ok := ctx.Value(actorKey{}).(Actor)
	return a, ok
}

// SessionAuth is the session middleware the credential module provides. Routes depend on this contract, not on how sessions are stored.
type SessionAuth interface {
	// Require answers 401 not_authenticated ("Not authenticated") when the request has no valid session, and otherwise calls SetActor before the handler runs.
	Require() gin.HandlerFunc
	// Optional calls SetActor when the request has a valid session and lets every request through.
	Optional() gin.HandlerFunc
}
