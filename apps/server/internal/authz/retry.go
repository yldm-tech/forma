package authz

import (
	"context"
	"log/slog"
	"math"
	"math/rand/v2"
	"time"
)

// Retry limits shared by every SpiceDB call.
const (
	maxAttempts       = 3
	retryJitterRatio  = 0.2
	requestTimeout    = time.Second
	bulkTimeout       = 30 * time.Second
	maxUpdatesPerCall = 1000
	maxParallelDelete = 10
)

// retryBaseDelays are the waits before the second and third attempts, before jitter.
var retryBaseDelays = [...]time.Duration{100 * time.Millisecond, 200 * time.Millisecond}

// Profile is how a process talks to SpiceDB: the per-call deadline and the retry policy that goes with it. They are one decision. A request-serving process wants a short deadline and refuses to answer an overloaded SpiceDB with more calls; an operator command wants the long deadline and keeps retrying, because finishing matters more than the load it adds.
type Profile struct {
	timeout time.Duration
	bulk    bool
}

var (
	// RequestProfile is for `forma serve` and `forma worker`: a 1 s deadline, no retry of authzed_overloaded, and the circuit breaker.
	RequestProfile = Profile{timeout: requestTimeout}
	// BulkProfile is for the `forma authz` commands: a 30 s deadline (SpiceDB's own streaming default), retries of every retryable class, and no circuit breaker.
	BulkProfile = Profile{timeout: bulkTimeout, bulk: true}
)

// runner executes one SpiceDB operation under the profile's deadline, retry policy and (on the request path) circuit breaker.
type runner struct {
	profile Profile
	breaker *breaker
	log     *slog.Logger
	random  func() float64
	sleep   func(ctx context.Context, d time.Duration) error
}

func newRunner(profile Profile, log *slog.Logger) *runner {
	r := &runner{profile: profile, log: log, random: rand.Float64, sleep: sleepContext}
	// Bulk work is deliberately unguarded: a backfill that gives up on a blip leaves the projection drifted, and no user's latency is at stake.
	if !profile.bulk {
		r.breaker = newBreaker(time.Now)
	}
	return r
}

func sleepContext(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// retryDelay is the base delay for retryIndex with ±20% jitter.
func retryDelay(retryIndex int, random float64) time.Duration {
	random = math.Min(1, math.Max(0, random))
	multiplier := 1 - retryJitterRatio + random*retryJitterRatio*2
	return time.Duration(math.Round(float64(retryBaseDelays[retryIndex].Milliseconds())*multiplier)) * time.Millisecond
}

// shouldRetry: authzed_overloaded is SpiceDB saying it is past capacity, so the request path does not answer it with more calls; bulk work retries it because the backoff is the point.
func (r *runner) shouldRetry(e *Error) bool {
	return e.Retryable && (r.profile.bulk || e.Code != CodeOverloaded)
}

// do runs call, each attempt under its own deadline, and returns nil or an *Error.
func (r *runner) do(ctx context.Context, operation string, call func(ctx context.Context) error) error {
	started := time.Now()
	attempts := maxAttempts
	if r.breaker != nil {
		adm := r.breaker.admit()
		if !adm.admitted {
			r.log.WarnContext(ctx, "AuthZed request short-circuited by an open circuit", "component", "authz", "attempt_count", 0, "error_code", adm.code, "operation", operation)
			recordShortCircuit(ctx, adm.code, operation)
			return newError(adm.code, operation, true, 0)
		}
		// A half-open probe only asks whether SpiceDB is back: one call, not three plus two backoffs.
		if adm.probe {
			attempts = 1
		}
	}

	for attempt := 1; ; attempt++ {
		attemptCtx, cancel := context.WithTimeout(ctx, r.profile.timeout)
		err := call(attemptCtx)
		cancel()
		if err == nil {
			if r.breaker != nil {
				r.breaker.recordSuccess()
			}
			return nil
		}

		e := mapError(err, operation, attempt)
		if !r.shouldRetry(e) || attempt >= attempts {
			r.log.WarnContext(ctx, "AuthZed request failed", "component", "authz", "attempt_count", attempt, "duration_ms", time.Since(started).Milliseconds(), "error_code", e.Code, "grpc_status", e.GRPCStatus, "operation", operation, "retryable", e.Retryable)
			recordRequestFailure(ctx, e)
			if r.breaker != nil {
				r.breaker.recordFailure(e.Code)
			}
			return e
		}

		recordRequestRetry(ctx, e)
		delay := retryDelay(attempt-1, r.random())
		r.log.DebugContext(ctx, "AuthZed request retry scheduled", "component", "authz", "attempt_count", attempt, "duration_ms", time.Since(started).Milliseconds(), "error_code", e.Code, "grpc_status", e.GRPCStatus, "operation", operation, "retryable", e.Retryable, "retry_delay_ms", delay.Milliseconds())
		if err := r.sleep(ctx, delay); err != nil {
			return mapError(err, operation, attempt)
		}
	}
}
