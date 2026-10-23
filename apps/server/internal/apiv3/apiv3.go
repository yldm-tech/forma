// Package apiv3 holds the conventions of the v3 management API (forma2's apps/web/app/api/v3/lib): RFC 9457 problem responses, authentication by session or API key, the per-principal rate limit, workspace access that answers 403 for a workspace the caller may not see whether or not it exists, and cursor pagination of workspace reference lists. The contract is docs/api-v3-reference.
package apiv3

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/apikey"
	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpserver"
	"github.com/yldm-tech/forma/apps/server/internal/platform/ratelimit"
)

const (
	problemJSON = "application/problem+json"
	noStore     = "private, no-store"
	// ForbiddenDetail is the detail of every 403: a workspace that does not exist and one the caller may not see are refused alike.
	ForbiddenDetail = "You are not authorized to access this resource"
	internalDetail  = "An unexpected error occurred."
)

// InvalidParam is one field-level failure.
type InvalidParam struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// Problem is the RFC 9457 body of every v3 failure.
type Problem struct {
	Type          string         `json:"type,omitempty"`
	Title         string         `json:"title"`
	Status        int            `json:"status"`
	Detail        string         `json:"detail"`
	Instance      string         `json:"instance,omitempty"`
	Code          string         `json:"code,omitempty"`
	RequestID     string         `json:"requestId"`
	InvalidParams []InvalidParam `json:"invalid_params,omitempty"`
}

// invalidParamsKey carries a BadRequest's invalid_params in an apperr.Error's Details.
const invalidParamsKey = "invalid_params"

// BadRequest is a 400 with field-level reasons.
func BadRequest(detail string, params ...InvalidParam) *apperr.Error {
	e := apperr.BadRequest(detail)
	if len(params) > 0 {
		e.Details = map[string]any{invalidParamsKey: params}
	}
	return e
}

// Forbidden is the v3 403.
func Forbidden() *apperr.Error {
	return apperr.Forbidden(ForbiddenDetail)
}

// problemCodes maps the /api/app error codes onto the v3 problem codes the spec allows.
func problemCode(e *apperr.Error) string {
	switch e.Status {
	case http.StatusBadRequest:
		return "bad_request"
	case http.StatusUnauthorized:
		return "not_authenticated"
	case http.StatusForbidden:
		return "forbidden"
	case http.StatusNotFound:
		return "not_found"
	case http.StatusConflict:
		return "conflict"
	case http.StatusRequestEntityTooLarge:
		return "payload_too_large"
	case http.StatusUnprocessableEntity:
		return "unprocessable_content"
	case http.StatusTooManyRequests:
		return "too_many_requests"
	}
	return "internal_server_error"
}

func writeProblem(c *gin.Context, e *apperr.Error) {
	requestID := httpserver.RequestID(c)
	p := Problem{Title: http.StatusText(e.Status), Status: e.Status, Detail: e.Message, Instance: c.Request.URL.Path, Code: problemCode(e), RequestID: requestID}
	if params, ok := e.Details[invalidParamsKey].([]InvalidParam); ok {
		p.InvalidParams = params
	}
	if e.RetryAfter > 0 {
		c.Header("Retry-After", strconv.Itoa(e.RetryAfter))
	}
	c.Header("Cache-Control", noStore)
	c.Header("X-Request-Id", requestID)
	body, err := json.Marshal(p)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	c.Data(e.Status, problemJSON, body)
}

// ErrorHandler renders the error a handler or middleware passed to httpapi.Fail as a problem. An unexpected error is logged once, here, and answered with a generic 500.
func ErrorHandler(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		if len(c.Errors) == 0 || c.Writer.Written() {
			return
		}
		err := c.Errors.Last().Err
		e, ok := apperr.As(err)
		if !ok {
			log.ErrorContext(c.Request.Context(), "V3 API unexpected error", "request_id", httpserver.RequestID(c), "route", c.FullPath(), "err", err)
			e = &apperr.Error{Status: http.StatusInternalServerError, Code: apperr.CodeInternal, Message: internalDetail}
		}
		writeProblem(c, e)
	}
}

