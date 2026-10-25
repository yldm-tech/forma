package authz

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/jobs"
)

func TestAnOverdueRevocationStopsEveryCheckUntilItIsDelivered(t *testing.T) {
	e := newTestEnv(t)
	org, owner, removed := e.org(), e.user(), e.user()
	e.member(org, owner, "owner")
	e.member(org, removed, "manager")
	e.drain()

	// The removal is committed but not delivered, and has waited past the window.
	e.exec(`DELETE FROM memberships WHERE organization_id = ? AND user_id = ?`, org, removed)
	e.exec(`UPDATE authz_projection_outbox SET created_at = now() - interval '61 seconds' WHERE processed_at IS NULL`)
	e.authorizer.fresh = newFreshnessGuard(e.db)
	for _, actor := range []Actor{User(owner), User(removed)} {
		_, err := e.authorizer.Can(e.ctx, actor, OrganizationRead, Organization(org))
		if ae, ok := AsError(err); !ok || ae.Code != CodeProjectionStale || ae.Retryable || ae.Operation != "authorization" {
			t.Fatalf("Can = %v, want authzed_projection_stale", err)
		}
	}
	if _, err := e.authorizer.LookupWorkspaceIDs(e.ctx, User(owner), false); err == nil {
		t.Fatal("a lookup answered from a stale graph")
	}

	e.drain()
	e.authorizer.fresh = newFreshnessGuard(e.db)
	e.expect(User(owner), Organization(org), map[Action]bool{OrganizationRead: true})
	e.expect(User(removed), Organization(org), map[Action]bool{OrganizationRead: false})
}

func TestAnInvalidRevocationIsDeadLetteredAndFailsClosedUntilReplayed(t *testing.T) {
	e := newTestEnv(t)
	org, user := e.org(), e.user()
	e.member(org, user, "owner")
	e.drain()

	e.exec(`INSERT INTO authz_projection_outbox (target_type, primary_id, is_revocation) VALUES ('user', 'not-a-uuid', true)`)
	res, err := e.delivery.Drain(e.ctx, DefaultDrainBatches)
	if err != nil || res.Status != "drained" {
		t.Fatalf("Drain = %+v, %v", res, err)
	}
	if e.countOutbox(`dead_lettered_at IS NOT NULL AND last_error_code = 'authzed_projection_invalid_event'`) != 1 {
		t.Fatal("invalid event not dead-lettered")
	}
	e.authorizer.fresh = newFreshnessGuard(e.db)
	if _, err := e.authorizer.Can(e.ctx, User(user), OrganizationRead, Organization(org)); err == nil {
		t.Fatal("a dead-lettered revocation did not fail the check closed")
	}
	// A clean audit hands the dead letter back; it dead-letters again, so the guard stays armed.
	if err := NewAudit(e.db, e.client, discardLog()).Run(e.ctx); err != nil {
		t.Fatal(err)
	}
	if e.countOutbox(`dead_lettered_at IS NULL AND attempts = 0 AND processed_at IS NULL`) != 1 {
		t.Fatal("clean audit did not replay the dead letter")
	}
	e.exec(`DELETE FROM authz_projection_outbox WHERE primary_id = 'not-a-uuid'`)
	e.authorizer.fresh = newFreshnessGuard(e.db)
	e.expect(User(user), Organization(org), map[Action]bool{OrganizationRead: true})
}

