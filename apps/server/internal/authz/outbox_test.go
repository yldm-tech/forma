package authz

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// enqueue inserts an outbox row directly, created ageSeconds ago.
func (e *testEnv) enqueue(targetType string, revocation bool, ageSeconds int) uuid.UUID {
	e.t.Helper()
	var secondary *string
	if pairTargets[targetType] {
		s := uuid.NewString()
		secondary = &s
	}
	return e.insert(`
		INSERT INTO authz_projection_outbox (target_type, primary_id, secondary_id, is_revocation, created_at)
		VALUES (?, ?, ?, ?, now() - ? * interval '1 second') RETURNING id`, targetType, uuid.NewString(), secondary, revocation, ageSeconds)
}

func (e *testEnv) claim(owner string, limit int) []outboxEvent {
	e.t.Helper()
	events, err := claimOutbox(e.ctx, e.db, owner, limit)
	if err != nil {
		e.t.Fatalf("claim: %v", err)
	}
	return events
}

func TestClaimLeasesRevocationsFirstThenOldest(t *testing.T) {
	e := newDBEnv(t)
	oldGrant := e.enqueue(targetTeam, false, 30)
	newRevocation := e.enqueue(targetTeam, true, 1)
	oldRevocation := e.enqueue(targetUser, true, 20)
	newGrant := e.enqueue(targetWorkspace, false, 2)
	e.exec(`UPDATE authz_projection_outbox SET available_at = now() + interval '1 hour' WHERE id = ?`, newGrant)

	first := e.claim("a", 2)
	if len(first) != 2 || first[0].ID != oldRevocation || first[1].ID != newRevocation || first[0].Attempts != 1 {
		t.Fatalf("first claim = %+v", first)
	}
	// Leased rows are not claimed again, and a row that is not yet available is skipped.
	second := e.claim("b", 10)
	if len(second) != 1 || second[0].ID != oldGrant {
		t.Fatalf("second claim = %+v", second)
	}
	// An expired lease is claimable again.
	e.exec(`UPDATE authz_projection_outbox SET lease_expires_at = now() - interval '1 second' WHERE id = ?`, oldRevocation)
	third := e.claim("c", 10)
	if len(third) != 1 || third[0].ID != oldRevocation || third[0].Attempts != 2 {
		t.Fatalf("third claim = %+v", third)
	}
	var owner string
	var leaseSeconds float64
	if err := e.db.Raw(`SELECT lease_owner, EXTRACT(EPOCH FROM lease_expires_at - leased_at) FROM authz_projection_outbox WHERE id = ?`, oldRevocation).Row().Scan(&owner, &leaseSeconds); err != nil {
		t.Fatal(err)
	}
	if owner != "c" || leaseSeconds != 60 {
		t.Fatalf("lease = %s for %vs, want c for 60s", owner, leaseSeconds)
	}
}

func TestClaimDeadLettersAnEventThatNamesNothingDeliverable(t *testing.T) {
	e := newDBEnv(t)
	valid := e.enqueue(targetUser, false, 0)
	invalid := e.insert(`INSERT INTO authz_projection_outbox (target_type, primary_id, is_revocation) VALUES ('membership', 'not-a-uuid', true) RETURNING id`)

	events := e.claim("a", 10)
	if len(events) != 1 || events[0].ID != valid {
		t.Fatalf("claim = %+v", events)
	}
	if n := e.countOutbox(`id = ? AND dead_lettered_at IS NOT NULL AND last_error_code = 'authzed_projection_invalid_event' AND lease_owner IS NULL`, invalid); n != 1 {
		t.Fatal("invalid event was not dead-lettered")
	}
}

func TestMarkDeliveredOnlyCompletesTheActiveLease(t *testing.T) {
	e := newDBEnv(t)
	id := e.enqueue(targetTeam, true, 0)
	e.claim("a", 10)
	if err := markDelivered(e.ctx, e.db, "stale-owner", []uuid.UUID{id}); err != nil {
		t.Fatal(err)
	}
	if e.countOutbox(`processed_at IS NULL`) != 1 {
		t.Fatal("a lease that is no longer owned completed the event")
	}
	if err := markDelivered(e.ctx, e.db, "a", []uuid.UUID{id}); err != nil {
		t.Fatal(err)
	}
	if e.countOutbox(`processed_at IS NOT NULL AND lease_owner IS NULL AND last_error_code IS NULL`) != 1 {
		t.Fatal("event not delivered")
	}
}

