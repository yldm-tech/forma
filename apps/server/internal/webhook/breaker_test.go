package webhook

import (
	"container/heap"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river/rivertype"
)

// testBreaker is a breaker on a clock the test moves, whose waits carry no random part.
func testBreaker() (*breaker, *time.Time) {
	now := time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)
	b := newBreaker(maxHookSlots)
	b.now = func() time.Time { return now }
	b.random = func() float64 { return 0 }
	return b, &now
}

// Each webhook gets its slots' worth of deliveries at once; the next one waits for a slot, and another webhook is not held back by it.
func TestBreakerCapsDeliveriesPerWebhook(t *testing.T) {
	t.Parallel()
	b, _ := testBreaker()
	slow, other := uuid.New(), uuid.New()
	var running []admission
	for range b.slots {
		adm := b.enter(slow, false)
		if adm.Wait != 0 {
			t.Fatalf("a delivery within the cap waits %s", adm.Wait)
		}
		running = append(running, adm)
	}
	adm := b.enter(slow, false)
	if adm.Wait != timeout/time.Duration(b.slots) || adm.Open {
		t.Fatalf("a delivery over the cap = %+v, want a wait for a slot of the request timeout's share", adm)
	}
	if adm := b.enter(other, false); adm.Wait != 0 {
		t.Fatalf("another webhook's delivery waits %s", adm.Wait)
	}
	b.done(slow, running[0], outcomeAnswered)
	if adm := b.enter(slow, false); adm.Wait != 0 {
		t.Fatalf("a freed slot is not given out: %+v", adm)
	}
}

// The deliveries waiting for a slot come back one at a time, a pace apart, however many wait: the request timeout's share of a slot until the webhook's deliveries have shown how long they take, then that share, and never less than minBusyPace.
func TestBreakerPacesTheDeliveriesWaitingForASlot(t *testing.T) {
	t.Parallel()
	b, now := testBreaker()
	hook := uuid.New()
	var running []admission
	for range b.slots {
		running = append(running, b.enter(hook, false))
	}
	pace := timeout / time.Duration(b.slots)
	for i := range 1000 {
		if got, want := b.enter(hook, false).Wait, time.Duration(i+1)*pace; got != want {
			t.Fatalf("waiting delivery %d waits %s, want %s", i+1, got, want)
		}
	}
	tail := 1000 * pace
	*now = now.Add(time.Second)
	b.done(hook, running[0], outcomeAnswered)
	running[0] = b.enter(hook, false)
	learned := time.Second / time.Duration(b.slots)
	if got, want := b.enter(hook, false).Wait, tail-time.Second+learned; got != want {
		t.Fatalf("after a delivery that took a second, the next waits %s, want %s", got, want)
	}
	*now = now.Add(time.Millisecond)
	for range 20 {
		b.done(hook, running[0], outcomeAnswered)
		running[0] = b.enter(hook, false)
	}
	before := b.enter(hook, false).Wait
	if got := b.enter(hook, false).Wait - before; got != minBusyPace {
		t.Fatalf("for an endpoint that answers at once, waiting deliveries come back %s apart, want %s", got, minBusyPace)
	}
}

// A webhook's slots are one fewer than its queue's, so a slow endpoint that never opens its circuit cannot hold every slot of a small worker; a queue of one slot gives it that one.
func TestHookSlots(t *testing.T) {
	t.Parallel()
	for concurrency, want := range map[int]int{1: 1, 2: 1, 3: 2, 4: 2, 8: 2, 32: 2} {
		if got := hookSlots(concurrency); got != want {
			t.Errorf("hookSlots(%d) = %d, want %d", concurrency, got, want)
		}
	}
}

