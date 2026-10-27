package webhook

import (
	"math/rand/v2"
	"sync"
	"time"

	"github.com/google/uuid"
)

// The breaker keeps one webhook's endpoint from taking the worker's delivery slots from every other webhook. Each worker process lets at most a few deliveries to the same webhook run at once (hookSlots), and stops sending to a webhook whose last breakerThreshold attempts timed out or answered 5xx: its circuit opens for breakerOpenFor, then lets a single delivery through as a probe, which closes it again when the endpoint answers. A delivery the breaker holds back is snoozed, which frees its slot at once and does not count as one of its attempts, so it still gets all its retries once the endpoint recovers. The state lives in the process: two worker replicas each keep their own counts, which is close enough for a guard of slots, and a restart starts every webhook closed.
//
// Every snooze ends in the delivery being fetched again, which takes a slot of the queue for a moment, so the deliveries held back for a webhook are paced: each is handed a time a pace after the last one held for the same webhook, and they come back one at a time however many there are. The queue's cost of a webhook's backlog is then a fetch per pace, not a fetch of every delivery every few minutes, which with thousands of deliveries held would fill the queue for as long as the endpoint stays down. The price is that a long backlog takes as long to come back: n deliveries held for an open circuit come back over n times openPace.
//
// Snoozing reorders deliveries: a delivery held back runs after later ones that found a free slot. Webhook deliveries were never ordered (a failed attempt is retried after later events are delivered); receivers order events by the timestamps in their data, and deduplicate on the webhook-id header.
const (
	// maxHookSlots is how many deliveries to one webhook a worker process runs at once when its queue has slots to spare.
	maxHookSlots = 2
	// breakerThreshold is how many attempts in a row that time out or answer 5xx open a webhook's circuit.
	breakerThreshold = 5
	// breakerOpenFor is how long an open circuit holds every delivery to the webhook back before it lets a probe through.
	breakerOpenFor = 2 * time.Minute
	// openPace is how far apart the deliveries an open circuit holds back come back: one per second per webhook and worker process, however many are held.
	openPace = time.Second
	// minBusyPace is the shortest time between the deliveries waiting for one of a webhook's slots coming back, however fast its endpoint answers.
	minBusyPace = 100 * time.Millisecond
)

// hookSlots is how many deliveries to one webhook a worker process whose queue runs concurrency jobs at once lets run: maxHookSlots, but always one slot less than the queue has, so a webhook whose endpoint is slow but answers in time, which never opens its circuit, cannot hold every slot of a small worker (WORKER_CONCURRENCY defaults to 2 on a 2-CPU host). A queue of one slot gives it that one.
func hookSlots(concurrency int) int {
	return min(maxHookSlots, max(1, concurrency-1))
}

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
	// outcomeSkipped is a delivery that sent nothing and says nothing about its webhook: its response is gone, a resend delivered it, or its request could not be built.
	outcomeSkipped outcome = iota
	// outcomeAnswered is an endpoint that answered in time with anything but 5xx, or a URL refused before anything was sent: nothing to hold back.
	outcomeAnswered
	// outcomeFailed is an attempt that timed out or got a 5xx answer, the endpoint failures that open the circuit.
	outcomeFailed
	// outcomeGone is a delivery that sent nothing because its webhook was deleted or no longer subscribes to the event: the deliveries held back for it are dropped as they come back, so its circuit no longer holds them.
	outcomeGone
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
	// transitionReset is an open circuit dropped because its webhook was deleted or no longer subscribes.
	transitionReset
)

// admission is the breaker's answer to a delivery that asks to run.
type admission struct {
	// Wait is how long the delivery is snoozed. Zero admits it, and the worker reports its outcome to done.
	Wait time.Duration
	// Open reports that the delivery waits because the webhook's circuit is open, not for a slot.
	Open  bool
	probe bool
	// at is when the delivery was admitted, from which done learns how long the webhook's deliveries hold a slot.
	at time.Time
}

