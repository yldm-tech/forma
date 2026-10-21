package authz

import (
	"context"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Outbox limits, forma2's outbox-repository constants.
const (
	// outboxBatchSize is how many events one claim leases.
	outboxBatchSize = 200
	// outboxLease is how long a claim owns its events before another worker may take them.
	outboxLease = 60 * time.Second
	// outboxMaxBackoffAttempts bounds the backoff exponent, not the retries: attempts keep counting while SpiceDB is down.
	outboxMaxBackoffAttempts = 20
	outboxMaxRetryDelay      = 5 * time.Minute
	// outboxMaxPermanentFailures is how many failures attributable to one event alone dead-letter it. Only those count: a dead-lettered revocation arms the freshness guard for the whole deployment, so it must mean "PostgreSQL removed access and SpiceDB cannot be made to agree", never "SpiceDB was unreachable for a while".
	outboxMaxPermanentFailures = 10
	// revocationMaxAge is the freshness window: an undelivered revocation older than this stops every authorization check.
	revocationMaxAge      = 60 * time.Second
	revocationCriticalAge = 45 * time.Second
	revocationWarningAge  = 15 * time.Second
	// Delivered history is kept for a week, then deleted in bounded batches.
	outboxHistoryRetention   = 7 * 24 * time.Hour
	outboxHistoryDeleteBatch = 10_000
	outboxHistoryMaxBatches  = 100
)

// Outbox target types, the values the triggers write.
const (
	targetOrganization    = "organization"
	targetMembership      = "membership"
	targetUser            = "user"
	targetTeam            = "team"
	targetTeamMembership  = "team_membership"
	targetWorkspace       = "workspace"
	targetWorkspaceTeam   = "workspace_team"
	targetAPIKey          = "api_key"
	targetAPIKeyWorkspace = "api_key_workspace"
)

var pairTargets = map[string]bool{targetMembership: true, targetTeamMembership: true, targetWorkspaceTeam: true, targetAPIKeyWorkspace: true}

var singleTargets = map[string]bool{targetOrganization: true, targetUser: true, targetTeam: true, targetWorkspace: true, targetAPIKey: true}

// outboxEvent is one claimed row. Its identifiers are operationally sensitive and never reach logs.
type outboxEvent struct {
	ID           uuid.UUID
	TargetType   string
	PrimaryID    string
	SecondaryID  string
	IsRevocation bool
	Attempts     int
	CreatedAt    time.Time
}

// valid reports whether the event names a target the projectors understand: a known type with well-formed ids, a second id exactly for the pair types.
func (e outboxEvent) valid() bool {
	if uuid.Validate(e.PrimaryID) != nil {
		return false
	}
	switch {
	case pairTargets[e.TargetType]:
		return uuid.Validate(e.SecondaryID) == nil
	case singleTargets[e.TargetType]:
		return e.SecondaryID == ""
	default:
		return false
	}
}

// claimOutbox leases up to limit deliverable events to owner, revocations first and then oldest first. SKIP LOCKED lets concurrent workers claim disjoint batches; an expired lease is claimable again. Events that name nothing deliverable are dead-lettered at once rather than left leased forever; an invalid revocation then arms the freshness guard.
func claimOutbox(ctx context.Context, db *gorm.DB, owner string, limit int) ([]outboxEvent, error) {
	type row struct {
		ID           uuid.UUID
		TargetType   string
		PrimaryID    string
		SecondaryID  *string
		IsRevocation bool
		Attempts     int
		CreatedAt    time.Time
	}
	var rows []row
	err := db.WithContext(ctx).Raw(`
		WITH claimable AS (
			SELECT id
			FROM authz_projection_outbox
			WHERE processed_at IS NULL
				AND dead_lettered_at IS NULL
				AND available_at <= now()
				AND (lease_expires_at IS NULL OR lease_expires_at <= now())
			ORDER BY is_revocation DESC, created_at ASC
			LIMIT ?
			FOR UPDATE SKIP LOCKED
		)
		UPDATE authz_projection_outbox AS outbox
		SET attempts = outbox.attempts + 1,
			last_attempt_at = now(),
			leased_at = now(),
			lease_expires_at = now() + ? * interval '1 millisecond',
			lease_owner = ?,
			updated_at = now()
		FROM claimable
		WHERE outbox.id = claimable.id
		RETURNING outbox.id, outbox.target_type, outbox.primary_id, outbox.secondary_id, outbox.is_revocation, outbox.attempts, outbox.created_at`,
		limit, outboxLease.Milliseconds(), owner).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("claim authz outbox: %w", err)
	}

	events := make([]outboxEvent, 0, len(rows))
	var invalidIDs []uuid.UUID
	for _, r := range rows {
		e := outboxEvent{ID: r.ID, TargetType: r.TargetType, PrimaryID: r.PrimaryID, IsRevocation: r.IsRevocation, Attempts: r.Attempts, CreatedAt: r.CreatedAt}
		if r.SecondaryID != nil {
			e.SecondaryID = *r.SecondaryID
		}
		if !e.valid() {
			invalidIDs = append(invalidIDs, r.ID)
			continue
		}
		events = append(events, e)
	}
	if len(invalidIDs) > 0 {
		err := db.WithContext(ctx).Exec(`
			UPDATE authz_projection_outbox
			SET dead_lettered_at = now(), last_error_code = ?, lease_expires_at = NULL, leased_at = NULL, lease_owner = NULL, updated_at = now()
			WHERE id = ANY(?::uuid[]) AND lease_owner = ? AND processed_at IS NULL`,
			string(codeProjectionInvalidEvent), uuidArray(invalidIDs), owner).Error
		if err != nil {
			return nil, fmt.Errorf("dead-letter invalid authz outbox events: %w", err)
		}
	}
	// Oldest first within each kind, as the claim ordered them; the scan does not promise an order.
	slices.SortStableFunc(events, func(a, b outboxEvent) int {
		if a.IsRevocation != b.IsRevocation {
			if a.IsRevocation {
				return -1
			}
			return 1
		}
		return a.CreatedAt.Compare(b.CreatedAt)
	})
	return events, nil
}

