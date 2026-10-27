package webhook

import (
	"math/rand/v2"
	"sync"
	"time"

	"github.com/google/uuid"
)

// The breaker keeps one webhook's endpoint from taking the worker's delivery slots from every other webhook. Each worker process lets at most hookConcurrency deliveries to the same webhook run at once, and stops sending to a webhook whose last breakerThreshold attempts timed out or answered 5xx: its circuit opens for breakerOpenFor, then lets a single delivery through as a probe, which closes it again when the endpoint answers. A delivery the breaker holds back is snoozed, which frees its slot at once and does not count as one of its attempts, so it still gets all its retries once the endpoint recovers. The state lives in the process: two worker replicas each keep their own counts, which is close enough for a guard of slots, and a restart starts every webhook closed.
//
// Snoozing reorders deliveries: a delivery held back runs after later ones that found a free slot. Webhook deliveries were never ordered (a failed attempt is retried after later events are delivered); receivers order events by the timestamps in their data, and deduplicate on the webhook-id header.
const (
	// hookConcurrency is how many deliveries to one webhook a worker process runs at once.
	hookConcurrency = 2
	// breakerThreshold is how many attempts in a row that time out or answer 5xx open a webhook's circuit.
	breakerThreshold = 5
	// breakerOpenFor is how long an open circuit holds every delivery to the webhook back before it lets a probe through.
	breakerOpenFor = 2 * time.Minute
	// openJitter spreads the deliveries an open circuit held back over this long after it ends, so they do not all come back at once.
	openJitter = 30 * time.Second
	// maxBusyWait caps how long a delivery waits for one of its webhook's slots, however many others wait too. Every wait ends in the delivery being fetched again, which takes a slot of the queue for a moment, so a backlog of hundreds of deliveries for one slow endpoint must come back over minutes, not seconds, or its comings and goings crowd out the other jobs.
	maxBusyWait = 2 * time.Minute
)

// giveUpAfter is how long after it was queued a delivery stops waiting for an open circuit: the span of its retry schedule, about 28 hours. A delivery past it is sent even through an open circuit, as its last attempt, so a webhook whose endpoint never comes back loses its deliveries after the same time as before there was a breaker, instead of keeping them snoozed forever.
var giveUpAfter = func() time.Duration {
	var total time.Duration
	for _, d := range retrySchedule {
		total += d
	}
	return total
}()

// outcome is what an admitted delivery tells the breaker about its endpoint.
type outcome int

const (
	// outcomeSkipped is a delivery that sent nothing: its webhook or response is gone, a resend delivered it, or its request could not be built.
	outcomeSkipped outcome = iota
	// outcomeAnswered is an endpoint that answered in time with anything but 5xx, or a URL refused before anything was sent: nothing to hold back.
	outcomeAnswered
	// outcomeFailed is an attempt that timed out or got a 5xx answer, the endpoint failures that open the circuit.
	outcomeFailed
)

// outcomeOf is how the breaker counts an attempt. Only a timeout and a 5xx answer count against the endpoint: a timeout is the failure that holds a slot for the whole request timeout, and a 5xx is an endpoint that is down. A 4xx, a redirect, an unreachable host or a refused URL fail fast and say nothing about the endpoint's capacity.
func outcomeOf(a Attempt) outcome {
	if a.Failure == FailureTimeout || (a.Failure == FailureStatus && a.StatusCode >= 500) {
		return outcomeFailed
	}
	return outcomeAnswered
}

// transition is a change of a webhook's circuit that the worker logs.
type transition int

const (
	transitionNone transition = iota
	// transitionOpened is a closed circuit that opened.
	transitionOpened
	// transitionReopened is an open circuit whose probe failed.
	transitionReopened
	// transitionClosed is an open circuit closed by a delivery the endpoint answered.
	transitionClosed
)

