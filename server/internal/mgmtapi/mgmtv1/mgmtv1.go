// Package mgmtv1 is forma2's withV1ApiWrapper (apps/web/lib/api/with-api-logging.ts) for the v1 routes external tools call with an API key: /api/v1/management/* and /api/v1/webhooks. It authenticates the caller, charges the route's rate limit, records the audit event of a mutation once the handler has answered, and resolves the workspace a request body names (resolveBodyIds). Handlers answer through platform/v1api.
package mgmtv1

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/apikey"
	"github.com/yldm-tech/forma/server/internal/authz"
	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/platform/audit"
	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/server/internal/platform/httpserver"
	"github.com/yldm-tech/forma/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/server/internal/platform/v1api"
	"github.com/yldm-tech/forma/server/internal/workspace"
)

// Messages of the v1 API's unexpected failures: an error escaping the handler (forma2's wrapper) and one its handleApiError caught.
const (
	MessageUnexpected = "An unexpected error occurred."
	MessageGeneric    = "Something went wrong. Please try again."
)

// RouteVary is the Vary header Next.js added to every route handler's response.
const RouteVary = "rsc, next-router-state-tree, next-router-prefetch, next-router-segment-prefetch"

// Wrapper holds what the v1 routes share.
type Wrapper struct {
	cfg        config.Config
	keys       *apikey.Authenticator
	limiter    *ratelimit.Limiter
	audit      *audit.Emitter
	authz      *authz.Authorizer
	workspaces *workspace.Repository
	csrf       func(r *http.Request) *apperr.Error
	log        *slog.Logger
}

func New(cfg config.Config, keys *apikey.Authenticator, limiter *ratelimit.Limiter, emitter *audit.Emitter, authorizer *authz.Authorizer, workspaces *workspace.Repository, log *slog.Logger) *Wrapper {
	return &Wrapper{cfg: cfg, keys: keys, limiter: limiter, audit: emitter, authz: authorizer, workspaces: workspaces, csrf: httpapi.CSRFCheck(cfg), log: log}
}

// Caller is who a v1 request acts as: an API key, or on the routes that also accept one, a signed-in user.
type Caller struct {
	Key    *apikey.Principal
	UserID uuid.UUID
}

// Actor is the caller as an authorization subject.
func (c Caller) Actor() authz.Actor {
	if c.Key != nil {
		return c.Key.Actor()
	}
	return authz.User(c.UserID)
}

// Context keys of the caller and the audit record.
const (
	callerKey = "mgmtv1.caller"
	auditKey  = "mgmtv1.audit"
)

// CallerOf is the request's authenticated caller.
func CallerOf(c *gin.Context) Caller {
	v, _ := c.Get(callerKey)
	caller, _ := v.(Caller)
	return caller
}

// PrincipalOf is the request's API key; nil on a session-authenticated request.
func PrincipalOf(c *gin.Context) *apikey.Principal {
	return CallerOf(c).Key
}

// Front sets what every response of these routes carries regardless of its handler, Next.js's Vary, and answers a failure a middleware recorded with httpapi.Fail (a session that could not be read) as v1's unexpected error, since these routes have no /api/app error envelope.
func Front(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Vary", RouteVary)
		c.Next()
		if len(c.Errors) == 0 || c.Writer.Written() {
			return
		}
		log.ErrorContext(c.Request.Context(), "v1 management request failed", "route", c.FullPath(), "err", c.Errors.Last().Err)
		v1api.InternalError(c, MessageUnexpected)
	}
}

// APIKey is the front of forma2's withV1ApiWrapper for a route that accepts an API key only (a session is never read): a key valid for opts, then rule charged to the key. A missing or invalid key is 401 not_authenticated and charges nothing.
func (w *Wrapper) APIKey(opts apikey.Options, rule ratelimit.Rule) gin.HandlerFunc {
	return func(c *gin.Context) {
		p, err := w.keys.Authenticate(c.Request.Context(), c.Request.Header, opts)
		if err != nil {
			w.Unexpected(c, err, MessageUnexpected)
			return
		}
		if p == nil {
			v1api.NotAuthenticated(c)
			return
		}
		c.Set(callerKey, Caller{Key: p})
		if w.limited(c, rule, p.APIKeyID.String()) {
			return
		}
		c.Next()
	}
}

