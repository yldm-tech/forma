// Package ratelimit implements forma2's fixed-window rate limiter on Valkey: the same window alignment, key scheme, atomic weighted increment and fail-open policy (apps/web/modules/core/rate-limit).
package ratelimit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/platform/httpapi"
)

// Rule is one limit: at most Limit units per Window for each identifier, counted in Namespace. Window must be a whole number of seconds; windows are aligned to the Unix epoch, so every replica agrees on where one ends.
type Rule struct {
	Namespace string
	Window    time.Duration
	Limit     int
}

func (r Rule) validate() error {
	if r.Namespace == "" || r.Limit < 1 || r.Window < time.Second || r.Window%time.Second != 0 {
		return fmt.Errorf("invalid rate limit rule %+v", r)
	}
	return nil
}

// Result is the outcome of a check. RetryAfter is set only when the request is denied: the seconds left in the current window.
type Result struct {
	Allowed    bool
	RetryAfter int
}

// ErrInvalidUsage is returned for a weight that is not a positive integer; it is a programming error, not a limit.
var ErrInvalidUsage = errors.New("rate limit usage must be a positive integer")

// checkScript refuses before INCRBY, so a weighted request larger than the remaining budget consumes nothing, and sets the TTL only when it creates the counter, so a busy key never extends its window.
var checkScript = redis.NewScript(`
local key = KEYS[1]
local limit = tonumber(ARGV[1])
local ttl = tonumber(ARGV[2])
local requested = tonumber(ARGV[3])

local current = tonumber(redis.call('GET', key) or '0')
local next = current + requested

if next > limit then
  return {next, 0}
end

local updated = redis.call('INCRBY', key, requested)

if current == 0 then
  redis.call('EXPIRE', key, ttl)
end

return {updated, 1}
`)

type Limiter struct {
	rdb      redis.UniversalClient
	disabled bool
	log      *slog.Logger
	now      func() time.Time
}

func New(cfg config.Config, rdb *redis.Client, log *slog.Logger) *Limiter {
	return &Limiter{rdb: rdb, disabled: cfg.RateLimitingDisabled, log: log, now: time.Now}
}

// WithClock returns a copy of the limiter that reads the time from now. Windows are aligned to the wall clock, so tests freeze it to keep a scenario from straddling a window boundary on a busy machine.
func (l *Limiter) WithClock(now func() time.Time) *Limiter {
	c := *l
	c.now = now
	return &c
}

type window struct {
	key        string
	ttlSeconds int64
}

func (l *Limiter) window(rule Rule, identifier string) (window, error) {
	nowMs := l.now().UnixMilli()
	interval := int64(rule.Window / time.Second)
	start := nowMs / (interval * 1000) * interval
	key, err := cacheKey("rate_limit", rule.Namespace, identifier, strconv.FormatInt(start, 10))
	if err != nil {
		return window{}, err
	}
	end := start + interval
	// ceil((end*1000 - now) / 1000), at least one second.
	ttl := max(1, (end*1000-nowMs+999)/1000)
	return window{key: key, ttlSeconds: ttl}, nil
}

var cacheKeyPattern = regexp.MustCompile(`^fb:[^:]+(:[^:]+)*$`)

// cacheKey builds forma2's cache key ("fb:" + parts joined by ":"), rejecting empty parts and empty segments the way packages/cache did. Namespaces may themselves contain ":" ("auth:login").
func cacheKey(parts ...string) (string, error) {
	for _, p := range parts {
		if p == "" {
			return "", errors.New("invalid cache key: parts cannot be empty")
		}
	}
	key := "fb:" + strings.Join(parts, ":")
	if !cacheKeyPattern.MatchString(key) {
		return "", errors.New("invalid cache key: invalid structure")
	}
	return key, nil
}

