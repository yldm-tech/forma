package ratelimit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/server/internal/platform/valkey/valkeytest"
)

var discard = slog.New(slog.NewTextHandler(io.Discard, nil))

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	os.Exit(m.Run())
}

// windowBase is aligned to the epoch on every window length the tests use; fixedNow is 10 seconds into all of them.
const windowBase = 1_699_999_200

var fixedNow = time.Unix(windowBase+10, 0)

func newLimiter(rdb redis.UniversalClient, now time.Time) *Limiter {
	return &Limiter{rdb: rdb, log: discard, now: func() time.Time { return now }}
}

var perMinute = Rule{Namespace: "test:minute", Window: time.Minute, Limit: 3}

func TestCheckRejectsInvalidUsage(t *testing.T) {
	l := newLimiter(nil, fixedNow)
	for _, n := range []int{0, -1} {
		if _, err := l.Check(context.Background(), perMinute, "id", n); !errors.Is(err, ErrInvalidUsage) {
			t.Errorf("Check(requested=%d) err = %v, want ErrInvalidUsage", n, err)
		}
	}
}

func TestDisabledAllowsWithoutTouchingValkey(t *testing.T) {
	// A nil client would panic if the limiter reached for it.
	l := &Limiter{disabled: true, log: discard, now: time.Now}
	res, err := l.Check(context.Background(), perMinute, "id", 1)
	if err != nil || !res.Allowed {
		t.Fatalf("Check = %+v, %v; want allowed", res, err)
	}
	if res, err = l.Peek(context.Background(), perMinute, "id"); err != nil || !res.Allowed {
		t.Fatalf("Peek = %+v, %v; want allowed", res, err)
	}
	if err := New(config.Config{RateLimitingDisabled: true}, nil, discard).Apply(context.Background(), perMinute, "id"); err != nil {
		t.Fatalf("Apply with RATE_LIMITING_DISABLED: %v", err)
	}
}

func TestFailsOpenWhenValkeyIsUnreachable(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", DialTimeout: 200 * time.Millisecond, MaxRetries: -1})
	t.Cleanup(func() { _ = rdb.Close() })
	l := newLimiter(rdb, fixedNow)
	if res, err := l.Check(context.Background(), perMinute, "id", 1); err != nil || !res.Allowed {
		t.Fatalf("Check = %+v, %v; want allowed", res, err)
	}
	if res, err := l.Peek(context.Background(), perMinute, "id"); err != nil || !res.Allowed {
		t.Fatalf("Peek = %+v, %v; want allowed", res, err)
	}
}

func TestInvalidRuleIsAnError(t *testing.T) {
	l := newLimiter(nil, fixedNow)
	for _, r := range []Rule{
		{Namespace: "", Window: time.Minute, Limit: 1},
		{Namespace: "x", Window: 0, Limit: 1},
		{Namespace: "x", Window: 1500 * time.Millisecond, Limit: 1},
		{Namespace: "x", Window: time.Minute, Limit: 0},
	} {
		if _, err := l.Check(context.Background(), r, "id", 1); err == nil {
			t.Errorf("Check accepted rule %+v", r)
		}
	}
}

func TestWindow(t *testing.T) {
	cases := []struct {
		name    string
		rule    Rule
		now     time.Time
		wantKey string
		wantTTL int64
	}{
		{"aligned to the epoch", perMinute, fixedNow, "fb:rate_limit:test:minute:id:1699999200", 50},
		{"namespace with colons", Rule{Namespace: "auth:login", Window: 15 * time.Minute, Limit: 10}, fixedNow, "fb:rate_limit:auth:login:id:1699999200", 890},
		{"rounds a partial second up", perMinute, time.Unix(windowBase+9, 500_000_000), "fb:rate_limit:test:minute:id:1699999200", 51},
		{"at least one second", perMinute, time.Unix(windowBase+59, 999_000_000), "fb:rate_limit:test:minute:id:1699999200", 1},
		{"first instant of a window", perMinute, time.Unix(windowBase+60, 0), "fb:rate_limit:test:minute:id:1699999260", 60},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, err := newLimiter(nil, tc.now).window(tc.rule, "id")
			if err != nil {
				t.Fatal(err)
			}
			if w.key != tc.wantKey || w.ttlSeconds != tc.wantTTL {
				t.Fatalf("window = %q ttl %d, want %q ttl %d", w.key, w.ttlSeconds, tc.wantKey, tc.wantTTL)
			}
		})
	}
}

func TestCacheKeyRejectsEmptySegments(t *testing.T) {
	for _, id := range []string{"", "a::b", ":a", "a:"} {
		if _, err := cacheKey("rate_limit", "ns", id, "1"); err == nil {
			t.Errorf("cacheKey accepted identifier %q", id)
		}
	}
	if k, err := cacheKey("rate_limit", "api:client", "env:hash", "1"); err != nil || k != "fb:rate_limit:api:client:env:hash:1" {
		t.Fatalf("cacheKey = %q, %v", k, err)
	}
}

