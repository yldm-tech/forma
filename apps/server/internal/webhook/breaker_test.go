package webhook

import (
	"errors"
	"net/http"
	"net/http/httptest"
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
	b := newBreaker()
	b.now = func() time.Time { return now }
	b.random = func() float64 { return 0 }
	return b, &now
}

// Each webhook gets hookConcurrency deliveries at once; the next one waits a few seconds for a slot, and another webhook is not held back by it.
func TestBreakerCapsDeliveriesPerWebhook(t *testing.T) {
	t.Parallel()
	b, _ := testBreaker()
	slow, other := uuid.New(), uuid.New()
	var running []admission
	for range hookConcurrency {
		adm := b.enter(slow, false)
		if adm.Wait != 0 {
			t.Fatalf("a delivery within the cap waits %s", adm.Wait)
		}
		running = append(running, adm)
	}
	adm := b.enter(slow, false)
	if adm.Wait < timeout/2 || adm.Wait > maxBusyWait || adm.Open {
		t.Fatalf("a delivery over the cap = %+v, want a wait for a slot", adm)
	}
	if adm := b.enter(other, false); adm.Wait != 0 {
		t.Fatalf("another webhook's delivery waits %s", adm.Wait)
	}
	b.done(slow, running[0], outcomeAnswered)
	if adm := b.enter(slow, false); adm.Wait != 0 {
		t.Fatalf("a freed slot is not given out: %+v", adm)
	}
}

// The wait for a slot grows with the deliveries turned away, so a long backlog for one webhook is not fetched again every few seconds, and stays under maxBusyWait.
func TestBreakerBusyWaitGrowsWithTheBacklog(t *testing.T) {
	t.Parallel()
	b, _ := testBreaker()
	hook := uuid.New()
	for range hookConcurrency {
		b.enter(hook, false)
	}
	first := b.enter(hook, false).Wait
	var last time.Duration
	for range 200 {
		last = b.enter(hook, false).Wait
	}
	if last <= first || last != maxBusyWait/2 {
		t.Fatalf("waits %s then %s, want growth up to the cap (%s with no random part)", first, last, maxBusyWait/2)
	}
}

// breakerThreshold timeouts or 5xx answers in a row open the circuit; an answer in between starts the count again. While open every delivery waits until it ends; then one probe goes through, a failed probe opens it again, and an answered one closes it.
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
	if !adm.Open || adm.Wait != breakerOpenFor {
		t.Fatalf("a delivery to an open circuit = %+v, want a wait of %s", adm, breakerOpenFor)
	}
	*now = now.Add(breakerOpenFor / 2)
	if adm := b.enter(hook, false); !adm.Open || adm.Wait != breakerOpenFor/2 {
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
	if adm := b.enter(hook, false); !adm.Open || adm.Wait != breakerOpenFor {
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

// A probe that sends nothing (its webhook or response is gone) leaves the circuit open for the next delivery to probe.
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

// A delivery past its deadline goes through an open circuit, still within the webhook's slots, and leaves the circuit open when it fails.
func TestBreakerLetsDeliveriesPastTheirDeadlineThrough(t *testing.T) {
	t.Parallel()
	b, _ := testBreaker()
	hook := uuid.New()
	for range breakerThreshold {
		b.done(hook, b.enter(hook, false), outcomeFailed)
	}
	var running []admission
	for range hookConcurrency {
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

	for i := range hookConcurrency {
		a := args(slowHook, "slow-"+string(rune('a'+i)))
		running.Go(func() {
			if err := e.work(w, a, 1); err != nil {
				t.Errorf("a slow delivery failed: %v", err)
			}
		})
	}
	for range hookConcurrency {
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
	if n := slowRequests.Load(); n != hookConcurrency {
		t.Fatalf("the slow endpoint got %d requests, want %d: the snoozed delivery was sent", n, hookConcurrency)
	}
	if n := e.count(`SELECT count(*) FROM webhook_deliveries WHERE webhook_id = ?`, slowHook); n != hookConcurrency {
		t.Fatalf("%d deliveries recorded for the slow webhook, want %d: the snoozed one has no attempt", n, hookConcurrency)
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
}
