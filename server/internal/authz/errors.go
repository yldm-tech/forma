package authz

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Code is a stable, enumerable failure class. Codes are what reaches logs and callers; identifiers and SpiceDB's own messages never do.
type Code string

const (
	CodeAborted                  Code = "authzed_aborted"
	CodeCancelled                Code = "authzed_cancelled"
	CodeConflict                 Code = "authzed_conflict"
	CodeDisabled                 Code = "authzed_disabled"
	CodeFailedPrecondition       Code = "authzed_failed_precondition"
	CodeInternal                 Code = "authzed_internal"
	CodeInvalidRequest           Code = "authzed_invalid_request"
	CodeLimitExceeded            Code = "authzed_limit_exceeded"
	CodeNotFound                 Code = "authzed_not_found"
	CodeOverloaded               Code = "authzed_overloaded"
	CodePermissionDenied         Code = "authzed_permission_denied"
	CodeProjectionStale          Code = "authzed_projection_stale"
	CodeSchemaChanged            Code = "authzed_schema_changed"
	CodeSchemaVerificationFailed Code = "authzed_schema_verification_failed"
	CodeTimeout                  Code = "authzed_timeout"
	CodeUnauthenticated          Code = "authzed_unauthenticated"
	CodeUnavailable              Code = "authzed_unavailable"
	CodeUnsupported              Code = "authzed_unsupported"

	// Projection outcomes the delivery job records on outbox rows.
	codeProjectionInvalidSource  Code = "authzed_projection_invalid_source"
	codeProjectionUnstable       Code = "authzed_projection_unstable"
	codeProjectionInvalidEvent   Code = "authzed_projection_invalid_event"
	codeProjectionDeliveryFailed Code = "authzed_projection_delivery_failed"
)

// Error is an authorization operational failure: no decision could be made. It always fails closed, as an error rather than a denial.
type Error struct {
	Code Code
	// Operation names the step that failed (check_permission, authorization, ...).
	Operation string
	// Retryable says the failure is about SpiceDB's availability rather than this request or this deployment's configuration.
	Retryable bool
	// Attempts is how many calls stood behind the failure; 0 when it was refused before any call.
	Attempts int
	// GRPCStatus is the gRPC status name when the failure came from SpiceDB.
	GRPCStatus string
	cause      error
}

// Error is the code alone: SpiceDB messages can carry identifiers and must not reach logs through err.Error().
func (e *Error) Error() string { return string(e.Code) }

func (e *Error) Unwrap() error { return e.cause }

func newError(code Code, operation string, retryable bool, attempts int) *Error {
	return &Error{Code: code, Operation: operation, Retryable: retryable, Attempts: attempts}
}

// AsError returns the *Error in err's chain, if any.
func AsError(err error) (*Error, bool) {
	var e *Error
	ok := errors.As(err, &e)
	return e, ok
}

type grpcDescriptor struct {
	code      Code
	retryable bool
}

var grpcDescriptors = map[codes.Code]grpcDescriptor{
	codes.DeadlineExceeded:   {CodeTimeout, true},
	codes.Unavailable:        {CodeUnavailable, true},
	codes.ResourceExhausted:  {CodeOverloaded, true},
	codes.Aborted:            {CodeAborted, true},
	codes.Unauthenticated:    {CodeUnauthenticated, false},
	codes.PermissionDenied:   {CodePermissionDenied, false},
	codes.InvalidArgument:    {CodeInvalidRequest, false},
	codes.OutOfRange:         {CodeInvalidRequest, false},
	codes.FailedPrecondition: {CodeFailedPrecondition, false},
	codes.NotFound:           {CodeNotFound, false},
	codes.AlreadyExists:      {CodeConflict, false},
	codes.Canceled:           {CodeCancelled, false},
	codes.Unimplemented:      {CodeUnsupported, false},
}

// mapError classifies any failure as an *Error for operation. An *Error keeps its code and retryability; a gRPC status maps through forma2's table; context errors map like their gRPC counterparts; anything else is authzed_internal.
func mapError(err error, operation string, attempts int) *Error {
	if e, ok := AsError(err); ok {
		return &Error{Code: e.Code, Operation: operation, Retryable: e.Retryable, Attempts: attempts, GRPCStatus: e.GRPCStatus, cause: e.cause}
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return &Error{Code: CodeTimeout, Operation: operation, Retryable: true, Attempts: attempts, cause: err}
	case errors.Is(err, context.Canceled):
		return &Error{Code: CodeCancelled, Operation: operation, Attempts: attempts, cause: err}
	}
	if s, ok := status.FromError(err); ok && s.Code() != codes.OK {
		d, known := grpcDescriptors[s.Code()]
		if !known {
			d = grpcDescriptor{CodeInternal, false}
		}
		return &Error{Code: d.code, Operation: operation, Retryable: d.retryable, Attempts: attempts, GRPCStatus: s.Code().String(), cause: err}
	}
	return &Error{Code: CodeInternal, Operation: operation, Attempts: attempts, cause: err}
}

// normalize re-labels a failure with the operation the caller performed. A non-*Error (a database error, an invalid action) becomes authzed_internal, as forma2's normalizeAuthorizationOperationalError does; its cause stays reachable through errors.Unwrap.
func normalize(err error, operation string) *Error {
	if e, ok := AsError(err); ok {
		return &Error{Code: e.Code, Operation: operation, Retryable: e.Retryable, Attempts: e.Attempts, GRPCStatus: e.GRPCStatus, cause: e.cause}
	}
	return &Error{Code: CodeInternal, Operation: operation, Attempts: 1, cause: err}
}