// Check consumes requested units for identifier and reports whether the request fits in the current window. It fails open: when rate limiting is disabled, or Valkey cannot be reached, the request is allowed and the failure logged, because an outage of the counter store must not take sign-in down with it.
func (l *Limiter) Check(ctx context.Context, rule Rule, identifier string, requested int) (Result, error) {
	if requested < 1 {
		return Result{}, ErrInvalidUsage
	}
	if err := rule.validate(); err != nil {
		return Result{}, err
	}
	if l.disabled {
		return Result{Allowed: true}, nil
	}

	w, err := l.window(rule, identifier)
	if err != nil {
		return l.failOpen(ctx, rule, identifier, err), nil
	}
	vals, err := checkScript.Run(ctx, l.rdb, []string{w.key}, rule.Limit, w.ttlSeconds, requested).Int64Slice()
	if err != nil || len(vals) != 2 {
		if err == nil {
			err = fmt.Errorf("unexpected script result %v", vals)
		}
		return l.failOpen(ctx, rule, identifier, err), nil
	}
	count, allowed := vals[0], vals[1] == 1

	l.log.DebugContext(ctx, "Rate limit check",
		"identifier", identifier, "currentCount", count, "limit", rule.Limit, "window", int64(rule.Window/time.Second),
		"key", w.key, "requested", requested, "allowed", allowed)
	if allowed {
		return Result{Allowed: true}, nil
	}
	l.log.ErrorContext(ctx, "Rate limit exceeded",
		"identifier", identifier, "currentCount", count, "limit", rule.Limit, "window", int64(rule.Window/time.Second),
		"key", w.key, "namespace", rule.Namespace, "requested", requested)
	return Result{Allowed: false, RetryAfter: int(w.ttlSeconds)}, nil
}

// Peek reports whether one more unit would currently be allowed, without consuming anything.
func (l *Limiter) Peek(ctx context.Context, rule Rule, identifier string) (Result, error) {
	if err := rule.validate(); err != nil {
		return Result{}, err
	}
	if l.disabled {
		return Result{Allowed: true}, nil
	}
	w, err := l.window(rule, identifier)
	if err != nil {
		return l.failOpen(ctx, rule, identifier, err), nil
	}
	count, err := l.rdb.Get(ctx, w.key).Int64()
	if errors.Is(err, redis.Nil) {
		count, err = 0, nil
	}
	if err != nil {
		return l.failOpen(ctx, rule, identifier, err), nil
	}
	if count < int64(rule.Limit) {
		return Result{Allowed: true}, nil
	}
	l.log.ErrorContext(ctx, "Rate limit exceeded",
		"identifier", identifier, "currentCount", count, "limit", rule.Limit, "window", int64(rule.Window/time.Second),
		"key", w.key, "namespace", rule.Namespace)
	return Result{Allowed: false, RetryAfter: int(w.ttlSeconds)}, nil
}

func (l *Limiter) failOpen(ctx context.Context, rule Rule, identifier string, err error) Result {
	l.log.ErrorContext(ctx, "Rate limit check failed", "err", err, "identifier", identifier, "namespace", rule.Namespace)
	return Result{Allowed: true}
}

// Apply consumes one unit and returns apperr.TooManyRequests when the limit is reached.
func (l *Limiter) Apply(ctx context.Context, rule Rule, identifier string) error {
	return l.ApplyN(ctx, rule, identifier, 1)
}

// ApplyN consumes n units at once, all or nothing; an invite to five recipients is one request weighing five.
func (l *Limiter) ApplyN(ctx context.Context, rule Rule, identifier string, n int) error {
	res, err := l.Check(ctx, rule, identifier, n)
	if err != nil {
		return err
	}
	return deny(res)
}

// AssertAvailable fails fast when the window is already exhausted, without consuming a unit; pair it with a later Apply once the operation has succeeded.
func (l *Limiter) AssertAvailable(ctx context.Context, rule Rule, identifier string) error {
	res, err := l.Peek(ctx, rule, identifier)
	if err != nil {
		return err
	}
	return deny(res)
}

// ApplyIP consumes one unit for the request's client address (see IPIdentifier).
func (l *Limiter) ApplyIP(ctx context.Context, rule Rule) error {
	return l.Apply(ctx, rule, IPIdentifier(ctx))
}

func deny(res Result) error {
	if res.Allowed {
		return nil
	}
	return apperr.TooManyRequests(res.RetryAfter)
}

// IPIdentifier is the identifier of IP-based limits: the SHA-256 hex digest of the canonical client address (or of the shared untrusted identity), so raw addresses never appear in keys or logs. Like forma2's it is unsalted.
func IPIdentifier(ctx context.Context) string {
	sum := sha256.Sum256([]byte(httpapi.ClientIP(ctx)))
	return hex.EncodeToString(sum[:])
}
