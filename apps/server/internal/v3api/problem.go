// Package v3api holds the conventions every /api/v3 endpoint follows, forma2's withV3ApiWrapper (apps/web/app/api/v3/lib): RFC 9457 problem responses, authentication by session or API key, the per-caller rate limit, strict input parsing that reports invalid_params, cursor pagination over bounded collections, workspace access and audit events.
package v3api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpserver"
)

const (
	contentTypeProblem = "application/problem+json"
	// CacheNoStore is the Cache-Control of every v3 response.
	CacheNoStore = "private, no-store"
	// ForbiddenDetail is the detail of every refused access, a missing resource included, so ids cannot be probed.
	ForbiddenDetail = "You are not authorized to access this resource"
	// InternalDetail is the detail of an unexpected failure; the cause is only logged.
	InternalDetail = "An unexpected error occurred."
)

// InvalidParam is one field-level failure of a request, forma2's InvalidParam.
type InvalidParam struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// Problem is an expected v3 failure, rendered as RFC 9457 Problem Details with forma2's extension members.
type Problem struct {
	Status        int
	Title         string
	Detail        string
	Code          string
	InvalidParams []InvalidParam
	// RetryAfter is in seconds; zero leaves the header unset.
	RetryAfter int
	// cause is the unexpected failure behind a 500 with an operation-specific detail; ErrorHandler logs it.
	cause error
}

func (p *Problem) Error() string {
	return p.Code + ": " + p.Detail
}

// BadRequest is forma2's problemBadRequest (400).
func BadRequest(detail string, params ...InvalidParam) *Problem {
	return &Problem{Status: http.StatusBadRequest, Title: "Bad Request", Detail: detail, Code: "bad_request", InvalidParams: params}
}

// Unauthorized is forma2's problemUnauthorized (401).
func Unauthorized(detail string) *Problem {
	return &Problem{Status: http.StatusUnauthorized, Title: "Unauthorized", Detail: detail, Code: "not_authenticated"}
}

// Forbidden is forma2's problemForbidden (403).
func Forbidden(detail string) *Problem {
	return &Problem{Status: http.StatusForbidden, Title: "Forbidden", Detail: detail, Code: "forbidden"}
}

// Unprocessable is forma2's problemUnprocessableContent (422).
func Unprocessable(detail string, params ...InvalidParam) *Problem {
	return &Problem{Status: http.StatusUnprocessableEntity, Title: "Unprocessable Content", Detail: detail, Code: "unprocessable_content", InvalidParams: params}
}

// Internal is forma2's problemInternalError (500) with a detail of the operation's choosing.
func Internal(detail string) *Problem {
	return &Problem{Status: http.StatusInternalServerError, Title: "Internal Server Error", Detail: detail, Code: "internal_server_error"}
}

// Unexpected is a 500 whose detail names the operation that failed (forma2's "Unable to update tag"); the cause is logged, never sent.
func Unexpected(detail string, cause error) *Problem {
	p := Internal(detail)
	p.cause = cause
	return p
}

// fromAppErr renders an expected failure of a shared component (the session lookup, the rate limiter) in the v3 vocabulary.
func fromAppErr(e *apperr.Error) *Problem {
	switch e.Status {
	case http.StatusBadRequest:
		return BadRequest(e.Message)
	case http.StatusUnauthorized:
		return Unauthorized(e.Message)
	case http.StatusForbidden:
		return Forbidden(e.Message)
	case http.StatusNotFound:
		return &Problem{Status: http.StatusNotFound, Title: "Not Found", Detail: e.Message, Code: "not_found"}
	case http.StatusConflict:
		return &Problem{Status: http.StatusConflict, Title: "Conflict", Detail: e.Message, Code: "conflict"}
	case http.StatusRequestEntityTooLarge:
		return &Problem{Status: http.StatusRequestEntityTooLarge, Title: "Payload Too Large", Detail: e.Message, Code: "payload_too_large"}
	case http.StatusUnprocessableEntity:
		return Unprocessable(e.Message)
	case http.StatusTooManyRequests:
		return &Problem{Status: http.StatusTooManyRequests, Title: "Too Many Requests", Detail: e.Message, Code: "too_many_requests", RetryAfter: e.RetryAfter}
	}
	return nil
}

// Fail records err as the request's outcome and stops the handler chain; ErrorHandler writes the response.
func Fail(c *gin.Context, err error) {
	_ = c.Error(err)
	c.Abort()
}

// ErrorHandler renders the error a handler passed to Fail: a *Problem as it is, an *apperr.Error in its v3 form, and anything else, logged once here, as forma2's generic 500.
func ErrorHandler(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		if len(c.Errors) == 0 || c.Writer.Written() {
			return
		}
		err := c.Errors.Last().Err
		var p *Problem
		var appErr *apperr.Error
		switch {
		case errors.As(err, &p):
			if p.cause != nil {
				log.ErrorContext(c.Request.Context(), "V3 API operation failed", "request_id", RequestID(c), "route", c.FullPath(), "err", p.cause)
			}
		case errors.As(err, &appErr):
			p = fromAppErr(appErr)
		}
		if p == nil {
			log.ErrorContext(c.Request.Context(), "V3 API unexpected error", "request_id", RequestID(c), "route", c.FullPath(), "err", err)
			p = Internal(InternalDetail)
		}
		write(c, p)
	}
}

type problemBody struct {
	Title         string         `json:"title"`
	Status        int            `json:"status"`
	Detail        string         `json:"detail"`
	Instance      string         `json:"instance,omitempty"`
	Code          string         `json:"code,omitempty"`
	RequestID     string         `json:"requestId"`
	InvalidParams []InvalidParam `json:"invalid_params,omitempty"`
}

func write(c *gin.Context, p *Problem) {
	body := problemBody{Title: p.Title, Status: p.Status, Detail: p.Detail, Code: p.Code, RequestID: RequestID(c), InvalidParams: p.InvalidParams}
	// forma2 left the instance off rate-limit rejections only.
	if p.Status != http.StatusTooManyRequests {
		body.Instance = c.Request.URL.Path
	}
	raw, err := json.Marshal(body)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	if p.RetryAfter > 0 {
		c.Header("Retry-After", strconv.Itoa(p.RetryAfter))
	}
	c.Header("Cache-Control", CacheNoStore)
	c.Data(p.Status, contentTypeProblem, raw)
}

const requestIDKey = "X-Request-Id"

// RequestID is the request's correlation id: the one the server's middleware assigned, or one minted here and echoed back when the route runs without it.
func RequestID(c *gin.Context) string {
	if id := httpserver.RequestID(c); id != "" {
		return id
	}
	var b [16]byte
	_, _ = rand.Read(b[:])
	id := hex.EncodeToString(b[:])
	c.Set(requestIDKey, id)
	c.Header(requestIDKey, id)
	return id
}

// OK answers {data} with forma2's cache header.
func OK(c *gin.Context, data any) {
	c.Header("Cache-Control", CacheNoStore)
	c.JSON(http.StatusOK, gin.H{"data": data})
}

// ListMeta is the meta of a cursor-paginated list.
type ListMeta struct {
	Limit      int     `json:"limit"`
	NextCursor *string `json:"nextCursor"`
}

// List answers {data, meta} with forma2's cache header.
func List(c *gin.Context, data any, meta ListMeta) {
	c.Header("Cache-Control", CacheNoStore)
	c.JSON(http.StatusOK, gin.H{"data": data, "meta": meta})
}
