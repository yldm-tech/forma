package authz

import (
	"context"
	"sync"
	"time"

	"gorm.io/gorm"
)

// freshnessMemoTTL is how long one staleness answer is reused across checks; the guard can arm up to this much later than the window it enforces.
const freshnessMemoTTL = time.Second

// freshnessGuard refuses to authorize from a graph that may still hold access PostgreSQL revoked: once any revocation has waited past the 60-second window, or dead-lettered, every check fails with authzed_projection_stale until it is delivered. The guard is global, not per tenant, and fails closed.
//
// The answer is memoized process-wide for a second, measured on the monotonic clock so a wall-clock step cannot freeze it. Concurrent checks share one query; a failed read is never memoized and keeps failing.
type freshnessGuard struct {
	db  *gorm.DB
	now func() time.Time

	mu       sync.Mutex
	at       time.Time
	stale    bool
	inflight *freshnessRead
}

type freshnessRead struct {
	done  chan struct{}
	stale bool
	err   error
}

func newFreshnessGuard(db *gorm.DB) *freshnessGuard {
	return &freshnessGuard{db: db, now: time.Now}
}

func (g *freshnessGuard) read(ctx context.Context) (bool, error) {
	g.mu.Lock()
	if !g.at.IsZero() && g.now().Sub(g.at) < freshnessMemoTTL {
		stale := g.stale
		g.mu.Unlock()
		return stale, nil
	}
	if call := g.inflight; call != nil {
		g.mu.Unlock()
		select {
		case <-call.done:
			return call.stale, call.err
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}
	call := &freshnessRead{done: make(chan struct{})}
	g.inflight = call
	g.mu.Unlock()

	call.stale, call.err = hasStaleRevocation(ctx, g.db)

	g.mu.Lock()
	if call.err == nil {
		g.stale, g.at = call.stale, g.now()
	}
	g.inflight = nil
	g.mu.Unlock()
	close(call.done)
	return call.stale, call.err
}

// assert returns nil when the projection is fresh, authzed_projection_stale when it is not, or the read's failure.
func (g *freshnessGuard) assert(ctx context.Context) error {
	stale, err := g.read(ctx)
	if err != nil {
		return err
	}
	if stale {
		return newError(CodeProjectionStale, "authorization_projection_freshness", false, 0)
	}
	return nil
}