// admission is the breaker's answer to a delivery that asks to run.
type admission struct {
	// Wait is how long the delivery is snoozed. Zero admits it, and the worker reports its outcome to done.
	Wait time.Duration
	// Open reports that the delivery waits because the webhook's circuit is open, not for a slot.
	Open  bool
	probe bool
}

// hookState is one webhook's slots and circuit. A webhook with no delivery running, a closed circuit and no failure counted has none.
type hookState struct {
	running int
	// turnedAway is how many deliveries were told to wait for a slot since the webhook last had none running, a rough count of the deliveries waiting for it.
	turnedAway int
	// failures is the attempts in a row that counted against the endpoint.
	failures int
	// openUntil is when an open circuit lets a probe through; zero while the circuit is closed.
	openUntil time.Time
	// probing is a probe running while the circuit is open.
	probing bool
}

// breaker is the per-webhook guard of a worker process's delivery slots.
type breaker struct {
	mu    sync.Mutex
	hooks map[uuid.UUID]*hookState
	now   func() time.Time
	// random is a number in [0, 1), which spreads the waits.
	random func() float64
}

func newBreaker() *breaker {
	return &breaker{hooks: map[uuid.UUID]*hookState{}, now: time.Now, random: rand.Float64}
}

// spread is d plus a random part of up to spreadBy.
func (b *breaker) spread(d, spreadBy time.Duration) time.Duration {
	return d + time.Duration(b.random()*float64(spreadBy))
}

// enter admits a delivery to the webhook or tells it how long to wait. A delivery waits while the webhook's circuit is open, while a probe of the open circuit runs, and while hookConcurrency deliveries to the webhook are running; the wait for a slot grows with the number of deliveries waiting for one, up to maxBusyWait, so a long backlog for one webhook is not fetched again every few seconds: each delivery waits about as long as the deliveries ahead of it would take if each took the full request timeout. pastDeadline is a delivery past giveUpAfter, which waits only for a slot.
func (b *breaker) enter(hook uuid.UUID, pastDeadline bool) admission {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	s := b.hooks[hook]
	if s == nil {
		s = &hookState{}
		b.hooks[hook] = s
	}
	probe := false
	if !s.openUntil.IsZero() && !pastDeadline {
		if now.Before(s.openUntil) {
			return admission{Wait: b.spread(s.openUntil.Sub(now), openJitter), Open: true}
		}
		if s.probing {
			return admission{Wait: b.spread(timeout/2, timeout), Open: true}
		}
		probe = true
	}
	if s.running >= hookConcurrency {
		s.turnedAway++
		busy := min(timeout*time.Duration(max(1, s.turnedAway/hookConcurrency)), maxBusyWait)
		return admission{Wait: b.spread(busy/2, busy)}
	}
	s.running++
	s.probing = s.probing || probe
	return admission{probe: probe}
}

// done records the outcome of a delivery enter admitted and frees its slot. An endpoint that answered closes the circuit; breakerThreshold failures in a row open it, and a failed probe opens it again. A delivery sent past its deadline through an open circuit closes it when it is answered and otherwise leaves it as it is.
func (b *breaker) done(hook uuid.UUID, adm admission, o outcome) transition {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.hooks[hook]
	s.running--
	if adm.probe {
		s.probing = false
	}
	t := transitionNone
	switch o {
	case outcomeAnswered:
		if !s.openUntil.IsZero() {
			t = transitionClosed
		}
		s.failures, s.openUntil = 0, time.Time{}
	case outcomeFailed:
		s.failures++
		switch {
		case adm.probe:
			s.openUntil, t = b.now().Add(breakerOpenFor), transitionReopened
		case s.openUntil.IsZero() && s.failures >= breakerThreshold:
			s.openUntil, t = b.now().Add(breakerOpenFor), transitionOpened
		}
	case outcomeSkipped:
	}
	if s.running == 0 {
		s.turnedAway = 0
		if s.openUntil.IsZero() && s.failures == 0 {
			delete(b.hooks, hook)
		}
	}
	return t
}