// APIKeyOrSession is the front of a route that accepts either (forma2's "Both"): a request carrying an API key is authenticated by it alone, an invalid one never falling back to the session; otherwise the session read by sessionOptional before it decides. A session-authenticated mutation must also pass the CSRF check the /api/app routes apply, which forma2 did not: an API key cannot be sent by another site, a cookie can. rule is charged to the key or the user.
func (w *Wrapper) APIKeyOrSession(sessionOptional gin.HandlerFunc, rule ratelimit.Rule) []gin.HandlerFunc {
	readSession := func(c *gin.Context) {
		if apikey.KeyFromHeaders(c.Request.Header) != "" {
			c.Next()
			return
		}
		sessionOptional(c)
	}
	authenticate := func(c *gin.Context) {
		if apikey.KeyFromHeaders(c.Request.Header) != "" {
			w.APIKey(apikey.Options{}, rule)(c)
			return
		}
		actor, ok := httpapi.ActorFrom(c.Request.Context())
		if !ok {
			v1api.NotAuthenticated(c)
			return
		}
		if e := w.csrf(c.Request); e != nil {
			v1api.Forbidden(c, e.Message, false)
			return
		}
		c.Set(callerKey, Caller{UserID: actor.UserID})
		if w.limited(c, rule, actor.UserID.String()) {
			return
		}
		c.Next()
	}
	return []gin.HandlerFunc{readSession, authenticate}
}

// limited charges rule to identifier and answers when the request may not proceed: 429 at the limit, 500 when the limiter itself failed.
func (w *Wrapper) limited(c *gin.Context, rule ratelimit.Rule, identifier string) bool {
	err := w.limiter.Apply(c.Request.Context(), rule, identifier)
	if err == nil {
		return false
	}
	if e, ok := apperr.As(err); ok && e.Status == http.StatusTooManyRequests {
		v1api.TooManyRequests(c, apperr.TooManyRequestsMessage)
		return true
	}
	w.Unexpected(c, err, MessageGeneric)
	return true
}

// Unexpected logs an error the handler cannot answer otherwise and answers 500 with message.
func (w *Wrapper) Unexpected(c *gin.Context, err error, message string) {
	w.log.ErrorContext(c.Request.Context(), "v1 management request failed", "route", c.FullPath(), "err", err)
	v1api.InternalError(c, message)
}

// Audit is the audit record of one v1 mutation, filled in by the handler as it learns the target.
type Audit struct {
	TargetID  string
	OldObject any
	NewObject any
	// Failed marks a 2xx answer that is still a failure, as forma2's handlers marked a delete that did not happen.
	Failed bool
}

// Audited records the request's audit event once the handler has answered, as forma2's wrapper did: the API key (or the signed-in user) as the actor in the key's organization, success only for a 2xx the handler did not mark failed, and the request id on a failure. A request refused before the handler ran (no caller, rate limited) is not recorded.
func (w *Wrapper) Audited(action audit.Action, target audit.Target, handler gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		rec := &Audit{TargetID: audit.Unknown}
		c.Set(auditKey, rec)
		handler(c)
		caller := CallerOf(c)
		e := audit.Event{
			Action: action, TargetType: target, TargetID: rec.TargetID, ActorType: audit.ActorAPI, Status: audit.StatusFailure,
			OldObject: rec.OldObject, NewObject: rec.NewObject,
			APIURL: w.cfg.WebAppURL.Scheme + "://" + w.cfg.WebAppURL.Host + c.Request.URL.RequestURI(),
		}
		if caller.Key != nil {
			e.ActorID, e.OrganizationID = caller.Key.APIKeyID.String(), caller.Key.OrganizationID.String()
		} else {
			e.ActorID, e.OrganizationID = caller.UserID.String(), audit.Unknown
		}
		if status := c.Writer.Status(); status >= 200 && status < 300 && !rec.Failed {
			e.Status = audit.StatusSuccess
		} else {
			e.EventID = httpserver.RequestID(c)
		}
		ctx := c.Request.Context()
		w.audit.Log(ctx, audit.FromRequest(ctx, e))
	}
}