// breakerThreshold timeouts or 5xx answers in a row open the circuit; an answer in between starts the count again. While open every delivery waits until it ends, the deliveries held back coming back openPace apart; then one probe goes through, a failed probe opens it again, and an answered one closes it.
func TestBreakerOpensAndRecovers(t *testing.T) {
	t.Parallel()
	b, now := testBreaker()
	hook := uuid.New()
	fail := func() transition {
		t.Helper()
		adm := b.enter(hook, false)
		if adm.Wait != 0 {
			t.Fatalf("a delivery to a closed circuit waits %+v", adm)
		}
		return b.done(hook, adm, outcomeFailed)
	}
	for range breakerThreshold - 1 {
		fail()
	}
	b.done(hook, b.enter(hook, false), outcomeAnswered)
	for range breakerThreshold - 1 {
		if tr := fail(); tr != transitionNone {
			t.Fatalf("the circuit changed before the threshold: %v", tr)
		}
	}
	if tr := fail(); tr != transitionOpened {
		t.Fatalf("the threshold failure = %v, want opened", tr)
	}

	adm := b.enter(hook, false)
	if !adm.Open || adm.Wait != breakerOpenFor+openPace {
		t.Fatalf("a delivery to an open circuit = %+v, want a wait of %s", adm, breakerOpenFor+openPace)
	}
	*now = now.Add(breakerOpenFor / 2)
	if adm := b.enter(hook, false); !adm.Open || adm.Wait != breakerOpenFor/2+2*openPace {
		t.Fatalf("halfway through = %+v", adm)
	}

	*now = now.Add(breakerOpenFor / 2)
	probe := b.enter(hook, false)
	if probe.Wait != 0 || !probe.probe {
		t.Fatalf("the first delivery after the circuit's time = %+v, want a probe", probe)
	}
	if adm := b.enter(hook, false); !adm.Open || adm.Wait == 0 {
		t.Fatalf("a delivery during the probe = %+v, want a wait", adm)
	}
	if tr := b.done(hook, probe, outcomeFailed); tr != transitionReopened {
		t.Fatalf("a failed probe = %v, want reopened", tr)
	}
	if adm := b.enter(hook, false); !adm.Open || adm.Wait != breakerOpenFor+openPace {
		t.Fatalf("after a failed probe = %+v", adm)
	}

	*now = now.Add(breakerOpenFor)
	probe = b.enter(hook, false)
	if tr := b.done(hook, probe, outcomeAnswered); tr != transitionClosed {
		t.Fatalf("an answered probe = %v, want closed", tr)
	}
	if adm := b.enter(hook, false); adm.Wait != 0 || adm.probe {
		t.Fatalf("after the circuit closed = %+v", adm)
	}
}

// A probe that sends nothing and says nothing about the webhook (its response is gone, or a resend delivered it) leaves the circuit open for the next delivery to probe.
func TestBreakerSkippedProbe(t *testing.T) {
	t.Parallel()
	b, now := testBreaker()
	hook := uuid.New()
	for range breakerThreshold {
		b.done(hook, b.enter(hook, false), outcomeFailed)
	}
	*now = now.Add(breakerOpenFor)
	if tr := b.done(hook, b.enter(hook, false), outcomeSkipped); tr != transitionNone {
		t.Fatalf("a skipped probe = %v", tr)
	}
	if adm := b.enter(hook, false); adm.Wait != 0 || !adm.probe {
		t.Fatalf("after a skipped probe = %+v, want another probe", adm)
	}
}

// A probe that finds its webhook deleted, or no longer subscribed to the event, drops the circuit: the deliveries held back for it are admitted as they come back, to be dropped, instead of waiting one probe at a time.
func TestBreakerGoneProbeDropsTheCircuit(t *testing.T) {
	t.Parallel()
	b, now := testBreaker()
	hook := uuid.New()
	for range breakerThreshold {
		b.done(hook, b.enter(hook, false), outcomeFailed)
	}
	held := b.enter(hook, false)
	*now = now.Add(breakerOpenFor)
	probe := b.enter(hook, false)
	if !probe.probe {
		t.Fatalf("the first delivery after the circuit's time = %+v, want a probe", probe)
	}
	if tr := b.done(hook, probe, outcomeGone); tr != transitionReset {
		t.Fatalf("a probe that found its webhook gone = %v, want reset", tr)
	}
	*now = now.Add(held.Wait - breakerOpenFor)
	for range b.slots {
		if adm := b.enter(hook, false); adm.Wait != 0 || adm.Open || adm.probe {
			t.Fatalf("a held delivery back after its webhook was found gone = %+v, want it admitted", adm)
		}
	}
}

// A delivery past its deadline goes through an open circuit, still within the webhook's slots, and leaves the circuit open when it fails.
func TestBreakerLetsDeliveriesPastTheirDeadlineThrough(t *testing.T) {
	t.Parallel()
	b, _ := testBreaker()
	hook := uuid.New()
	for range breakerThreshold {
		b.done(hook, b.enter(hook, false), outcomeFailed)
	}
	var running []admission
	for range b.slots {
		adm := b.enter(hook, true)
		if adm.Wait != 0 || adm.probe {
			t.Fatalf("a delivery past its deadline = %+v, want it sent", adm)
		}
		running = append(running, adm)
	}
	if adm := b.enter(hook, true); adm.Wait == 0 || adm.Open {
		t.Fatalf("a delivery past its deadline over the cap = %+v, want a wait for a slot", adm)
	}
	if tr := b.done(hook, running[0], outcomeFailed); tr != transitionNone {
		t.Fatalf("a failed delivery past its deadline = %v", tr)
	}
	if adm := b.enter(hook, false); !adm.Open {
		t.Fatalf("the circuit closed after a failure: %+v", adm)
	}
	if tr := b.done(hook, running[1], outcomeAnswered); tr != transitionClosed {
		t.Fatalf("an answered delivery past its deadline = %v, want closed", tr)
	}
}