func TestUnreachableSpiceDBReleasesTheBatchWithoutBlame(t *testing.T) {
	e := newTestEnv(t)
	down, err := NewClient(config.AuthzConfig{Enabled: true, Endpoint: "127.0.0.1:1", Token: "x", Insecure: true, Consistency: "fully_consistent"}, RequestProfile, discardLog())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = down.Close() })
	org, user := e.org(), e.user()
	e.member(org, user, "owner")
	e.team(org)
	e.apiKey(org, true, false)

	res, err := NewDelivery(e.db, down, discardLog()).Drain(e.ctx, DefaultDrainBatches)
	if err != nil {
		t.Fatal(err)
	}
	// The first group's transient failure halts the tick: one claim, nothing delivered, everything released for a later retry.
	if res.Claimed != 5 || res.Delivered != 0 || res.Failed != 5 || res.DeadLettered != 0 || res.Status != "partial" {
		t.Fatalf("Drain = %+v", res)
	}
	if e.countOutbox(`permanent_failures = 0 AND dead_lettered_at IS NULL AND last_error_code = 'authzed_unavailable' AND available_at > now() AND lease_owner IS NULL`) != 5 {
		t.Fatal("failed events were not released with a backoff")
	}
}

func TestDeliverAPIKeyProjectsThatKeyBeforeTheWorker(t *testing.T) {
	e := newTestEnv(t)
	org := e.org()
	ws := e.workspace(org)
	e.drain()
	key, other := e.apiKey(org, true, false), e.apiKey(org, true, false)
	e.apiKeyWorkspace(key, ws, "read")
	e.apiKeyWorkspace(other, ws, "read")

	e.delivery.DeliverAPIKey(e.ctx, key)
	e.expect(APIKey(key), Workspace(ws), map[Action]bool{WorkspaceRead: true})
	e.expect(APIKey(key), Organization(org), map[Action]bool{OrganizationReadAccess: true})
	if n := e.countOutbox(`primary_id = ? AND processed_at IS NULL`, key.String()); n != 0 {
		t.Fatalf("%d of the key's events are still pending", n)
	}
	// Only the named aggregate is delivered; the other key waits for the worker as before.
	if n := e.countOutbox(`primary_id = ? AND processed_at IS NULL AND lease_owner IS NULL AND attempts = 0`, other.String()); n != 2 {
		t.Fatalf("the other key's events were touched: %d still untouched, want 2", n)
	}
	e.expect(APIKey(other), Workspace(ws), map[Action]bool{WorkspaceRead: false})
	// Nil is AuthZed disabled: nothing to project, nothing to fail.
	(*Delivery)(nil).DeliverAPIKey(e.ctx, other)
}

func TestDeliverAPIKeyFallsBackToTheOutboxWhenSpiceDBIsDown(t *testing.T) {
	e := newTestEnv(t)
	down, err := NewClient(config.AuthzConfig{Enabled: true, Endpoint: "127.0.0.1:1", Token: "x", Insecure: true, Consistency: "fully_consistent"}, RequestProfile, discardLog())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = down.Close() })
	org := e.org()
	ws := e.workspace(org)
	e.drain()
	key := e.apiKey(org, false, false)
	e.apiKeyWorkspace(key, ws, "read")

	NewDelivery(e.db, down, discardLog()).DeliverAPIKey(e.ctx, key)
	// Released exactly as a failed worker batch is: unblamed, unleased, backing off.
	if n := e.countOutbox(`primary_id = ? AND processed_at IS NULL AND lease_owner IS NULL AND permanent_failures = 0 AND last_error_code = 'authzed_unavailable' AND available_at > now()`, key.String()); n != 2 {
		t.Fatalf("%d of the key's 2 events were released to the outbox", n)
	}
	e.exec(`UPDATE authz_projection_outbox SET available_at = now() WHERE primary_id = ?`, key.String())
	e.drain()
	e.expect(APIKey(key), Workspace(ws), map[Action]bool{WorkspaceRead: true})
}

