package authz

import (
	"testing"
	"time"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }
func newFakeClock() *fakeClock               { return &fakeClock{t: time.Unix(1_700_000_000, 0)} }
func failTimes(b *breaker, code Code, n int) {
	for range n {
		b.admit()
		b.recordFailure(code)
	}
}

func TestBreakerStaysClosedBelowThreshold(t *testing.T) {
	b := newBreaker(newFakeClock().now)
	failTimes(b, CodeUnavailable, circuitFailureThreshold-1)
	if a := b.admit(); !a.admitted || a.probe {
		t.Fatalf("admit = %+v, want closed", a)
	}
}

func TestBreakerOpensOnSaturationAndRefusesWithTheLastCode(t *testing.T) {
	b := newBreaker(newFakeClock().now)
	failTimes(b, CodeTimeout, circuitFailureThreshold-1)
	failTimes(b, CodeOverloaded, 1)
	if a := b.admit(); a.admitted || a.code != CodeOverloaded {
		t.Fatalf("admit = %+v, want refused with authzed_overloaded", a)
	}
}

func TestBreakerStreakIsBrokenBySuccessOrByANonSaturationFailure(t *testing.T) {
	for name, interrupt := range map[string]func(*breaker){
		"success":         func(b *breaker) { b.recordSuccess() },
		"invalid request": func(b *breaker) { b.recordFailure(CodeInvalidRequest) },
	} {
		t.Run(name, func(t *testing.T) {
			b := newBreaker(newFakeClock().now)
			failTimes(b, CodeUnavailable, circuitFailureThreshold-1)
			interrupt(b)
			failTimes(b, CodeUnavailable, circuitFailureThreshold-1)
			if a := b.admit(); !a.admitted || a.probe {
				t.Fatalf("admit = %+v, want closed", a)
			}
		})
	}
}

func TestBreakerAdmitsExactlyOneProbePerCooldown(t *testing.T) {
	clock := newFakeClock()
	b := newBreaker(clock.now)
	failTimes(b, CodeUnavailable, circuitFailureThreshold)
	clock.advance(circuitCooldown - time.Millisecond)
	if a := b.admit(); a.admitted {
		t.Fatalf("admit inside cooldown = %+v", a)
	}
	clock.advance(time.Millisecond)
	if a := b.admit(); !a.admitted || !a.probe {
		t.Fatalf("admit after cooldown = %+v, want a probe", a)
	}
	if a := b.admit(); a.admitted || a.code != CodeUnavailable {
		t.Fatalf("second admit while probing = %+v, want refused", a)
	}
}

func TestBreakerFailedProbeReopensForAFullCooldown(t *testing.T) {
	clock := newFakeClock()
	b := newBreaker(clock.now)
	failTimes(b, CodeUnavailable, circuitFailureThreshold)
	clock.advance(circuitCooldown)
	b.admit()
	b.recordFailure(CodeTimeout)
	clock.advance(circuitCooldown - time.Millisecond)
	if a := b.admit(); a.admitted || a.code != CodeTimeout {
		t.Fatalf("admit = %+v, want refused with authzed_timeout", a)
	}
	clock.advance(time.Millisecond)
	if a := b.admit(); !a.admitted || !a.probe {
		t.Fatalf("admit = %+v, want a probe", a)
	}
}

func TestBreakerSuccessfulProbeCloses(t *testing.T) {
	clock := newFakeClock()
	b := newBreaker(clock.now)
	failTimes(b, CodeUnavailable, circuitFailureThreshold)
	clock.advance(circuitCooldown)
	b.admit()
	b.recordSuccess()
	if a := b.admit(); !a.admitted || a.probe {
		t.Fatalf("admit = %+v, want closed", a)
	}
}