// A webhook with nothing running and nothing counted against it leaves no state behind.
func TestBreakerForgetsIdleWebhooks(t *testing.T) {
	t.Parallel()
	b, _ := testBreaker()
	for range 100 {
		hook := uuid.New()
		b.done(hook, b.enter(hook, false), outcomeAnswered)
	}
	if len(b.hooks) != 0 {
		t.Fatalf("%d idle webhooks kept", len(b.hooks))
	}
}

// Only a timeout and a 5xx answer count against the endpoint.
func TestOutcomeOf(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		attempt Attempt
		want    outcome
	}{
		{Attempt{StatusCode: 200}, outcomeAnswered},
		{Attempt{StatusCode: 404, Failure: FailureStatus}, outcomeAnswered},
		{Attempt{StatusCode: 302, Failure: FailureRedirect}, outcomeAnswered},
		{Attempt{Failure: FailureUnreachable}, outcomeAnswered},
		{Attempt{Failure: FailureRefused}, outcomeAnswered},
		{Attempt{StatusCode: 500, Failure: FailureStatus}, outcomeFailed},
		{Attempt{StatusCode: 503, Failure: FailureStatus}, outcomeFailed},
		{Attempt{Failure: FailureTimeout}, outcomeFailed},
	} {
		if got := outcomeOf(c.attempt); got != c.want {
			t.Errorf("outcomeOf(%+v) = %v, want %v", c.attempt, got, c.want)
		}
	}
}

// The webhook's deliveries share the process's slots fairly: while a slow endpoint holds two deliveries, a third to it is snoozed at once without being sent or recorded, and another webhook's delivery goes through.
func TestSlowWebhookIsCappedPerProcess(t *testing.T) {
	e := newEnv(t, true)
	owner := e.user()
	_, ws := e.organization(owner)
	sv := e.survey(ws)
	release := make(chan struct{})
	arrived := make(chan struct{}, 10)
	var slowRequests atomic.Int32
	slow := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		slowRequests.Add(1)
		arrived <- struct{}{}
		<-release
	}))
	t.Cleanup(slow.Close)
	var running sync.WaitGroup
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	// Runs before slow.Close, which waits for the requests the endpoint holds, and lets the deliveries holding them end before the test does.
	t.Cleanup(func() {
		unblock()
		running.Wait()
	})
	fast := newReceiver(t, http.StatusOK)
	slowHook := e.insert(`INSERT INTO webhooks (workspace_id, url, triggers) VALUES (?, ?, '{responseFinished}') RETURNING id`, ws, slow.URL)
	fastHook := e.insert(`INSERT INTO webhooks (workspace_id, url, triggers) VALUES (?, ?, '{responseFinished}') RETURNING id`, ws, fast.URL)
	w := e.worker()
	args := func(hook uuid.UUID, key string) DeliverArgs {
		return DeliverArgs{WebhookID: hook, JobKey: key, Event: "responseFinished", WorkspaceID: ws, SurveyID: sv, ResponseID: e.response(sv), Data: `{}`}
	}

	for i := range w.breaker.slots {
		a := args(slowHook, "slow-"+string(rune('a'+i)))
		running.Go(func() {
			if err := e.work(w, a, 1); err != nil {
				t.Errorf("a slow delivery failed: %v", err)
			}
		})
	}
	for range w.breaker.slots {
		<-arrived
	}

	var snooze *rivertype.JobSnoozeError
	if err := e.work(w, args(slowHook, "slow-over"), 1); !errors.As(err, &snooze) || snooze.Duration <= 0 {
		t.Fatalf("a delivery over the webhook's cap = %v, want a snooze", err)
	}
	if err := e.work(w, args(fastHook, "fast"), 1); err != nil {
		t.Fatalf("another webhook's delivery = %v", err)
	}
	if len(fast.received()) != 1 {
		t.Fatalf("the other webhook got %d requests", len(fast.received()))
	}
	unblock()
	running.Wait()
	if n := slowRequests.Load(); int(n) != w.breaker.slots {
		t.Fatalf("the slow endpoint got %d requests, want %d: the snoozed delivery was sent", n, w.breaker.slots)
	}
	if n := e.count(`SELECT count(*) FROM webhook_deliveries WHERE webhook_id = ?`, slowHook); n != w.breaker.slots {
		t.Fatalf("%d deliveries recorded for the slow webhook, want %d: the snoozed one has no attempt", n, w.breaker.slots)
	}
}

