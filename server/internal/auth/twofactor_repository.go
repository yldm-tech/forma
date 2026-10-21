package auth

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Account lockout, Better Auth's defaults that forma2 ran with (plugins/two-factor/verify-two-factor.mjs): ten consecutive failed second factors at sign-in, TOTP and backup codes together and across sign-in attempts, lock the second factor for fifteen minutes.
const (
	totpLockoutThreshold = 10
	totpLockoutDuration  = 15 * time.Minute
)

// totpRow is a user's user_totp row. Verified is false between setup and the first confirmed code; only a verified row turns two-factor authentication on.
type totpRow struct {
	UserID                uuid.UUID
	SecretCiphertext      []byte
	BackupCodesCiphertext []byte
	Verified              bool
	FailedAttempts        int
	LockedUntil           sql.NullTime
}

// lockTOTP reads the user's row for update, or nil when there is none.
func (r *Repository) lockTOTP(ctx context.Context, userID uuid.UUID) (*totpRow, error) {
	var rows []totpRow
	err := r.db.WithContext(ctx).Raw(
		`SELECT user_id, secret_ciphertext, backup_codes_ciphertext, verified, failed_attempts, locked_until FROM user_totp WHERE user_id = ? FOR UPDATE`, userID,
	).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("load totp: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// putPendingTOTP stores a new, unverified enrolment, replacing an earlier unfinished one.
func (r *Repository) putPendingTOTP(ctx context.Context, userID uuid.UUID, secretCiphertext, backupCodesCiphertext []byte, now time.Time) error {
	if err := r.db.WithContext(ctx).Exec(
		`INSERT INTO user_totp (user_id, secret_ciphertext, backup_codes_ciphertext, verified, failed_attempts, locked_until, created_at, updated_at)
		 VALUES (?, ?, ?, false, 0, NULL, ?, ?)
		 ON CONFLICT (user_id) DO UPDATE SET secret_ciphertext = excluded.secret_ciphertext, backup_codes_ciphertext = excluded.backup_codes_ciphertext,
		     verified = false, failed_attempts = 0, locked_until = NULL, created_at = excluded.created_at, updated_at = excluded.updated_at`,
		userID, secretCiphertext, backupCodesCiphertext, now, now,
	).Error; err != nil {
		return fmt.Errorf("store totp enrolment: %w", err)
	}
	return nil
}

func (r *Repository) markTOTPVerified(ctx context.Context, userID uuid.UUID, now time.Time) error {
	if err := r.db.WithContext(ctx).Exec(`UPDATE user_totp SET verified = true, updated_at = ? WHERE user_id = ?`, now, userID).Error; err != nil {
		return fmt.Errorf("verify totp: %w", err)
	}
	return nil
}

func (r *Repository) deleteTOTP(ctx context.Context, userID uuid.UUID) error {
	if err := r.db.WithContext(ctx).Exec(`DELETE FROM user_totp WHERE user_id = ?`, userID).Error; err != nil {
		return fmt.Errorf("delete totp: %w", err)
	}
	return nil
}

func (r *Repository) replaceBackupCodes(ctx context.Context, userID uuid.UUID, backupCodesCiphertext []byte, now time.Time) error {
	if err := r.db.WithContext(ctx).Exec(
		`UPDATE user_totp SET backup_codes_ciphertext = ?, updated_at = ? WHERE user_id = ?`, backupCodesCiphertext, now, userID,
	).Error; err != nil {
		return fmt.Errorf("update backup codes: %w", err)
	}
	return nil
}

// recordTOTPFailure counts one failed second factor and locks the second factor once the count reaches the threshold.
func (r *Repository) recordTOTPFailure(ctx context.Context, userID uuid.UUID, now time.Time) error {
	if err := r.db.WithContext(ctx).Exec(
		`UPDATE user_totp SET failed_attempts = failed_attempts + 1,
		     locked_until = CASE WHEN failed_attempts + 1 >= ? THEN ?::timestamptz ELSE locked_until END,
		     updated_at = ?
		 WHERE user_id = ?`,
		totpLockoutThreshold, now.Add(totpLockoutDuration), now, userID,
	).Error; err != nil {
		return fmt.Errorf("record totp failure: %w", err)
	}
	return nil
}

// resetTOTPFailures clears the count and any lock after a successful second factor, so the count tracks consecutive failures only; an expired lock is cleared the same way before it is checked again.
func (r *Repository) resetTOTPFailures(ctx context.Context, userID uuid.UUID, now time.Time) error {
	if err := r.db.WithContext(ctx).Exec(
		`UPDATE user_totp SET failed_attempts = 0, locked_until = NULL, updated_at = ? WHERE user_id = ?`, now, userID,
	).Error; err != nil {
		return fmt.Errorf("reset totp failures: %w", err)
	}
	return nil
}

// pendingSignIn is a sign-in waiting for its second factor (a two_factor verification). Attempts counts the codes already refused for it.
type pendingSignIn struct {
	ID       uuid.UUID
	UserID   uuid.UUID
	Attempts int
}

// lockPendingSignIn reads a live pending sign-in for update, or nil when the token is unknown, used or expired. Holding the row serializes concurrent attempts against one sign-in, so the attempt budget cannot be overrun by sending codes in parallel.
func (r *Repository) lockPendingSignIn(ctx context.Context, tokenHash []byte, now time.Time) (*pendingSignIn, error) {
	var rows []pendingSignIn
	err := r.db.WithContext(ctx).Raw(
		`SELECT id, user_id, COALESCE((payload ->> 'attempts')::int, 0) AS attempts FROM verifications
		 WHERE token_hash = ? AND purpose = ? AND consumed_at IS NULL AND expires_at > ? AND user_id IS NOT NULL
		 FOR UPDATE`,
		tokenHash, purposeTwoFactor, now,
	).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("load pending sign-in: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

func (r *Repository) countPendingSignInFailure(ctx context.Context, id uuid.UUID) error {
	if err := r.db.WithContext(ctx).Exec(
		`UPDATE verifications SET payload = jsonb_set(payload, '{attempts}', to_jsonb(COALESCE((payload ->> 'attempts')::int, 0) + 1)) WHERE id = ?`, id,
	).Error; err != nil {
		return fmt.Errorf("count pending sign-in failure: %w", err)
	}
	return nil
}

func (r *Repository) consumePendingSignIn(ctx context.Context, id uuid.UUID, now time.Time) error {
	if err := r.db.WithContext(ctx).Exec(`UPDATE verifications SET consumed_at = ? WHERE id = ?`, now, id).Error; err != nil {
		return fmt.Errorf("consume pending sign-in: %w", err)
	}
	return nil
}
