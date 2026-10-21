// Package apperr defines the expected failures services return, mirroring forma2's error classes (packages/types/errors.ts). Each carries the HTTP status and the stable code the /api/app error envelope reports; anything that is not an *Error is an unexpected failure and reaches the client as a generic 500.
package apperr

import (
	"errors"
	"fmt"
	"net/http"
)

// Generic codes. A service may use its own stable code (for example "signup_disabled" or "password_compromised") with InvalidInput so the client can map it to a localized message.
const (
	CodeBadRequest          = "bad_request"
	CodeValidation          = "validation_error"
	CodeNotAuthenticated    = "not_authenticated"
	CodeForbidden           = "forbidden"
	CodeOperationNotAllowed = "operation_not_allowed"
	CodeNotFound            = "not_found"
	CodeConflict            = "conflict"
	CodeRequestTooLarge     = "request_body_too_large"
	CodeTooManyRequests     = "too_many_requests"
	CodeInternal            = "internal_server_error"
)

// TooManyRequestsMessage is forma2's wording for every rate-limit rejection.
const TooManyRequestsMessage = "Maximum number of requests reached. Please try again later."

// InternalMessage is what an unexpected failure says to the client; the cause is only logged.
const InternalMessage = "Something went wrong while executing the operation."

// Error is an expected failure: the request was understood and refused for a reason the client may show.
type Error struct {
	Status  int
	Code    string
	Message string
	// Details carries structured data for the client, such as per-field validation errors.
	Details map[string]any
	// RetryAfter is the number of seconds until a rate-limited request may succeed; zero when not rate limited.
	RetryAfter int
}

func (e *Error) Error() string {
	return e.Code + ": " + e.Message
}

// As returns the *Error in err's chain, if any.
func As(err error) (*Error, bool) {
	var e *Error
	ok := errors.As(err, &e)
	return e, ok
}

// FieldError is one failed constraint on one input field. Field is the JSON name; Rule is the constraint that failed (for example "required", "email", "max").
type FieldError struct {
	Field string `json:"field"`
	Rule  string `json:"rule"`
	Param string `json:"param,omitempty"`
}

// InvalidInput is forma2's InvalidInputError: 400 with a stable code the client can key off.
func InvalidInput(code, message string) *Error {
	return &Error{Status: http.StatusBadRequest, Code: code, Message: message}
}

// BadRequest is a malformed request the client should not have sent.
func BadRequest(message string) *Error {
	return InvalidInput(CodeBadRequest, message)
}

// Validation reports which fields failed which constraints.
func Validation(fields []FieldError) *Error {
	return &Error{Status: http.StatusBadRequest, Code: CodeValidation, Message: "Invalid input", Details: map[string]any{"fields": fields}}
}

// NotAuthenticated is forma2's AuthenticationError (401).
func NotAuthenticated(message string) *Error {
	return &Error{Status: http.StatusUnauthorized, Code: CodeNotAuthenticated, Message: message}
}

// Forbidden is forma2's AuthorizationError (403).
func Forbidden(message string) *Error {
	return &Error{Status: http.StatusForbidden, Code: CodeForbidden, Message: message}
}

// OperationNotAllowed is forma2's OperationNotAllowedError (403): the caller may act, but not in this state.
func OperationNotAllowed(message string) *Error {
	return &Error{Status: http.StatusForbidden, Code: CodeOperationNotAllowed, Message: message}
}

// NotFound is forma2's ResourceNotFoundError, with its message format.
func NotFound(resource, id string) *Error {
	msg := resource + " not found"
	if id != "" {
		msg = fmt.Sprintf("%s with ID %s not found", resource, id)
	}
	return &Error{Status: http.StatusNotFound, Code: CodeNotFound, Message: msg}
}

// Conflict is forma2's UniqueConstraintError (409).
func Conflict(message string) *Error {
	return &Error{Status: http.StatusConflict, Code: CodeConflict, Message: message}
}

// RequestTooLarge is a body over the endpoint's limit.
func RequestTooLarge(limitBytes int64) *Error {
	return &Error{Status: http.StatusRequestEntityTooLarge, Code: CodeRequestTooLarge, Message: fmt.Sprintf("Request body exceeds %d bytes", limitBytes)}
}

// TooManyRequests is forma2's TooManyRequestsError (429). retryAfter is in seconds; zero leaves it unset.
func TooManyRequests(retryAfter int) *Error {
	e := &Error{Status: http.StatusTooManyRequests, Code: CodeTooManyRequests, Message: TooManyRequestsMessage, RetryAfter: retryAfter}
	if retryAfter > 0 {
		e.Details = map[string]any{"retryAfter": retryAfter}
	}
	return e
}