// A webhook whose endpoint keeps failing stops getting deliveries: after breakerThreshold 5xx answers in a row its circuit opens, and the next delivery is snoozed without being sent or counted as an attempt. A delivery queued longer ago than its retry schedule spans is still sent, as its last attempt, and discarded when it fails.
func TestFailingWebhookOpensItsCircuit(t *testing.T) {
	e := newEnv(t, true)
	owner := e.user()
	_, ws := e.organization(owner)
	sv := e.survey(ws)
	rec := newReceiver(t, http.StatusServiceUnavailable)
	hook := e.insert(`INSERT INTO webhooks (workspace_id, url, triggers) VALUES (?, ?, '{responseFinished}') RETURNING id`, ws, rec.URL)
	w := e.worker()
	args := func(key string) DeliverArgs {
		return DeliverArgs{WebhookID: hook, JobKey: key, Event: "responseFinished", WorkspaceID: ws, SurveyID: sv, ResponseID: e.response(sv), Data: `{}`}
	}
	for i := range breakerThreshold {
		if err := e.work(w, args("job-"+string(rune('a'+i))), 1); err == nil {
			t.Fatal("a 503 answer must fail the attempt")
		}
	}
	var snooze *rivertype.JobSnoozeError
	if err := e.work(w, args("held"), 1); !errors.As(err, &snooze) || snooze.Duration < breakerOpenFor {
		t.Fatalf("a delivery to an open circuit = %v, want a snooze of at least %s", err, breakerOpenFor)
	}
	if n := len(rec.received()); n != breakerThreshold {
		t.Fatalf("the endpoint got %d requests, want %d", n, breakerThreshold)
	}

	old := args("old")
	err := e.workQueuedAt(w, old, 2, time.Now().Add(-giveUpAfter-time.Minute))
	var cancel *rivertype.JobCancelError
	if !errors.As(err, &cancel) {
		t.Fatalf("a delivery past its deadline that failed = %v, want it cancelled", err)
	}
	if n := len(rec.received()); n != breakerThreshold+1 {
		t.Fatalf("the delivery past its deadline was not sent: %d requests", n)
	}
	if n := e.count(`SELECT count(*) FROM webhook_deliveries WHERE webhook_id = ? AND message_id = ? AND status = 'failed' AND next_attempt_at IS NULL`,
		hook, MessageID("old", hook, "responseFinished")); n != 1 {
		t.Fatal("the delivery past its deadline is not recorded as failed")
	}

	// The webhook is deleted while its circuit is open: the delivery that probes it next finds it gone and drops the circuit, so the deliveries still held back are admitted, to be dropped, as they come back instead of one probe at a time.
	w.breaker.now = func() time.Time { return time.Now().Add(breakerOpenFor + time.Minute) }
	e.exec(`DELETE FROM webhooks WHERE id = ?`, hook)
	if err := e.work(w, args("after-delete"), 1); err != nil {
		t.Fatalf("a delivery to a deleted webhook = %v", err)
	}
	if !strings.Contains(e.logs.String(), "Webhook circuit dropped") {
		t.Error("dropping the circuit was not logged")
	}
	for range w.breaker.slots {
		if adm := w.breaker.enter(hook, false); adm.Wait != 0 || adm.probe {
			t.Fatalf("a delivery after its webhook was found gone = %+v, want it admitted", adm)
		}
	}
}

// queueFetch is how far apart the queue fetches a burst of new deliveries in the simulations: about 65 a second, what an 8-slot worker fetched on the audit stack.
const queueFetch = 15 * time.Millisecond

// simEvent is a delivery fetched by the queue (finish false) or a sent delivery whose request ends (finish true).
type simEvent struct {
	at       time.Time
	finish   bool
	delivery int
	adm      admission
}

type simQueue []simEvent

func (q simQueue) Len() int { return len(q) }
func (q simQueue) Less(i, j int) bool {
	if !q[i].at.Equal(q[j].at) {
		return q[i].at.Before(q[j].at)
	}
	return q[i].finish && !q[j].finish
}
func (q simQueue) Swap(i, j int) { q[i], q[j] = q[j], q[i] }
func (q *simQueue) Push(x any)   { *q = append(*q, x.(simEvent)) }
func (q *simQueue) Pop() any {
	old := *q
	e := old[len(old)-1]
	*q = old[:len(old)-1]
	return e
}

