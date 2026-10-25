package v3api

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/apikey"
	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/apps/server/internal/platform/ratelimit"
)

// AuthMode is which credentials a route accepts, forma2's TV3AuthMode.
type AuthMode int

const (
	// AuthSession accepts a signed-in browser only; an API key is never consulted.
	AuthSession AuthMode = iota
	// AuthBoth accepts a session or an API key; a request that carries a key is authenticated by the key alone.
	AuthBoth
	// AuthBothOrganizationKey is AuthBoth that also accepts a key with no workspace grant, only organization access. It is for routes that reveal nothing beyond the key's own grants, such as GET /api/v3/me; every other v3 route refuses such a key.
	AuthBothOrganizationKey
)

// Principal is who a v3 request acts as: a signed-in user, or an API key.
type Principal struct {
	UserID uuid.UUID
	APIKey *apikey.Principal
}

// Actor is the principal as an authorization subject.
func (p *Principal) Actor() authz.Actor {
	if p.APIKey != nil {
		return p.APIKey.Actor()
	}
	return authz.User(p.UserID)
}

type principalKey struct{}

// PrincipalFrom returns the principal Auth recorded. Every v3 route is mounted behind Auth.
func PrincipalFrom(ctx context.Context) *Principal {
	p, _ := ctx.Value(principalKey{}).(*Principal)
	return p
}

// Middleware authenticates v3 requests and applies their rate limit.
type Middleware struct {
	keys    *apikey.Authenticator
	limiter *ratelimit.Limiter
	csrf    func(*http.Request) *apperr.Error
}

func NewMiddleware(cfg config.Config, keys *apikey.Authenticator, limiter *ratelimit.Limiter) *Middleware {
	return &Middleware{keys: keys, limiter: limiter, csrf: httpapi.CSRFCheck(cfg)}
}

// Auth is the front half of forma2's withV3ApiWrapper for one route: authenticate (401 when nothing valid was presented), then the api:v3 limit of 100 requests a minute per user or API key. The session itself is read by the group's SessionAuth.Optional, which runs before this.
//
// forma2's route handlers trusted the session cookie's SameSite attribute alone; a session-authenticated mutation here must also pass the /api/app CSRF rule (an allowed Origin and the X-Requested-With header the SPA always sends). API keys are not ambient credentials and are exempt.
func (m *Middleware) Auth(mode AuthMode) gin.HandlerFunc {
	return func(c *gin.Context) {
		p, err := m.authenticate(c, mode)
		if err != nil {
			Fail(c, err)
			return
		}
		if p == nil {
			detail := "Not authenticated"
			if mode == AuthSession {
				detail = "Session required"
			}
			Fail(c, Unauthorized(detail))
			return
		}
		if p.APIKey == nil {
			if e := m.csrf(c.Request); e != nil {
				Fail(c, Forbidden(e.Message))
				return
			}
		}
		if err := m.limiter.Apply(c.Request.Context(), ratelimit.APIV3, p.Actor().ID.String()); err != nil {
			Fail(c, err)
			return
		}
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), principalKey{}, p))
		c.Next()
	}
}

func (m *Middleware) authenticate(c *gin.Context, mode AuthMode) (*Principal, error) {
	ctx := c.Request.Context()
	if mode != AuthSession && apikey.KeyFromHeaders(c.Request.Header) != "" {
		return m.apiKey(ctx, c.Request.Header, apikey.Options{AllowOrganizationOnly: mode == AuthBothOrganizationKey})
	}
	if a, ok := httpapi.ActorFrom(ctx); ok {
		return &Principal{UserID: a.UserID}, nil
	}
	return nil, nil
}

// apiKey resolves the request's key. v3 never accepted a key without a workspace grant; only AuthBothOrganizationKey routes pass opts that allow one.
func (m *Middleware) apiKey(ctx context.Context, h http.Header, opts apikey.Options) (*Principal, error) {
	key, err := m.keys.Authenticate(ctx, h, opts)
	if err != nil || key == nil {
		return nil, err
	}
	return &Principal{APIKey: key}, nil
}

// RequireWorkspace is forma2's requireV3WorkspaceAccess: the caller must be granted action on the workspace. A workspace that does not exist is refused exactly like one the caller may not see (403, never 404).
func RequireWorkspace(ctx context.Context, authorizer *authz.Authorizer, p *Principal, action authz.Action, workspaceID uuid.UUID) error {
	allowed, err := authorizer.Can(ctx, p.Actor(), action, authz.Workspace(workspaceID))
	if err != nil {
		return err
	}
	if !allowed {
		return Forbidden(ForbiddenDetail)
	}
	return nil
}