// OK writes a success body with the v3 cache and correlation headers.
func OK(c *gin.Context, body any) {
	c.Header("Cache-Control", noStore)
	c.Header("X-Request-Id", httpserver.RequestID(c))
	c.JSON(http.StatusOK, body)
}

// Principal is who a v3 request acts as: a user with a session or an API key.
type Principal struct {
	UserID uuid.UUID
	APIKey *apikey.Principal
}

// Actor is the principal as an authorization subject.
func (p Principal) Actor() authz.Actor {
	if p.APIKey != nil {
		return p.APIKey.Actor()
	}
	return authz.User(p.UserID)
}

func (p Principal) id() string {
	if p.APIKey != nil {
		return p.APIKey.APIKeyID.String()
	}
	return p.UserID.String()
}

type principalKey struct{}

// PrincipalFrom returns the request's principal; every route behind Authenticate has one.
func PrincipalFrom(ctx context.Context) Principal {
	p, _ := ctx.Value(principalKey{}).(Principal)
	return p
}

// Authenticate is forma2's withV3ApiWrapper with auth "both", followed by its rate limit. A request that carries an API key is authenticated by the key alone; any other needs a session. Then forma2's api.v3 limit is charged to the user or the key.
func Authenticate(sessions httpapi.SessionAuth, keys *apikey.Authenticator, limiter *ratelimit.Limiter) []gin.HandlerFunc {
	optional := sessions.Optional()
	return []gin.HandlerFunc{
		func(c *gin.Context) {
			if apikey.KeyFromHeaders(c.Request.Header) != "" {
				c.Next()
				return
			}
			optional(c)
		},
		func(c *gin.Context) {
			var p Principal
			if apikey.KeyFromHeaders(c.Request.Header) != "" {
				key, err := keys.Authenticate(c.Request.Context(), c.Request.Header, apikey.Options{})
				if err != nil {
					httpapi.Fail(c, err)
					return
				}
				if key == nil {
					httpapi.Fail(c, apperr.NotAuthenticated("Not authenticated"))
					return
				}
				p.APIKey = key
			} else {
				actor, ok := httpapi.ActorFrom(c.Request.Context())
				if !ok {
					httpapi.Fail(c, apperr.NotAuthenticated("Not authenticated"))
					return
				}
				p.UserID = actor.UserID
			}
			if err := limiter.Apply(c.Request.Context(), ratelimit.APIV3, p.id()); err != nil {
				httpapi.Fail(c, err)
				return
			}
			c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), principalKey{}, p))
			c.Next()
		},
	}
}

// RequireWorkspace is forma2's requireV3WorkspaceAccess: the principal must hold action on the workspace. A missing workspace and a denial are both a 403, so the answer does not reveal which workspaces exist.
func RequireWorkspace(ctx context.Context, authorizer *authz.Authorizer, p Principal, action authz.Action, workspaceID uuid.UUID) error {
	allowed, err := authorizer.Can(ctx, p.Actor(), action, authz.Workspace(workspaceID))
	if err != nil {
		return err
	}
	if !allowed {
		return Forbidden()
	}
	return nil
}

// Page sizes of the workspace reference lists.
const (
	DefaultLimit = 50
	MaxLimit     = 250
)

// WorkspaceListQuery is forma2's ZV3WorkspaceListQuery: a workspace, a page size and an optional cursor, and nothing else.
type WorkspaceListQuery struct {
	WorkspaceID uuid.UUID
	Limit       int
	Cursor      string
}