// markDelivered completes the events this lease still owns.
func markDelivered(ctx context.Context, db *gorm.DB, owner string, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	err := db.WithContext(ctx).Exec(`
		UPDATE authz_projection_outbox
		SET processed_at = now(), last_error_code = NULL, lease_expires_at = NULL, leased_at = NULL, lease_owner = NULL, updated_at = now()
		WHERE id = ANY(?::uuid[]) AND lease_owner = ? AND processed_at IS NULL`,
		uuidArray(ids), owner).Error
	if err != nil {
		return fmt.Errorf("mark authz outbox events delivered: %w", err)
	}
	return nil
}

// markFailed releases a failed lease in one statement and returns how many events it dead-lettered. Each event backs off by its own attempt count, 1 s doubling up to 5 min. A permanent failure is charged only when the failure is not retryable and attributable to these events alone; only that can dead-letter, so a SpiceDB outage, a rejected credential or a group failure never dead-letters a bystander.
func markFailed(ctx context.Context, db *gorm.DB, owner string, ids []uuid.UUID, code Code, attributable, retryable bool) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	permanent := !retryable && attributable
	increment := 0
	if permanent {
		increment = 1
	}
	var deadLettered int
	err := db.WithContext(ctx).Raw(`
		WITH released AS (
			UPDATE authz_projection_outbox
			SET available_at = now() + LEAST(?::double precision, 1000 * 2 ^ (LEAST(attempts, ?) - 1)) * interval '1 millisecond',
				permanent_failures = permanent_failures + ?,
				dead_lettered_at = CASE WHEN ?::boolean AND permanent_failures + 1 >= ? THEN now() ELSE NULL END,
				last_error_code = ?,
				lease_expires_at = NULL,
				leased_at = NULL,
				lease_owner = NULL,
				updated_at = now()
			WHERE id = ANY(?::uuid[])
				AND lease_owner = ?
				AND processed_at IS NULL
				AND dead_lettered_at IS NULL
			RETURNING dead_lettered_at
		)
		SELECT count(*) FILTER (WHERE dead_lettered_at IS NOT NULL) FROM released`,
		outboxMaxRetryDelay.Milliseconds(), outboxMaxBackoffAttempts, increment, permanent, outboxMaxPermanentFailures, string(code), uuidArray(ids), owner).Scan(&deadLettered).Error
	if err != nil {
		return 0, fmt.Errorf("release failed authz outbox events: %w", err)
	}
	return deadLettered, nil
}

// hasStaleRevocation is the freshness guard's query: an undelivered revocation older than the window, or a dead-lettered one of any age. Two EXISTS because the halves live in different partial indexes.
func hasStaleRevocation(ctx context.Context, db *gorm.DB) (bool, error) {
	var stale bool
	err := db.WithContext(ctx).Raw(`
		SELECT EXISTS (
			SELECT 1 FROM authz_projection_outbox
			WHERE is_revocation AND processed_at IS NULL AND dead_lettered_at IS NULL
				AND created_at <= now() - ? * interval '1 millisecond'
		) OR EXISTS (
			SELECT 1 FROM authz_projection_outbox
			WHERE is_revocation AND processed_at IS NULL AND dead_lettered_at IS NOT NULL
		)`, revocationMaxAge.Milliseconds()).Scan(&stale).Error
	if err != nil {
		return false, fmt.Errorf("read authz projection freshness: %w", err)
	}
	return stale, nil
}

