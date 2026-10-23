// Package httpapi holds the conventions every /api/app endpoint follows: the JSON error envelope, request binding and validation, the request's actor and client address, CSRF protection, and the session middleware contract.
package httpapi

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/platform/httpserver"
)

// Envelope is the body of every /api/app error response.
type Envelope struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

// Fail records err as the request's outcome and stops the handler chain; ErrorHandler writes the response.
func Fail(c *gin.Context, err error) {
	_ = c.Error(err)
	c.Abort()
}

// ErrorHandler renders the error a handler passed to Fail. Expected failures (*apperr.Error) keep their status, code and message; anything else is logged once, here, and answered with a generic 500 so internals never reach the client.
func ErrorHandler(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		if len(c.Errors) == 0 || c.Writer.Written() {
			return
		}
		err := c.Errors.Last().Err
		e, ok := apperr.As(err)
		if !ok {
			log.ErrorContext(c.Request.Context(), "request failed",
				"request_id", httpserver.RequestID(c),
				"route", c.FullPath(),
				"err", err,
			)
			e = &apperr.Error{Status: http.StatusInternalServerError, Code: apperr.CodeInternal, Message: apperr.InternalMessage}
		}
		write(c, e)
	}
}

// Abort writes e immediately. Middleware uses it; handlers use Fail.
func Abort(c *gin.Context, e *apperr.Error) {
	write(c, e)
	c.Abort()
}

func write(c *gin.Context, e *apperr.Error) {
	if e.RetryAfter > 0 {
		c.Header("Retry-After", strconv.Itoa(e.RetryAfter))
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(e.Status, Envelope{Code: e.Code, Message: e.Message, Details: e.Details})
}
