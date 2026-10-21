package authz

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func discardLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

type recordedSleeps struct{ delays []time.Duration }

func testRunner(profile Profile, log *slog.Logger) (*runner, *recordedSleeps) {
	r := newRunner(profile, log)
	s := &recordedSleeps{}
	r.random = func() float64 { return 0.5 }
	r.sleep = func(_ context.Context, d time.Duration) error {
		s.delays = append(s.delays, d)
		return nil
	}
	return r, s
}

// failing returns a call that fails with the given gRPC codes in order, then succeeds.
func failing(calls *int, failures ...codes.Code) func(context.Context) error {
	return func(context.Context) error {
		*calls++
		if *calls <= len(failures) {
			return status.Error(failures[*calls-1], "detail with tenant ids")
		}
		return nil
	}
}

func TestRunnerReturnsAFirstAttemptSuccessWithoutSleeping(t *testing.T) {
	r, sleeps := testRunner(RequestProfile, discardLog())
	calls := 0
	if err := r.do(context.Background(), "op", failing(&calls)); err != nil || calls != 1 || len(sleeps.delays) != 0 {
		t.Fatalf("err %v, calls %d, sleeps %v", err, calls, sleeps.delays)
	}
}

func TestRunnerRetriesTransientFailuresWithTheScheduledDelays(t *testing.T) {
	r, sleeps := testRunner(RequestProfile, discardLog())
	calls := 0
	if err := r.do(context.Background(), "op", failing(&calls, codes.Unavailable, codes.DeadlineExceeded)); err != nil || calls != 3 {
		t.Fatalf("err %v, calls %d", err, calls)
	}
	if want := []time.Duration{100 * time.Millisecond, 200 * time.Millisecond}; len(sleeps.delays) != 2 || sleeps.delays[0] != want[0] || sleeps.delays[1] != want[1] {
		t.Fatalf("delays = %v, want %v", sleeps.delays, want)
	}
}

func TestRunnerStopsAfterThreeAttemptsWithTheFinalClassification(t *testing.T) {
	r, _ := testRunner(RequestProfile, discardLog())
	calls := 0
	err := r.do(context.Background(), "check_permission", failing(&calls, codes.Unavailable, codes.Unavailable, codes.Aborted))
	e, ok := AsError(err)
	if !ok || e.Code != CodeAborted || !e.Retryable || e.Attempts != 3 || calls != 3 {
		t.Fatalf("err %+v, calls %d", err, calls)
	}
}

func TestRunnerDoesNotRetryPermanentErrors(t *testing.T) {
	r, sleeps := testRunner(RequestProfile, discardLog())
	calls := 0
	err := r.do(context.Background(), "op", failing(&calls, codes.PermissionDenied))
	if e, ok := AsError(err); !ok || e.Code != CodePermissionDenied || e.Attempts != 1 || calls != 1 || len(sleeps.delays) != 0 {
		t.Fatalf("err %+v, calls %d", err, calls)
	}
}

func TestRunnerOverloadedIsRetriedOnlyForBulkWork(t *testing.T) {
	r, _ := testRunner(RequestProfile, discardLog())
	calls := 0
	if e, _ := AsError(r.do(context.Background(), "op", failing(&calls, codes.ResourceExhausted))); e == nil || e.Code != CodeOverloaded || calls != 1 {
		t.Fatalf("request path: err %+v, calls %d", e, calls)
	}
	bulk, _ := testRunner(BulkProfile, discardLog())
	calls = 0
	if err := bulk.do(context.Background(), "op", failing(&calls, codes.ResourceExhausted, codes.ResourceExhausted)); err != nil || calls != 3 {
		t.Fatalf("bulk: err %v, calls %d", err, calls)
	}
}

func TestRunnerLogsOnlySanitizedMetadata(t *testing.T) {
	var buf bytes.Buffer
	r, _ := testRunner(RequestProfile, slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	calls := 0
	_ = r.do(context.Background(), "check_permission", failing(&calls, codes.Unavailable, codes.Unavailable, codes.Unavailable))
	out := buf.String()
	if strings.Contains(out, "tenant") || !strings.Contains(out, "error_code=authzed_unavailable") || !strings.Contains(out, "attempt_count=3") {
		t.Fatalf("log = %s", out)
	}
}

func TestRunnerOpensTheCircuitAndThenAnswersWithoutACall(t *testing.T) {
	clock := newFakeClock()
	r, _ := testRunner(RequestProfile, discardLog())
	r.breaker = newBreaker(clock.now)
	calls := 0
	for range circuitFailureThreshold {
		_ = r.do(context.Background(), "op", func(context.Context) error {
			calls++
			return status.Error(codes.DeadlineExceeded, "")
		})
	}
	before := calls
	err := r.do(context.Background(), "op", func(context.Context) error { calls++; return nil })
	e, ok := AsError(err)
	if !ok || e.Code != CodeTimeout || !e.Retryable || e.Attempts != 0 || calls != before {
		t.Fatalf("short-circuit: err %+v, calls %d -> %d", err, before, calls)
	}

	// After the cooldown one single-attempt probe goes out, and its success closes the circuit.
	clock.advance(circuitCooldown)
	probeCalls := 0
	err = r.do(context.Background(), "op", func(context.Context) error {
		probeCalls++
		return status.Error(codes.Unavailable, "")
	})
	if e, _ := AsError(err); e == nil || e.Attempts != 1 || probeCalls != 1 {
		t.Fatalf("failed probe: err %+v, calls %d", e, probeCalls)
	}
	clock.advance(circuitCooldown)
	if err := r.do(context.Background(), "op", func(context.Context) error { return nil }); err != nil {
		t.Fatalf("successful probe: %v", err)
	}
	if a := r.breaker.admit(); !a.admitted || a.probe {
		t.Fatalf("after a successful probe admit = %+v, want closed", a)
	}
}

func TestRunnerBulkHasNoCircuit(t *testing.T) {
	r, _ := testRunner(BulkProfile, discardLog())
	if r.breaker != nil {
		t.Fatal("bulk runner has a circuit breaker")
	}
}

func TestRetryDelayJitterStaysWithinTwentyPercent(t *testing.T) {
	cases := []struct {
		index  int
		random float64
		want   time.Duration
	}{
		{0, 0, 80 * time.Millisecond},
		{0, 1, 120 * time.Millisecond},
		{0, 2, 120 * time.Millisecond},
		{1, -1, 160 * time.Millisecond},
		{1, 0.5, 200 * time.Millisecond},
		{1, 1, 240 * time.Millisecond},
	}
	for _, tc := range cases {
		if got := retryDelay(tc.index, tc.random); got != tc.want {
			t.Errorf("retryDelay(%d, %v) = %v, want %v", tc.index, tc.random, got, tc.want)
		}
	}
}

func TestRunnerGivesEachAttemptItsOwnDeadline(t *testing.T) {
	r, _ := testRunner(RequestProfile, discardLog())
	var deadlines []time.Time
	calls := 0
	_ = r.do(context.Background(), "op", func(ctx context.Context) error {
		d, _ := ctx.Deadline()
		deadlines = append(deadlines, d)
		calls++
		if calls < 2 {
			return status.Error(codes.Unavailable, "")
		}
		return nil
	})
	if len(deadlines) != 2 || deadlines[0].IsZero() || time.Until(deadlines[1]) > requestTimeout {
		t.Fatalf("deadlines = %v", deadlines)
	}
}