// OutboxStatus is the outbox's health. The revocation counts include dead letters, which are overdue by definition.
type OutboxStatus struct {
	Pending                 int64    `json:"pending"`
	DeadLettered            int64    `json:"deadLettered"`
	OverdueRevocations      int64    `json:"overdueRevocations"`
	RevocationsPastCritical int64    `json:"revocationsPastCritical"`
	RevocationsPastWarning  int64    `json:"revocationsPastWarning"`
	OldestPendingAgeSeconds *float64 `json:"oldestPendingAgeSeconds"`
}

// Healthy reports an outbox with nothing dead-lettered and no revocation past the warning age.
func (s OutboxStatus) Healthy() bool {
	return s.DeadLettered == 0 && s.OverdueRevocations == 0 && s.RevocationsPastCritical == 0 && s.RevocationsPastWarning == 0
}

// outboxStatus aggregates the undelivered rows only, so the five-second delivery job never scans the retained history.
func outboxStatus(ctx context.Context, db *gorm.DB) (OutboxStatus, error) {
	var s OutboxStatus
	err := db.WithContext(ctx).Raw(`
		SELECT
			count(*) FILTER (WHERE dead_lettered_at IS NULL) AS pending,
			count(*) FILTER (WHERE dead_lettered_at IS NOT NULL) AS dead_lettered,
			count(*) FILTER (WHERE is_revocation AND (dead_lettered_at IS NOT NULL OR created_at <= now() - ? * interval '1 millisecond')) AS overdue_revocations,
			count(*) FILTER (WHERE is_revocation AND (dead_lettered_at IS NOT NULL OR created_at <= now() - ? * interval '1 millisecond')) AS revocations_past_critical,
			count(*) FILTER (WHERE is_revocation AND (dead_lettered_at IS NOT NULL OR created_at <= now() - ? * interval '1 millisecond')) AS revocations_past_warning,
			EXTRACT(EPOCH FROM now() - min(created_at) FILTER (WHERE dead_lettered_at IS NULL))::double precision AS oldest_pending_age_seconds
		FROM authz_projection_outbox
		WHERE processed_at IS NULL`,
		revocationMaxAge.Milliseconds(), revocationCriticalAge.Milliseconds(), revocationWarningAge.Milliseconds()).Scan(&s).Error
	if err != nil {
		return OutboxStatus{}, fmt.Errorf("read authz outbox status: %w", err)
	}
	if s.OldestPendingAgeSeconds != nil {
		age := math.Max(0, math.Floor(*s.OldestPendingAgeSeconds))
		s.OldestPendingAgeSeconds = &age
	}
	return s, nil
}

// replayDeadLetters hands every undelivered dead letter back to delivery with a fresh attempt and permanent-failure budget: a replay is a decision that the cause was addressed.
func replayDeadLetters(ctx context.Context, db *gorm.DB) (int64, error) {
	res := db.WithContext(ctx).Exec(`
		UPDATE authz_projection_outbox
		SET attempts = 0, permanent_failures = 0, available_at = now(), dead_lettered_at = NULL, last_error_code = NULL,
			lease_expires_at = NULL, leased_at = NULL, lease_owner = NULL, updated_at = now()
		WHERE dead_lettered_at IS NOT NULL AND processed_at IS NULL`)
	if res.Error != nil {
		return 0, fmt.Errorf("replay authz outbox dead letters: %w", res.Error)
	}
	return res.RowsAffected, nil
}

// pruneOutboxHistory deletes events delivered more than a week ago, in bounded batches so no statement holds locks across the whole history. Pending and dead-lettered rows are never touched.
func pruneOutboxHistory(ctx context.Context, db *gorm.DB) (int64, error) {
	var deleted int64
	for range outboxHistoryMaxBatches {
		res := db.WithContext(ctx).Exec(`
			WITH expired AS (
				SELECT id FROM authz_projection_outbox
				WHERE processed_at < now() - ? * interval '1 millisecond'
				ORDER BY processed_at
				LIMIT ?
			)
			DELETE FROM authz_projection_outbox AS outbox USING expired WHERE outbox.id = expired.id`,
			outboxHistoryRetention.Milliseconds(), outboxHistoryDeleteBatch)
		if res.Error != nil {
			return deleted, fmt.Errorf("prune authz outbox history: %w", res.Error)
		}
		deleted += res.RowsAffected
		if res.RowsAffected < outboxHistoryDeleteBatch {
			break
		}
	}
	return deleted, nil
}

// uuidArray renders ids as a PostgreSQL array literal for an `?::uuid[]` parameter. uuid's text form has no characters an array literal would need to escape.
func uuidArray(ids []uuid.UUID) string {
	b := make([]byte, 0, 2+len(ids)*37)
	b = append(b, '{')
	for i, id := range ids {
		if i > 0 {
			b = append(b, ',')
		}
		b = append(b, id.String()...)
	}
	return string(append(b, '}'))
}
