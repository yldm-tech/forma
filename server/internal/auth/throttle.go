package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// Failed sign-ins are audited, but a brute-force run must not flood the audit stream. forma2's shouldLogAuthFailure (apps/web/modules/auth/lib/utils.ts) keeps a log of attempts per identity in five-minute buckets: the first three failures are recorded, then every tenth, or one after a quiet minute.
const (
	failureBucket         = 5 * time.Minute
	failureFreeAttempts   = 3
	failureSampleInterval = 10
	failureQuietGap       = time.Minute
)

type failureThrottle struct {
	rdb *redis.Client
	log *slog.Logger
	now func() time.Time
}

// auditIdentifier is forma2's createAuditIdentifier: a prefixed, truncated SHA-256 of the lowercased identifier, so attempts can be correlated without recording the address.
func auditIdentifier(identifier, prefix string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(identifier)))
	return prefix + "_" + hex.EncodeToString(sum[:])[:32]
}

// shouldRecord reports whether this failure for identifier is audited. It fails closed: without Valkey nothing is recorded, as in forma2, because an unthrottled audit stream is the thing being prevented.
//
// forma2 measured the quiet gap against the entry it had just added, so only the every-tenth rule ever fired; the gap here is measured against the previous attempt, as its comment intended.
func (f *failureThrottle) shouldRecord(ctx context.Context, identifier string) bool {
	now := f.now()
	nowMs := now.UnixMilli()
	bucketMs := failureBucket.Milliseconds()
	bucketStart := nowMs / bucketMs * bucketMs
	key := "fb:rate_limit:auth:" + auditIdentifier(identifier, "ratelimit") + ":" + strconv.FormatInt(bucketStart, 10)

	var member [8]byte
	if _, err := rand.Read(member[:]); err != nil {
		f.log.WarnContext(ctx, "Auth failure throttle unavailable, not auditing the failure", "err", err)
		return false
	}
	var previous *redis.ZSliceCmd
	var count *redis.IntCmd
	_, err := f.rdb.TxPipelined(ctx, func(p redis.Pipeliner) error {
		p.ZRemRangeByScore(ctx, key, "0", strconv.FormatInt(bucketStart, 10))
		count = p.ZCard(ctx, key)
		previous = p.ZRangeWithScores(ctx, key, -1, -1)
		p.ZAdd(ctx, key, redis.Z{Score: float64(nowMs), Member: fmt.Sprintf("%d:%s", nowMs, hex.EncodeToString(member[:]))})
		p.Expire(ctx, key, failureBucket)
		return nil
	})
	if err != nil {
		f.log.WarnContext(ctx, "Auth failure throttle unavailable, not auditing the failure", "err", err)
		return false
	}
	// earlier is the number of failures already in this bucket. forma2 compared that count, not the attempt number, with the threshold, so the first four attempts are recorded.
	earlier := count.Val()
	if earlier <= failureFreeAttempts || earlier%failureSampleInterval == 0 {
		return true
	}
	if last := previous.Val(); len(last) == 1 {
		return now.Sub(time.UnixMilli(int64(last[0].Score))) > failureQuietGap
	}
	return true
}