// failRepeatedly claims and fails the event n times with the given verdict and returns the dead letters of the last release.
func (e *testEnv) failRepeatedly(id uuid.UUID, n int, attributable, retryable bool) (deadLettered int) {
	e.t.Helper()
	for range n {
		e.exec(`UPDATE authz_projection_outbox SET available_at = now() WHERE id = ?`, id)
		if len(e.claim("owner", 10)) == 0 {
			return deadLettered
		}
		var err error
		deadLettered, err = markFailed(e.ctx, e.db, "owner", []uuid.UUID{id}, CodeInvalidRequest, attributable, retryable)
		if err != nil {
			e.t.Fatal(err)
		}
	}
	return deadLettered
}

func TestMarkFailedDeadLettersOnlyAfterTenAttributableFailures(t *testing.T) {
	e := newDBEnv(t)
	id := e.enqueue(targetAPIKey, true, 0)
	if n := e.failRepeatedly(id, outboxMaxPermanentFailures-1, true, false); n != 0 {
		t.Fatalf("dead-lettered after %d failures", outboxMaxPermanentFailures-1)
	}
	if n := e.failRepeatedly(id, 1, true, false); n != 1 {
		t.Fatal("not dead-lettered after the tenth attributable failure")
	}
	if e.countOutbox(`id = ? AND dead_lettered_at IS NOT NULL AND permanent_failures = 10 AND attempts = 10`, id) != 1 {
		t.Fatal("dead letter state")
	}
	// A dead letter is never claimed, and a stray release cannot clear it.
	if got := e.claim("owner", 10); len(got) != 0 {
		t.Fatalf("claimed a dead letter: %+v", got)
	}
	if _, err := markFailed(e.ctx, e.db, "owner", []uuid.UUID{id}, CodeUnavailable, false, true); err != nil {
		t.Fatal(err)
	}
	if e.countOutbox(`id = ? AND dead_lettered_at IS NOT NULL`, id) != 1 {
		t.Fatal("a release cleared a dead letter")
	}
}

func TestMarkFailedNeverDeadLettersRetryableOrUnattributedFailures(t *testing.T) {
	e := newDBEnv(t)
	retryable := e.enqueue(targetTeam, true, 0)
	e.failRepeatedly(retryable, 25, true, true)
	group := e.enqueue(targetWorkspace, true, 0)
	e.exec(`UPDATE authz_projection_outbox SET available_at = now() + interval '1 hour' WHERE id = ?`, retryable)
	e.failRepeatedly(group, 25, false, false)
	if e.countOutbox(`dead_lettered_at IS NOT NULL OR permanent_failures > 0`) != 0 {
		t.Fatal("a retryable or unattributed failure was charged")
	}
	if e.countOutbox(`id = ? AND attempts = 25 AND last_error_code = 'authzed_invalid_request'`, retryable) != 1 {
		t.Fatal("attempts not counted")
	}
}

func TestMarkFailedBacksOffExponentiallyUpToFiveMinutes(t *testing.T) {
	e := newDBEnv(t)
	cases := []struct {
		attempts int
		want     float64
	}{{1, 1}, {2, 2}, {5, 16}, {9, 256}, {10, 300}, {40, 300}}
	for _, tc := range cases {
		id := e.enqueue(targetTeam, false, 0)
		e.claim("owner", 10)
		e.exec(`UPDATE authz_projection_outbox SET attempts = ? WHERE id = ?`, tc.attempts, id)
		if _, err := markFailed(e.ctx, e.db, "owner", []uuid.UUID{id}, CodeUnavailable, false, true); err != nil {
			t.Fatal(err)
		}
		var delay float64
		if err := e.db.Raw(`SELECT EXTRACT(EPOCH FROM available_at - updated_at) FROM authz_projection_outbox WHERE id = ?`, id).Scan(&delay).Error; err != nil {
			t.Fatal(err)
		}
		if delay != tc.want {
			t.Errorf("attempt %d backs off %vs, want %vs", tc.attempts, delay, tc.want)
		}
	}
}

func TestMarkFailedLeavesAnEventReleasedByAnotherLeaseUntouched(t *testing.T) {
	e := newDBEnv(t)
	id := e.enqueue(targetTeam, true, 0)
	e.claim("current", 10)
	if _, err := markFailed(e.ctx, e.db, "previous", []uuid.UUID{id}, CodeInvalidRequest, true, false); err != nil {
		t.Fatal(err)
	}
	if e.countOutbox(`id = ? AND lease_owner = 'current' AND permanent_failures = 0 AND last_error_code IS NULL`, id) != 1 {
		t.Fatal("a release by a lease that no longer owns the event changed it")
	}
}