// simResult is what a simulated backlog did: the fetches of its deliveries in each window, and how many were sent and how many of those the endpoint answered.
type simResult struct {
	fetches        []int
	sent, answered int
}

// simulate runs n deliveries to one webhook, queued arrive apart from the start as the queue would fetch a burst, through the breaker on the test's clock for span: a held delivery is fetched again when its snooze ends, a sent one takes took and ends in result(now), and a failed one is retried on retrySchedule. The queue itself is taken to have slots enough for every fetch, so the counts are what the breaker asks of it.
func simulate(b *breaker, now *time.Time, n int, arrive, took time.Duration, result func(time.Time) outcome, span, window time.Duration) simResult {
	hook := uuid.New()
	start := *now
	res := simResult{fetches: make([]int, int(span/window))}
	attempts := make([]int, n)
	q := &simQueue{}
	for i := range n {
		heap.Push(q, simEvent{at: start.Add(time.Duration(i) * arrive), delivery: i})
	}
	for q.Len() > 0 {
		ev := heap.Pop(q).(simEvent)
		if !ev.at.Before(start.Add(span)) {
			break
		}
		*now = ev.at
		if ev.finish {
			o := result(ev.at)
			b.done(hook, ev.adm, o)
			if o == outcomeFailed && attempts[ev.delivery] < deliveryAttempts {
				heap.Push(q, simEvent{at: ev.at.Add(retryDelay(attempts[ev.delivery])), delivery: ev.delivery})
			}
			if o == outcomeAnswered {
				res.answered++
			}
			continue
		}
		res.fetches[int(ev.at.Sub(start)/window)]++
		adm := b.enter(hook, false)
		if adm.Wait > 0 {
			heap.Push(q, simEvent{at: ev.at.Add(adm.Wait), delivery: ev.delivery})
			continue
		}
		attempts[ev.delivery]++
		res.sent++
		heap.Push(q, simEvent{at: ev.at.Add(took), finish: true, delivery: ev.delivery, adm: adm})
	}
	return res
}

// A large backlog held back by an open circuit costs the queue a steady trickle of fetches, not a fetch of every delivery every few minutes: 10,000 deliveries to an endpoint that answers 503 at once are fetched about once per openPace once the burst is queued, for as long as the endpoint stays down.
func TestBreakerOpenCircuitBacklogIsFetchedAtABoundedRate(t *testing.T) {
	t.Parallel()
	b, now := testBreaker()
	const window = 2 * time.Minute
	res := simulate(b, now, 10_000, queueFetch, 10*time.Millisecond, func(time.Time) outcome { return outcomeFailed }, 2*time.Hour, window)
	t.Logf("fetches per %s window: %v; sent %d", window, res.fetches, res.sent)
	limit := int(2 * window / openPace)
	for i, n := range res.fetches[2:] {
		if n > limit {
			t.Fatalf("window %d fetched the held deliveries %d times, want at most %d; per window: %v", i+2, n, limit, res.fetches)
		}
	}
	if res.sent > 2*breakerThreshold+int(2*time.Hour/breakerOpenFor) {
		t.Fatalf("%d deliveries were sent to the failing endpoint", res.sent)
	}
}

// A large backlog for an endpoint that answers, slowly, comes back as fast as the webhook's slots free and no faster: once the burst is queued the fetches in each window stay near the slots' capacity, however long the backlog, and the slots are kept busy.
func TestBreakerSlowEndpointBacklogIsFetchedAsSlotsFree(t *testing.T) {
	t.Parallel()
	b, now := testBreaker()
	const window = 2 * time.Minute
	const took = 4 * time.Second
	res := simulate(b, now, 10_000, queueFetch, took, func(time.Time) outcome { return outcomeAnswered }, 2*time.Hour, window)
	t.Logf("fetches per %s window: %v; answered %d", window, res.fetches, res.answered)
	capacity := int(window / took * time.Duration(b.slots))
	for i, n := range res.fetches[2:] {
		if n > 3*capacity {
			t.Fatalf("window %d fetched the waiting deliveries %d times for a capacity of %d; per window: %v", i+2, n, capacity, res.fetches)
		}
	}
	if want := int(2*time.Hour/took*time.Duration(b.slots)) * 9 / 10; res.answered < want {
		t.Fatalf("%d deliveries answered in 2 hours, want at least %d: the slots sat idle", res.answered, want)
	}
}