// ParseWorkspaceListQuery validates the query string strictly, reporting every failing parameter. Workspace ids are uuids, the ids this server issues.
func ParseWorkspaceListQuery(c *gin.Context) (WorkspaceListQuery, error) {
	q := WorkspaceListQuery{Limit: DefaultLimit}
	values := c.Request.URL.Query()
	var params []InvalidParam
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	var unknown []string
	for _, k := range keys {
		if k != "workspaceId" && k != "limit" && k != "cursor" {
			unknown = append(unknown, fmt.Sprintf("%q", k))
			continue
		}
		if len(values[k]) > 1 {
			params = append(params, InvalidParam{Name: k, Reason: "Invalid input: expected string, received array"})
		}
	}
	single := func(k string) (string, bool) {
		v, ok := values[k]
		if !ok || len(v) != 1 {
			return "", false
		}
		return v[0], true
	}
	switch raw, ok := single("workspaceId"); {
	case !ok && len(values["workspaceId"]) == 0:
		params = append(params, InvalidParam{Name: "workspaceId", Reason: "Invalid input: expected string, received undefined"})
	case ok:
		id, err := uuid.Parse(raw)
		if err != nil || strings.ContainsAny(raw, "{}") || len(raw) != 36 {
			params = append(params, InvalidParam{Name: "workspaceId", Reason: "Invalid UUID"})
		}
		q.WorkspaceID = id
	}
	if raw, ok := single("limit"); ok {
		trimmed := strings.TrimSpace(raw)
		n, err := strconv.ParseFloat(trimmed, 64)
		if trimmed == "" {
			// Number("") is 0.
			n, err = 0, nil
		}
		switch {
		case err != nil:
			params = append(params, InvalidParam{Name: "limit", Reason: "Invalid input: expected number, received NaN"})
		case n != float64(int(n)):
			params = append(params, InvalidParam{Name: "limit", Reason: "Invalid input: expected int, received number"})
		case n < 1:
			params = append(params, InvalidParam{Name: "limit", Reason: "Too small: expected number to be >=1"})
		case n > MaxLimit:
			params = append(params, InvalidParam{Name: "limit", Reason: fmt.Sprintf("Too big: expected number to be <=%d", MaxLimit)})
		default:
			q.Limit = int(n)
		}
	}
	if raw, ok := single("cursor"); ok {
		if raw == "" {
			params = append(params, InvalidParam{Name: "cursor", Reason: "Too small: expected string to have >=1 characters"})
		}
		q.Cursor = raw
	}
	if len(unknown) > 0 {
		reason := "Unrecognized key: " + unknown[0]
		if len(unknown) > 1 {
			reason = "Unrecognized keys: " + strings.Join(unknown, ", ")
		}
		params = append(params, InvalidParam{Name: "query", Reason: reason})
	}
	if len(params) > 0 {
		return q, BadRequest("Invalid query parameters", params...)
	}
	return q, nil
}

// invalidCursor is forma2's rejection of a cursor it could not have issued.
func invalidCursor() error {
	const msg = "Invalid pagination cursor"
	return BadRequest(msg, InvalidParam{Name: "cursor", Reason: msg})
}

// Paginate is forma2's paginateByIdCursor over a fully read reference collection: items in id order, the page after the cursor's id, and the next cursor when more remain. Ids are uuids, ordered bytewise. A cursor that does not decode back to itself is refused.
func Paginate[T any](items []T, id func(T) string, limit int, cursor string) ([]T, *string, error) {
	sorted := slices.Clone(items)
	slices.SortFunc(sorted, func(a, b T) int { return strings.Compare(id(a), id(b)) })
	if cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(cursor)
		after := string(raw)
		if err != nil || after == "" || base64.RawURLEncoding.EncodeToString(raw) != cursor {
			return nil, nil, invalidCursor()
		}
		i, _ := slices.BinarySearchFunc(sorted, after, func(item T, target string) int { return strings.Compare(id(item), target) })
		for i < len(sorted) && id(sorted[i]) <= after {
			i++
		}
		sorted = sorted[i:]
	}
	if len(sorted) <= limit {
		return sorted, nil, nil
	}
	page := sorted[:limit]
	next := base64.RawURLEncoding.EncodeToString([]byte(id(page[len(page)-1])))
	return page, &next, nil
}

// ListMeta is the meta of a list response.
type ListMeta struct {
	Limit      int     `json:"limit"`
	NextCursor *string `json:"nextCursor"`
}