func TestOutboxStatusReplayAndPrune(t *testing.T) {
	e := newDBEnv(t)
	e.enqueue(targetTeam, false, 120)
	e.enqueue(targetTeam, true, 20)
	e.enqueue(targetTeam, true, 50)
	e.enqueue(targetTeam, true, 70)
	dead := e.enqueue(targetUser, true, 1)
	e.exec(`UPDATE authz_projection_outbox SET dead_lettered_at = now(), attempts = 12, permanent_failures = 10, last_error_code = 'authzed_invalid_request' WHERE id = ?`, dead)
	old := e.enqueue(targetTeam, false, 0)
	recent := e.enqueue(targetTeam, false, 0)
	e.exec(`UPDATE authz_projection_outbox SET processed_at = now() - interval '8 days' WHERE id = ?`, old)
	e.exec(`UPDATE authz_projection_outbox SET processed_at = now() - interval '6 days' WHERE id = ?`, recent)

	s, err := outboxStatus(e.ctx, e.db)
	if err != nil {
		t.Fatal(err)
	}
	// Every revocation count includes the dead letter.
	if s.Pending != 4 || s.DeadLettered != 1 || s.OverdueRevocations != 2 || s.RevocationsPastCritical != 3 || s.RevocationsPastWarning != 4 || s.OldestPendingAgeSeconds == nil || *s.OldestPendingAgeSeconds < 120 {
		t.Fatalf("status = %+v", s)
	}

	pruned, err := pruneOutboxHistory(e.ctx, e.db)
	if err != nil || pruned != 1 || e.countOutbox(`id = ?`, old) != 0 || e.countOutbox(`id = ?`, recent) != 1 {
		t.Fatalf("pruned %d, %v", pruned, err)
	}

	replayed, err := replayDeadLetters(e.ctx, e.db)
	if err != nil || replayed != 1 {
		t.Fatalf("replayed %d, %v", replayed, err)
	}
	if e.countOutbox(`id = ? AND dead_lettered_at IS NULL AND attempts = 0 AND permanent_failures = 0 AND last_error_code IS NULL AND available_at <= now()`, dead) != 1 {
		t.Fatal("replay did not reset the dead letter")
	}

	empty := newDBEnv(t)
	if s, err := outboxStatus(empty.ctx, empty.db); err != nil || s.Pending != 0 || s.OldestPendingAgeSeconds != nil {
		t.Fatalf("empty status = %+v, %v", s, err)
	}
}

func TestFreshnessGuard(t *testing.T) {
	cases := []struct {
		name  string
		setup func(e *testEnv)
		stale bool
	}{
		{"empty outbox", func(*testEnv) {}, false},
		{"recent revocation", func(e *testEnv) { e.enqueue(targetMembership, true, 30) }, false},
		{"grant pending far past the window", func(e *testEnv) { e.enqueue(targetMembership, false, 3600) }, false},
		{"delivered overdue revocation", func(e *testEnv) {
			id := e.enqueue(targetMembership, true, 3600)
			e.exec(`UPDATE authz_projection_outbox SET processed_at = now() WHERE id = ?`, id)
		}, false},
		{"overdue revocation", func(e *testEnv) { e.enqueue(targetMembership, true, 61) }, true},
		{"fresh dead-lettered revocation", func(e *testEnv) {
			id := e.enqueue(targetUser, true, 0)
			e.exec(`UPDATE authz_projection_outbox SET dead_lettered_at = now() WHERE id = ?`, id)
		}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newDBEnv(t)
			tc.setup(e)
			err := newFreshnessGuard(e.db).assert(e.ctx)
			if got, ok := AsError(err); tc.stale != (ok && got.Code == CodeProjectionStale && !got.Retryable && got.Attempts == 0) || (!tc.stale && err != nil) {
				t.Fatalf("assert = %v, want stale %v", err, tc.stale)
			}
		})
	}
}

func TestFreshnessGuardMemoizesForOneSecond(t *testing.T) {
	e := newDBEnv(t)
	clock := newFakeClock()
	g := newFreshnessGuard(e.db)
	g.now = clock.now
	if err := g.assert(e.ctx); err != nil {
		t.Fatal(err)
	}
	e.enqueue(targetMembership, true, 120)
	clock.advance(freshnessMemoTTL - time.Millisecond)
	if err := g.assert(e.ctx); err != nil {
		t.Fatalf("inside the memo window: %v", err)
	}
	clock.advance(time.Millisecond)
	if err := g.assert(e.ctx); err == nil {
		t.Fatal("the memo outlived its window")
	}
}

func TestFreshnessGuardNeverMemoizesAFailedRead(t *testing.T) {
	e := newDBEnv(t)
	g := newFreshnessGuard(e.db)
	ctx, cancel := context.WithCancel(e.ctx)
	cancel()
	if err := g.assert(ctx); err == nil {
		t.Fatal("a failed read answered")
	}
	if !g.at.IsZero() {
		t.Fatal("a failed read was memoized")
	}
	e.enqueue(targetUser, true, 120)
	if err := g.assert(e.ctx); err == nil {
		t.Fatal("the next read did not ask again")
	}
}