// An identifier that cannot form a key fails open like any other limiter fault, as in forma2.
func TestInvalidIdentifierFailsOpen(t *testing.T) {
	if res, err := newLimiter(nil, fixedNow).Check(context.Background(), perMinute, "", 1); err != nil || !res.Allowed {
		t.Fatalf("Check = %+v, %v; want allowed", res, err)
	}
}

func TestIPIdentifierHashesTheClientAddress(t *testing.T) {
	// sha256("untrusted-client-ip"): every unresolved client shares this bucket.
	if got := IPIdentifier(context.Background()); got != "70a0442977df5f42f62519e4f88d18a97228ae0aec0a962b4fd4b9bf770952cc" {
		t.Fatalf("IPIdentifier without a resolved address = %q, want sha256(%q)", got, httpapi.UntrustedClientIP)
	}
	a := identifierFor(t, "203.0.113.7")
	b := identifierFor(t, "203.0.113.8")
	if a == b || len(a) != 64 || strings.Contains(a, "203.0.113.7") {
		t.Fatalf("identifiers %q / %q, want distinct hex digests that do not contain the address", a, b)
	}
}

func identifierFor(t *testing.T, xff string) string {
	t.Helper()
	resolver := httpapi.NewClientIPResolver(config.Config{TrustedProxyHopCount: 1}, discard)
	var got string
	r := gin.New()
	r.Use(resolver.Middleware())
	r.GET("/", func(c *gin.Context) { got = IPIdentifier(c.Request.Context()) })
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-For", xff)
	r.ServeHTTP(httptest.NewRecorder(), req)
	return got
}

// --- against a real Valkey ---

func TestCheckCountsUpToTheLimit(t *testing.T) {
	rdb := valkeytest.Client(t)
	l := newLimiter(rdb, fixedNow)
	ctx := context.Background()

	for i := 1; i <= perMinute.Limit; i++ {
		res, err := l.Check(ctx, perMinute, "user-1", 1)
		if err != nil || !res.Allowed {
			t.Fatalf("request %d: %+v, %v; want allowed", i, res, err)
		}
	}
	res, err := l.Check(ctx, perMinute, "user-1", 1)
	if err != nil || res.Allowed || res.RetryAfter != 50 {
		t.Fatalf("over the limit: %+v, %v; want denied with RetryAfter 50", res, err)
	}
	// Another identifier has its own budget.
	if res, _ := l.Check(ctx, perMinute, "user-2", 1); !res.Allowed {
		t.Fatal("a different identifier was denied")
	}

	key := "fb:rate_limit:test:minute:user-1:1699999200"
	if n, err := rdb.Get(ctx, key).Int(); err != nil || n != perMinute.Limit {
		t.Fatalf("counter %s = %d, %v; want %d (a denied request is not counted)", key, n, err, perMinute.Limit)
	}
	if ttl := rdb.TTL(ctx, key).Val(); ttl <= 0 || ttl > 50*time.Second {
		t.Fatalf("TTL = %v, want (0, 50s]", ttl)
	}
}

func TestOversizedWeightedRequestConsumesNothing(t *testing.T) {
	rdb := valkeytest.Client(t)
	ctx := context.Background()
	rule := Rule{Namespace: "test:weighted", Window: time.Hour, Limit: 5}
	key := "fb:rate_limit:test:weighted:org:1699999200"

	l := newLimiter(rdb, fixedNow)
	if res, _ := l.Check(ctx, rule, "org", 3); !res.Allowed {
		t.Fatal("first weighted request denied")
	}
	firstTTL := rdb.TTL(ctx, key).Val()

	// A later request in the same window must not extend it.
	l.now = func() time.Time { return fixedNow.Add(5 * time.Second) }
	res, err := l.Check(ctx, rule, "org", 3)
	if err != nil || res.Allowed || res.RetryAfter != 3585 {
		t.Fatalf("oversized request: %+v, %v; want denied with RetryAfter 3585", res, err)
	}
	if n := rdb.Get(ctx, key).Val(); n != "3" {
		t.Fatalf("counter = %s after a denied weighted request, want 3", n)
	}
	if res, _ := l.Check(ctx, rule, "org", 2); !res.Allowed {
		t.Fatal("the remaining budget of 2 was not available")
	}
	if ttl := rdb.TTL(ctx, key).Val(); ttl > firstTTL {
		t.Fatalf("TTL grew from %v to %v; the window was extended", firstTTL, ttl)
	}
}

func TestNewWindowStartsFresh(t *testing.T) {
	rdb := valkeytest.Client(t)
	ctx := context.Background()
	l := newLimiter(rdb, fixedNow)
	for range perMinute.Limit {
		_, _ = l.Check(ctx, perMinute, "id", 1)
	}
	if res, _ := l.Check(ctx, perMinute, "id", 1); res.Allowed {
		t.Fatal("limit not reached")
	}
	l.now = func() time.Time { return fixedNow.Add(50 * time.Second) }
	if res, _ := l.Check(ctx, perMinute, "id", 1); !res.Allowed {
		t.Fatal("the next window inherited the previous count")
	}
}