func TestDeliverAPIKeyReleasesTheKeyWhenSpiceDBStalls(t *testing.T) {
	e := newTestEnv(t)
	// A SpiceDB that accepts the connection and never answers: every call runs into its deadline.
	stalled, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var conns []net.Conn
	go func() {
		for {
			conn, err := stalled.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, conn)
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		_ = stalled.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, conn := range conns {
			_ = conn.Close()
		}
	})
	client, err := NewClient(config.AuthzConfig{Enabled: true, Endpoint: stalled.Addr().String(), Token: "x", Insecure: true, Consistency: "fully_consistent"}, RequestProfile, discardLog())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	org := e.org()
	ws := e.workspace(org)
	e.drain()
	key := e.apiKey(org, false, false)
	e.apiKeyWorkspace(key, ws, "read")

	started := time.Now()
	NewDelivery(e.db, client, discardLog()).DeliverAPIKey(e.ctx, key)
	if waited := time.Since(started); waited > syncDeliveryTimeout+syncSettleTimeout {
		t.Fatalf("Create would have waited %s on a stalled SpiceDB", waited)
	}
	// Released as a timed-out worker batch is: unblamed, unleased, backing off.
	if n := e.countOutbox(`primary_id = ? AND processed_at IS NULL AND lease_owner IS NULL AND permanent_failures = 0 AND last_error_code = 'authzed_timeout' AND available_at > now()`, key.String()); n != 2 {
		t.Fatalf("%d of the key's 2 events were released to the outbox", n)
	}
	e.exec(`UPDATE authz_projection_outbox SET available_at = now() WHERE primary_id = ?`, key.String())
	e.drain()
	e.expect(APIKey(key), Workspace(ws), map[Action]bool{WorkspaceRead: true})
}

func TestDeliverAPIKeyDoesNotWaitOnABlockedClaim(t *testing.T) {
	e := newTestEnv(t)
	org := e.org()
	ws := e.workspace(org)
	e.drain()
	key := e.apiKey(org, false, false)
	e.apiKeyWorkspace(key, ws, "read")

	// Another transaction holds the outbox, as a claim over a long backlog would hold the request: the claim cannot proceed until it ends.
	lock := e.db.Begin()
	if err := lock.Exec(`LOCK TABLE authz_projection_outbox IN EXCLUSIVE MODE`).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lock.Rollback() })
	done := make(chan time.Duration, 1)
	started := time.Now()
	go func() {
		e.delivery.DeliverAPIKey(e.ctx, key)
		done <- time.Since(started)
	}()
	select {
	case waited := <-done:
		if waited > syncDeliveryTimeout+syncSettleTimeout {
			t.Fatalf("Create waited %s on a blocked claim", waited)
		}
	case <-time.After(3 * (syncDeliveryTimeout + syncSettleTimeout)):
		lock.Rollback()
		<-done
		t.Fatal("Create waited for the blocked claim instead of giving up at its deadline")
	}
	lock.Rollback()

	// The cancelled claim leased nothing, so the worker delivers the key on its next tick.
	if n := e.countOutbox(`primary_id = ? AND processed_at IS NULL AND lease_owner IS NULL AND attempts = 0`, key.String()); n != 2 {
		t.Fatalf("%d of the key's 2 events are left untouched for the worker", n)
	}
	e.drain()
	e.expect(APIKey(key), Workspace(ws), map[Action]bool{WorkspaceRead: true})
}

func TestDeliveryJobRunsOnTheWorker(t *testing.T) {
	e := newTestEnv(t)
	org, user := e.org(), e.user()
	e.member(org, user, "owner")

	cfg := config.Config{DatabaseURL: e.dsn, DatabaseMaxOpenConns: 4, WorkerConcurrency: 2, ShutdownTimeout: time.Second}
	pool, closePool, err := jobs.OpenPool(e.ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer closePool()
	reg := jobs.NewRegistry(discardLog())
	RegisterJobs(reg, NewDelivery(e.db, e.client, discardLog()), NewAudit(e.db, e.client, discardLog()))
	worker, err := jobs.NewWorker(cfg, pool, reg, discardLog())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(e.ctx)
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()
	defer func() {
		cancel()
		<-done
	}()

	deadline := time.Now().Add(20 * time.Second)
	for e.countOutbox(`processed_at IS NULL`) > 0 {
		if time.Now().After(deadline) {
			t.Fatal("the periodic delivery job did not drain the outbox")
		}
		time.Sleep(100 * time.Millisecond)
	}
	e.expect(User(user), Organization(org), map[Action]bool{OrganizationWrite: true})
}