// hookState is one webhook's slots, circuit and paced waits. A webhook with no delivery running, a closed circuit, no failure counted and no delivery held back still to come back has none.
type hookState struct {
	running int
	// failures is the attempts in a row that counted against the endpoint.
	failures int
	// openUntil is when an open circuit lets a probe through; zero while the circuit is closed.
	openUntil time.Time
	// probing is a probe running while the circuit is open.
	probing bool
	// openNext and busyNext are the times handed to the last delivery held back for the open circuit and for a slot. The next one held is handed a time a pace after it, so the deliveries held back for the webhook come back one at a time. The two are kept apart so that a backlog held for an open circuit does not delay, once the circuit closes, a delivery that only waits for a slot.
	openNext, busyNext time.Time
	// took is how long the webhook's sent deliveries hold a slot, a moving average; zero until one has ended.
	took time.Duration
}

// busyPace is how far apart the deliveries waiting for one of the webhook's slots come back: as often as a slot frees, judged by how long its deliveries take, and no more often than minBusyPace. Until one has ended it takes the request timeout, the longest a delivery holds a slot.
func (s *hookState) busyPace(slots int) time.Duration {
	took := s.took
	if took == 0 {
		took = timeout
	}
	return max(minBusyPace, took/time.Duration(slots))
}

// breaker is the per-webhook guard of a worker process's delivery slots.
type breaker struct {
	mu    sync.Mutex
	hooks map[uuid.UUID]*hookState
	// slots is how many deliveries to one webhook run at once (hookSlots).
	slots int
	now   func() time.Time
	// random is a number in [0, 1), which spreads the waits.
	random func() float64
}

func newBreaker(slots int) *breaker {
	return &breaker{hooks: map[uuid.UUID]*hookState{}, slots: slots, now: time.Now, random: rand.Float64}
}

// hold hands a delivery held back the time it comes back: a pace after the time handed to the last one held (*next), and no earlier than from, and returns its wait. The random part, within the pace, keeps worker processes that each pace their own deliveries from fetching them in step.
func (b *breaker) hold(next *time.Time, now, from time.Time, pace time.Duration) time.Duration {
	at := now
	if from.After(at) {
		at = from
	}
	if next.After(at) {
		at = *next
	}
	at = at.Add(pace)
	*next = at
	return at.Sub(now) + time.Duration(b.random()*float64(pace))
}

// enter admits a delivery to the webhook or tells it how long to wait. A delivery waits while the webhook's circuit is open or a probe of it runs, and then comes back openPace after the last one held for the circuit, the first of them openPace after the circuit's time. It also waits while the webhook's slots are all taken, and then comes back busyPace after the last one held for a slot. pastDeadline is a delivery past giveUpAfter, which waits only for a slot.
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
		if now.Before(s.openUntil) || s.probing {
			return admission{Wait: b.hold(&s.openNext, now, s.openUntil, openPace), Open: true}
		}
		probe = true
	}
	if s.running >= b.slots {
		return admission{Wait: b.hold(&s.busyNext, now, now, s.busyPace(b.slots))}
	}
	s.running++
	s.probing = s.probing || probe
	return admission{probe: probe, at: now}
}

// done records the outcome of a delivery enter admitted and frees its slot. An endpoint that answered closes the circuit; breakerThreshold failures in a row open it, and a failed probe opens it again. A delivery sent past its deadline through an open circuit closes it when it is answered and otherwise leaves it as it is. A webhook found gone drops its circuit, so the deliveries held back for it are admitted, and dropped, as they come back instead of one probe at a time.
func (b *breaker) done(hook uuid.UUID, adm admission, o outcome) transition {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
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
			s.openUntil, t = now.Add(breakerOpenFor), transitionReopened
		case s.openUntil.IsZero() && s.failures >= breakerThreshold:
			s.openUntil, t = now.Add(breakerOpenFor), transitionOpened
		}
	case outcomeGone:
		if !s.openUntil.IsZero() {
			t = transitionReset
		}
		s.failures, s.openUntil = 0, time.Time{}
	case outcomeSkipped:
	}
	if o == outcomeAnswered || o == outcomeFailed {
		if took := now.Sub(adm.at); s.took == 0 {
			s.took = took
		} else {
			s.took += (took - s.took) / 4
		}
	}
	if s.running == 0 && s.openUntil.IsZero() && s.failures == 0 && !s.openNext.After(now) && !s.busyNext.After(now) {
		delete(b.hooks, hook)
	}
	return t
}