func TestConcurrentRequestsNeverExceedTheLimit(t *testing.T) {
	rdb := valkeytest.Client(t)
	rule := Rule{Namespace: "test:race", Window: time.Hour, Limit: 25}
	l := newLimiter(rdb, fixedNow)

	var allowed atomic.Int32
	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() {
			if res, err := l.Check(context.Background(), rule, "shared", 1); err == nil && res.Allowed {
				allowed.Add(1)
			}
		})
	}
	wg.Wait()
	if got := allowed.Load(); got != int32(rule.Limit) {
		t.Fatalf("%d of 100 concurrent requests allowed, want exactly %d", got, rule.Limit)
	}
}

func TestCounterExpires(t *testing.T) {
	rdb := valkeytest.Client(t)
	ctx := context.Background()
	rule := Rule{Namespace: "test:expiry", Window: time.Second, Limit: 1}
	l := &Limiter{rdb: rdb, log: discard, now: time.Now}
	if res, _ := l.Check(ctx, rule, "id", 1); !res.Allowed {
		t.Fatal("first request denied")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		keys, err := rdb.Keys(ctx, "fb:rate_limit:test:expiry:*").Result()
		if err != nil {
			t.Fatal(err)
		}
		if len(keys) == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("counters %v never expired", keys)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func TestPeekDoesNotConsume(t *testing.T) {
	rdb := valkeytest.Client(t)
	ctx := context.Background()
	l := newLimiter(rdb, fixedNow)

	for range 5 {
		if res, err := l.Peek(ctx, perMinute, "id"); err != nil || !res.Allowed {
			t.Fatalf("Peek on an empty window = %+v, %v", res, err)
		}
	}
	if n, _ := rdb.Exists(ctx, "fb:rate_limit:test:minute:id:1699999200").Result(); n != 0 {
		t.Fatal("Peek created the counter")
	}
	if err := l.ApplyN(ctx, perMinute, "id", perMinute.Limit); err != nil {
		t.Fatalf("ApplyN: %v", err)
	}
	res, err := l.Peek(ctx, perMinute, "id")
	if err != nil || res.Allowed || res.RetryAfter != 50 {
		t.Fatalf("Peek on a spent window = %+v, %v; want denied with RetryAfter 50", res, err)
	}
	e, ok := apperr.As(l.AssertAvailable(ctx, perMinute, "id"))
	if !ok || e.Status != http.StatusTooManyRequests || e.RetryAfter != 50 || e.Message != apperr.TooManyRequestsMessage {
		t.Fatalf("AssertAvailable = %+v, want 429 with RetryAfter 50", e)
	}
}

func TestMiddleware(t *testing.T) {
	rdb := valkeytest.Client(t)
	l := newLimiter(rdb, fixedNow)
	rule := Rule{Namespace: "test:middleware", Window: time.Minute, Limit: 1}

	r := gin.New()
	r.Use(httpapi.NewClientIPResolver(config.Config{TrustedProxyHopCount: 1}, discard).Middleware(), httpapi.ErrorHandler(discard))
	r.POST("/ip", Middleware(l, rule, ByClientIP), func(c *gin.Context) { c.Status(http.StatusNoContent) })
	r.POST("/user", Middleware(l, rule, ByUser), func(c *gin.Context) { c.Status(http.StatusNoContent) })

	send := func(path, xff string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(nil))
		req.Header.Set("X-Forwarded-For", xff)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	if rec := send("/ip", "198.51.100.1"); rec.Code != http.StatusNoContent {
		t.Fatalf("first request: %d", rec.Code)
	}
	rec := send("/ip", "198.51.100.1")
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") != "50" {
		t.Fatalf("second request: %d Retry-After %q, want 429 with Retry-After 50", rec.Code, rec.Header().Get("Retry-After"))
	}
	var body httpapi.Envelope
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != apperr.CodeTooManyRequests || body.Message != apperr.TooManyRequestsMessage || body.Details["retryAfter"] != float64(50) {
		t.Fatalf("body = %+v", body)
	}
	if rec := send("/ip", "198.51.100.2"); rec.Code != http.StatusNoContent {
		t.Fatalf("another client was limited: %d", rec.Code)
	}
	if rec := send("/user", "198.51.100.3"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("ByUser without a session: %d, want 401", rec.Code)
	}
}

func TestWithClockReadsTheGivenTimeAndLeavesTheOriginalAlone(t *testing.T) {
	original := newLimiter(nil, fixedNow)
	later := original.WithClock(func() time.Time { return time.Unix(windowBase+60, 0) })

	w, err := later.window(perMinute, "id")
	if err != nil {
		t.Fatal(err)
	}
	if w.key != "fb:rate_limit:test:minute:id:1699999260" {
		t.Fatalf("copy uses window %q, want the one its clock is in", w.key)
	}
	w, err = original.window(perMinute, "id")
	if err != nil {
		t.Fatal(err)
	}
	if w.key != "fb:rate_limit:test:minute:id:1699999200" {
		t.Fatalf("original moved to window %q; WithClock must not mutate it", w.key)
	}
}
