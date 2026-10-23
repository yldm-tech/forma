package authz

import (
	"sync"
	"time"
)

// Circuit breaker limits for the request path.
const (
	// circuitFailureThreshold is how many consecutive terminal saturation failures open the circuit.
	circuitFailureThreshold = 5
	// circuitCooldown is how long an open circuit refuses calls before it admits one probe.
	circuitCooldown = 5 * time.Second
)

type circuitState int

const (
	circuitClosed circuitState = iota
	circuitHalfOpen
	circuitOpen
)

func (s circuitState) String() string {
	switch s {
	case circuitHalfOpen:
		return "half_open"
	case circuitOpen:
		return "open"
	default:
		return "closed"
	}
}

// admission is the breaker's answer to "may this call go out": admitted (possibly as the single half-open probe), or refused with the saturation code the circuit opened on.
type admission struct {
	admitted bool
	probe    bool
	code     Code
}

// breaker is the process-local circuit breaker of the request path. It removes the retry multiplier and the deadline wait while SpiceDB is saturated or unreachable; it never turns an outage into a denial, because a refused call fails with the same typed error the exhausted retries would have produced. Only overloaded, timeout and unavailable count: any other failure means SpiceDB answered, so it breaks the streak and closes the circuit.
type breaker struct {
	now func() time.Time

	mu                  sync.Mutex
	state               circuitState
	consecutiveFailures int
	openedAt            time.Time
	probeInFlight       bool
	lastFailureCode     Code
}

func newBreaker(now func() time.Time) *breaker {
	return &breaker{now: now, lastFailureCode: CodeUnavailable}
}

func isSaturation(code Code) bool {
	return code == CodeOverloaded || code == CodeTimeout || code == CodeUnavailable
}

func (b *breaker) admit() admission {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state == circuitClosed {
		return admission{admitted: true}
	}
	if b.state == circuitOpen && b.now().Sub(b.openedAt) < circuitCooldown {
		return admission{code: b.lastFailureCode}
	}
	if b.probeInFlight {
		return admission{code: b.lastFailureCode}
	}
	b.probeInFlight = true
	b.transition(circuitHalfOpen)
	return admission{admitted: true, probe: true}
}

func (b *breaker) recordSuccess() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.close()
}

func (b *breaker) recordFailure(code Code) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !isSaturation(code) {
		b.close()
		return
	}
	b.lastFailureCode = code
	// A failed probe reopens for a full cooldown rather than rebuilding the streak, which would admit a probe on every call until it built back up.
	if b.probeInFlight || b.state == circuitHalfOpen {
		b.open()
		return
	}
	b.consecutiveFailures++
	if b.consecutiveFailures >= circuitFailureThreshold {
		b.open()
	}
}

func (b *breaker) close() {
	b.consecutiveFailures = 0
	b.probeInFlight = false
	b.transition(circuitClosed)
}

func (b *breaker) open() {
	b.consecutiveFailures = 0
	b.openedAt = b.now()
	b.probeInFlight = false
	b.transition(circuitOpen)
}

// transition moves the breaker to next and, when that changes its state, records the new state set.
func (b *breaker) transition(next circuitState) {
	if b.state == next {
		return
	}
	b.state = next
	recordCircuitState(next)
}