// AuditOf is the request's audit record; a route that is not audited gets a record nobody reads.
func AuditOf(c *gin.Context) *Audit {
	v, _ := c.Get(auditKey)
	rec, _ := v.(*Audit)
	if rec == nil {
		rec = &Audit{}
	}
	return rec
}

// Can is forma2's can(caller, workspace.<method's action>, workspace).
func (w *Wrapper) Can(c *gin.Context, method string, workspaceID uuid.UUID) (bool, error) {
	return w.authz.Can(c.Request.Context(), CallerOf(c).Actor(), authz.WorkspaceActionForMethod(method), authz.Workspace(workspaceID))
}

// Authorize is Can answering for the handler: 401 unauthorized when refused, 500 when the decision failed. It reports whether the handler may go on.
func (w *Wrapper) Authorize(c *gin.Context, method string, workspaceID uuid.UUID) bool {
	allowed, err := w.Can(c, method, workspaceID)
	if err != nil {
		w.Unexpected(c, err, MessageUnexpected)
		return false
	}
	if !allowed {
		v1api.Unauthorized(c)
		return false
	}
	return true
}

// ReadBody reads the JSON object body as forma2's v1 routes did and answers when it cannot: 413 over the limit, 400 when it is not JSON. A body that is JSON but not an object reads as an object without members.
func ReadBody(c *gin.Context) (map[string]any, bool) {
	body, err := v1api.ReadJSONObject(c.Request)
	if err != nil {
		if errors.Is(err, v1api.ErrBodyTooLarge) {
			v1api.PayloadTooLarge(c, map[string]any{"error": err.Error()})
			return nil, false
		}
		v1api.BadRequest(c, v1api.MalformedJSONMessage, nil, false)
		return nil, false
	}
	return body, true
}

// rawWorkspaceID is forma2's `body.workspaceId ?? body.environmentId`, the legacy alias read only when workspaceId is absent or null. It answers 400 itself when the result is falsy or not a string.
func rawWorkspaceID(c *gin.Context, body map[string]any) (string, bool) {
	raw, present := body["workspaceId"]
	if !present || raw == nil {
		raw = body["environmentId"]
	}
	if !v1api.Truthy(raw) {
		v1api.BadRequest(c, "workspaceId must be provided", nil, false)
		return "", false
	}
	s, ok := raw.(string)
	if !ok {
		v1api.BadRequest(c, "workspaceId must be a string", nil, false)
		return "", false
	}
	return s, true
}

// ResolveWorkspace is forma2's resolveBodyIds: the workspace the body names by workspaceId or its legacy environment id (404 Workspace naming the id sent), authorized for the method's action (401 when refused). On success the body's workspaceId is the canonical id.
func (w *Wrapper) ResolveWorkspace(c *gin.Context, body map[string]any, method string) (*workspace.ClientWorkspace, bool) {
	raw, ok := rawWorkspaceID(c, body)
	if !ok {
		return nil, false
	}
	ws, err := w.workspaces.ResolveClientID(c.Request.Context(), raw)
	if err != nil {
		w.Unexpected(c, err, MessageUnexpected)
		return nil, false
	}
	if ws == nil {
		v1api.NotFound(c, "Workspace", raw, false)
		return nil, false
	}
	if !w.Authorize(c, method, ws.ID) {
		return nil, false
	}
	body["workspaceId"] = ws.ID.String()
	return ws, true
}
