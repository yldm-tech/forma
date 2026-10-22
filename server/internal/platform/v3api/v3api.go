// Package v3api holds the conventions every /api/v3 endpoint follows, forma2's withV3ApiWrapper: RFC 9457 problem responses (application/problem+json with a request id and the offending invalid_params), authentication by session or API key, the api:v3 rate limit, and the success envelopes.
package v3api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/apikey"
	"github.com/yldm-tech/forma/server/internal/authz"
	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/server/internal/platform/httpserver"
	"github.com/yldm-tech/forma/server/internal/platform/ratelimit"
)

// CacheNoStore is the Cache-Control of every v3 response.
const CacheNoStore = "private, no-store"

// Invalid param codes, forma2's INVALID_PARAM_CODES.
const (
	CodeDanglingReference    = "dangling_reference"
	CodeDuplicateIdentifier  = "duplicate_identifier"
	CodeDuplicateLocale      = "duplicate_locale"
	CodeForbiddenIdentifier  = "forbidden_identifier"
	CodeImmutableIdentifier  = "immutable_identifier"
	CodeInvalidLocale        = "invalid_locale"
	CodeInvalidReference     = "invalid_reference"
	CodeMissingRequiredField = "missing_required_field"
	CodeMissingTranslation   = "missing_translation"
	CodeUnsupportedField     = "unsupported_field"
	CodeUnsupportedLocale    = "unsupported_locale"
)

// InvalidParam is one offending input of a problem.
type InvalidParam struct {
	Name          string `json:"name"`
	Reason        string `json:"reason"`
	Code          string `json:"code,omitempty"`
	Identifier    string `json:"identifier,omitempty"`
	ReferenceType string `json:"referenceType,omitempty"`
	MissingID     string `json:"missingId,omitempty"`
	FirstUsedAt   string `json:"firstUsedAt,omitempty"`
	ConflictsWith string `json:"conflictsWith,omitempty"`
}

// Problem is an RFC 9457 problem a v3 endpoint answers with.
type Problem struct {
	Status        int
	Title         string
	Detail        string
	Code          string
	InvalidParams []InvalidParam
	Details       map[string]any
	RetryAfter    int
}

func (p *Problem) Error() string { return p.Code + ": " + p.Detail }

// BadRequest is 400 bad_request.
func BadRequest(detail string, params ...InvalidParam) *Problem {
	return &Problem{Status: http.StatusBadRequest, Title: "Bad Request", Detail: detail, Code: "bad_request", InvalidParams: params}
}

// Unauthorized is 401 not_authenticated.
func Unauthorized(detail string) *Problem {
	return &Problem{Status: http.StatusUnauthorized, Title: "Unauthorized", Detail: detail, Code: "not_authenticated"}
}

// ForbiddenMessage is the detail of every v3 403 that hides whether a resource exists.
const ForbiddenMessage = "You are not authorized to access this resource"

// Forbidden is 403 forbidden.
func Forbidden(detail string) *Problem {
	if detail == "" {
		detail = ForbiddenMessage
	}
	return &Problem{Status: http.StatusForbidden, Title: "Forbidden", Detail: detail, Code: "forbidden"}
}

// Unprocessable is 422 unprocessable_content.
func Unprocessable(detail string, params ...InvalidParam) *Problem {
	return &Problem{Status: http.StatusUnprocessableEntity, Title: "Unprocessable Content", Detail: detail, Code: "unprocessable_content", InvalidParams: params}
}

// PayloadTooLarge is 413 payload_too_large.
func PayloadTooLarge(detail string) *Problem {
	return &Problem{Status: http.StatusRequestEntityTooLarge, Title: "Payload Too Large", Detail: detail, Code: "payload_too_large"}
}

// TooManyRequests is 429 too_many_requests.
func TooManyRequests(detail string, retryAfter int) *Problem {
	return &Problem{Status: http.StatusTooManyRequests, Title: "Too Many Requests", Detail: detail, Code: "too_many_requests", RetryAfter: retryAfter}
}

