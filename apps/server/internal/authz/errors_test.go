package authz

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestMapErrorClassifiesGRPCStatuses(t *testing.T) {
	cases := []struct {
		code      codes.Code
		want      Code
		retryable bool
	}{
		{codes.DeadlineExceeded, CodeTimeout, true},
		{codes.Unavailable, CodeUnavailable, true},
		{codes.ResourceExhausted, CodeOverloaded, true},
		{codes.Aborted, CodeAborted, true},
		{codes.Unauthenticated, CodeUnauthenticated, false},
		{codes.PermissionDenied, CodePermissionDenied, false},
		{codes.InvalidArgument, CodeInvalidRequest, false},
		{codes.OutOfRange, CodeInvalidRequest, false},
		{codes.FailedPrecondition, CodeFailedPrecondition, false},
		{codes.NotFound, CodeNotFound, false},
		{codes.AlreadyExists, CodeConflict, false},
		{codes.Canceled, CodeCancelled, false},
		{codes.Unimplemented, CodeUnsupported, false},
		{codes.Internal, CodeInternal, false},
		{codes.DataLoss, CodeInternal, false},
		{codes.Unknown, CodeInternal, false},
	}
	for _, tc := range cases {
		t.Run(tc.code.String(), func(t *testing.T) {
			e := mapError(status.Error(tc.code, "tenant 42 secret detail"), "check_permission", 2)
			if e.Code != tc.want || e.Retryable != tc.retryable || e.Attempts != 2 || e.Operation != "check_permission" || e.GRPCStatus != tc.code.String() {
				t.Fatalf("mapError(%v) = %+v, want %s retryable=%v", tc.code, e, tc.want, tc.retryable)
			}
			// The message is the code alone: SpiceDB's own text can carry identifiers.
			if e.Error() != string(tc.want) {
				t.Fatalf("Error() = %q, want %q", e.Error(), tc.want)
			}
		})
	}
}

func TestMapErrorKeepsTypedErrorsAndContextErrors(t *testing.T) {
	typed := newError(CodeOverloaded, "lookup_resources", true, 1)
	if e := mapError(fmt.Errorf("wrapped: %w", typed), "authorization", 3); e.Code != CodeOverloaded || !e.Retryable || e.Operation != "authorization" || e.Attempts != 3 {
		t.Fatalf("typed = %+v", e)
	}
	if e := mapError(context.DeadlineExceeded, "op", 1); e.Code != CodeTimeout || !e.Retryable {
		t.Fatalf("deadline = %+v", e)
	}
	if e := mapError(context.Canceled, "op", 1); e.Code != CodeCancelled || e.Retryable {
		t.Fatalf("canceled = %+v", e)
	}
	if e := mapError(errors.New("boom"), "op", 1); e.Code != CodeInternal || e.Retryable {
		t.Fatalf("plain = %+v", e)
	}
}

func TestNormalizeTurnsUntypedFailuresIntoInternalButKeepsTheCause(t *testing.T) {
	cause := errors.New("connection refused")
	e := normalize(cause, "authorization")
	if e.Code != CodeInternal || e.Retryable || e.Attempts != 1 || !errors.Is(e, cause) {
		t.Fatalf("normalize = %+v", e)
	}
	stale := newError(CodeProjectionStale, "authorization_projection_freshness", false, 0)
	if e := normalize(stale, "authorization"); e.Code != CodeProjectionStale || e.Operation != "authorization" || e.Attempts != 0 {
		t.Fatalf("normalize typed = %+v", e)
	}
}