// Internal is 500 internal_server_error.
func Internal() *Problem {
	return &Problem{Status: http.StatusInternalServerError, Title: "Internal Server Error", Detail: "An unexpected error occurred.", Code: "internal_server_error"}
}

type problemBody struct {
	Title         string         `json:"title"`
	Status        int            `json:"status"`
	Detail        string         `json:"detail"`
	RequestID     string         `json:"requestId"`
	Instance      string         `json:"instance,omitempty"`
	Code          string         `json:"code,omitempty"`
	Details       map[string]any `json:"details,omitempty"`
	InvalidParams []InvalidParam `json:"invalid_params,omitempty"`
}

// WriteProblem answers with the problem and stops the chain.
func WriteProblem(c *gin.Context, p *Problem) {
	c.Header("Cache-Control", CacheNoStore)
	if p.RetryAfter > 0 {
		c.Header("Retry-After", strconv.Itoa(p.RetryAfter))
	}
	body, _ := json.Marshal(problemBody{
		Title: p.Title, Status: p.Status, Detail: p.Detail, RequestID: httpserver.RequestID(c), Instance: c.Request.URL.Path,
		Code: p.Code, Details: p.Details, InvalidParams: p.InvalidParams,
	})
	c.Data(p.Status, "application/problem+json", body)
	c.Abort()
}

// Fail answers with the problem an error stands for: a *Problem as it is, anything else is logged once and answered 500.
func Fail(c *gin.Context, log *slog.Logger, err error) {
	var p *Problem
	if errors.As(err, &p) {
		WriteProblem(c, p)
		return
	}
	log.ErrorContext(c.Request.Context(), "V3 API unexpected error", "request_id", httpserver.RequestID(c), "route", c.FullPath(), "err", err)
	WriteProblem(c, Internal())
}

// OK answers {data} with 200 and the v3 cache header.
func OK(c *gin.Context, data any) {
	c.Header("Cache-Control", CacheNoStore)
	c.JSON(http.StatusOK, gin.H{"data": data})
}

// List answers {data, meta} with 200.
func List(c *gin.Context, data, meta any) {
	c.Header("Cache-Control", CacheNoStore)
	c.JSON(http.StatusOK, gin.H{"data": data, "meta": meta})
}

// Created answers {data} with 201 and its Location.
func Created(c *gin.Context, location string, data any) {
	c.Header("Cache-Control", CacheNoStore)
	c.Header("Location", location)
	c.JSON(http.StatusCreated, gin.H{"data": data})
}

// NoContent answers 204.
func NoContent(c *gin.Context) {
	c.Header("Cache-Control", CacheNoStore)
	c.Status(http.StatusNoContent)
	c.Abort()
}

// AuthMode is which credentials a route accepts.
type AuthMode int

const (
	// SessionOrAPIKey is forma2's "both": an x-api-key (or current-format Bearer key) wins over the session.
	SessionOrAPIKey AuthMode = iota
	// SessionOnly accepts only a signed-in user.
	SessionOnly
)

type actorKey struct{}

// ActorFrom is the authenticated caller of a v3 request.
func ActorFrom(ctx context.Context) (authz.Actor, bool) {
	a, ok := ctx.Value(actorKey{}).(authz.Actor)
	return a, ok
}

// Guard authenticates v3 requests and applies the api:v3 rate limit.
type Guard struct {
	sessions httpapi.SessionAuth
	keys     *apikey.Authenticator
	limiter  *ratelimit.Limiter
	csrf     func(*http.Request) *apperr.Error
	log      *slog.Logger
}

func NewGuard(cfg config.Config, sessions httpapi.SessionAuth, keys *apikey.Authenticator, limiter *ratelimit.Limiter, log *slog.Logger) *Guard {
	return &Guard{sessions: sessions, keys: keys, limiter: limiter, csrf: httpapi.CSRFCheck(cfg), log: log}
}

// authenticate resolves the caller, answering 401 when there is none, then charges the rule's limit to the user or key. It must follow the session middleware's Optional handler, which Handlers installs.
//
// forma2's route handlers trusted the session cookie's SameSite attribute alone; a session-authenticated mutation here must also pass the /api/app CSRF rule (an allowed Origin and the X-Requested-With header the SPA always sends), as on the other v3 routes. API keys are not ambient credentials and are exempt.
func (g *Guard) authenticate(mode AuthMode, rule ratelimit.Rule) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		var actor *authz.Actor
		if mode == SessionOrAPIKey && apikey.KeyFromHeaders(c.Request.Header) != "" {
			p, err := g.keys.Authenticate(ctx, c.Request.Header, apikey.Options{})
			if err != nil {
				Fail(c, g.log, err)
				return
			}
			if p != nil {
				a := p.Actor()
				actor = &a
			}
		} else if a, ok := httpapi.ActorFrom(ctx); ok {
			u := authz.User(a.UserID)
			actor = &u
		}
		if actor == nil {
			detail := "Not authenticated"
			if mode == SessionOnly {
				detail = "Session required"
			}
			WriteProblem(c, Unauthorized(detail))
			return
		}
		if actor.Type != authz.ActorAPIKey {
			if e := g.csrf(c.Request); e != nil {
				WriteProblem(c, Forbidden(e.Message))
				return
			}
		}
		if err := g.limiter.Apply(ctx, rule, actor.ID.String()); err != nil {
			if e, ok := apperr.As(err); ok && e.Status == http.StatusTooManyRequests {
				WriteProblem(c, TooManyRequests(e.Message, e.RetryAfter))
				return
			}
			Fail(c, g.log, err)
			return
		}
		c.Request = c.Request.WithContext(context.WithValue(ctx, actorKey{}, *actor))
		c.Next()
	}
}

// Handlers is the middleware chain of a v3 route group: the renderer of the errors the session middleware records, the optional session, the authorization decision memo, then authentication, the CSRF rule and the api:v3 rate limit.
func (g *Guard) Handlers(mode AuthMode) []gin.HandlerFunc {
	return g.LimitedHandlers(mode, ratelimit.APIV3)
}

// LimitedHandlers is Handlers charging rule in place of api:v3, forma2's customRateLimitConfig.
func (g *Guard) LimitedHandlers(mode AuthMode, rule ratelimit.Rule) []gin.HandlerFunc {
	return []gin.HandlerFunc{g.renderErrors(), g.sessions.Optional(), authz.DecisionMemo(), g.authenticate(mode, rule)}
}

// renderErrors answers with a problem when a middleware recorded an error with httpapi.Fail and wrote nothing, as the session middleware does when the session cannot be read: an *apperr.Error with its status and message, anything else as a logged 500.
func (g *Guard) renderErrors() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		if len(c.Errors) == 0 || c.Writer.Written() {
			return
		}
		err := c.Errors.Last().Err
		if e, ok := apperr.As(err); ok && e.Status < http.StatusInternalServerError {
			WriteProblem(c, &Problem{Status: e.Status, Title: http.StatusText(e.Status), Detail: e.Message, Code: e.Code, RetryAfter: e.RetryAfter})
			return
		}
		Fail(c, g.log, err)
	}
}

// RequireWorkspace is forma2's requireV3WorkspaceAccess: the caller must hold the action on the workspace; a missing workspace and a denial are the same 403, so existence does not leak. An authorization failure (SpiceDB unreachable) is returned as is.
func RequireWorkspace(ctx context.Context, authorizer *authz.Authorizer, actor authz.Actor, action authz.Action, workspaceID uuid.UUID) error {
	allowed, err := authorizer.Can(ctx, actor, action, authz.Workspace(workspaceID))
	if err != nil {
		return err
	}
	if !allowed {
		return Forbidden("")
	}
	return nil
}
