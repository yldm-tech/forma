package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Verification purposes of single sign-on.
const (
	purposeOAuthState  = "oauth_state"
	purposeSSORecovery = "sso_recovery"
)

// identityOwner returns the user an external identity is linked to, or uuid.Nil. External identities carry no issuer: forma2 keyed each provider's accounts by one fixed issuer string, so the provider alone is the namespace.
func (r *Repository) identityOwner(ctx context.Context, provider, subject string) (uuid.UUID, error) {
	ids, err := r.scanIDs(ctx, `SELECT user_id FROM user_identities WHERE provider = ? AND issuer = '' AND subject = ?`, provider, subject)
	if err != nil {
		return uuid.Nil, fmt.Errorf("load identity: %w", err)
	}
	if len(ids) == 0 {
		return uuid.Nil, nil
	}
	return ids[0], nil
}

func (r *Repository) insertIdentity(ctx context.Context, userID uuid.UUID, provider, subject string) error {
	if err := r.db.WithContext(ctx).Exec(
		`INSERT INTO user_identities (user_id, provider, subject) VALUES (?, ?, ?)`, userID, provider, subject,
	).Error; err != nil {
		return fmt.Errorf("insert %s identity: %w", provider, err)
	}
	return nil
}

// syncName applies the identity provider's display name, which forma2 re-read on every SSO sign-in.
func (r *Repository) syncName(ctx context.Context, userID uuid.UUID, name string, now time.Time) error {
	if err := r.db.WithContext(ctx).Exec(
		`UPDATE users SET name = ?, updated_at = ? WHERE id = ? AND name <> ?`, name, now, userID, name,
	).Error; err != nil {
		return fmt.Errorf("update name: %w", err)
	}
	return nil
}

func (r *Repository) setEmailVerified(ctx context.Context, userID uuid.UUID, verified bool) error {
	if err := r.db.WithContext(ctx).Exec(`UPDATE users SET email_verified = ? WHERE id = ?`, verified, userID).Error; err != nil {
		return fmt.Errorf("set email verified: %w", err)
	}
	return nil
}

// insertPayloadVerification stores a single-use token with a JSON payload. userID may be uuid.Nil for a token that belongs to no user yet.
func (r *Repository) insertPayloadVerification(ctx context.Context, purpose string, userID uuid.UUID, email string, tokenHash []byte, expiresAt time.Time, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode %s payload: %w", purpose, err)
	}
	var owner any
	if userID != uuid.Nil {
		owner = userID
	}
	if err := r.db.WithContext(ctx).Exec(
		`INSERT INTO verifications (purpose, user_id, email, token_hash, expires_at, payload) VALUES (?, ?, ?, ?, ?, ?)`,
		purpose, owner, email, tokenHash, expiresAt, string(raw),
	).Error; err != nil {
		return fmt.Errorf("insert %s token: %w", purpose, err)
	}
	return nil
}

// payloadVerification is a token together with its payload.
type payloadVerification struct {
	verification
	Payload []byte
}

// findPayloadVerification returns the token whatever its state, or nil when it does not exist.
func (r *Repository) findPayloadVerification(ctx context.Context, purpose string, tokenHash []byte) (*payloadVerification, error) {
	var rows []payloadVerification
	err := r.db.WithContext(ctx).Raw(
		`SELECT id, user_id, email, expires_at, consumed_at, payload FROM verifications WHERE token_hash = ? AND purpose = ?`,
		tokenHash, purpose,
	).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("load %s token: %w", purpose, err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// consumePayloadVerification marks a live token used and returns it with its payload, or nil when the token is unknown, expired or already used.
func (r *Repository) consumePayloadVerification(ctx context.Context, purpose string, tokenHash []byte, now time.Time) (*payloadVerification, error) {
	var rows []payloadVerification
	err := r.db.WithContext(ctx).Raw(
		`UPDATE verifications SET consumed_at = ?
		 WHERE token_hash = ? AND purpose = ? AND consumed_at IS NULL AND expires_at > ?
		 RETURNING id, user_id, email, expires_at, consumed_at, payload`,
		now, tokenHash, purpose, now,
	).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("consume %s token: %w", purpose, err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// reclaimOutcome is what reclaimUnprovenAccount removed.
type reclaimOutcome struct {
	PasswordsCleared  int64
	TwoFactorsRemoved int64
}

// reclaimUnprovenAccount marks the address verified and strips the local sign-in factors set while it was unproven: the credential identity's password (the row stays, so a reset can set a new one), two-factor authentication, and the email change links and pending second-factor sign-ins those factors authorized. Every write is idempotent, so concurrent recoveries converge.
func (r *Repository) reclaimUnprovenAccount(ctx context.Context, userID uuid.UUID, now time.Time) (reclaimOutcome, error) {
	db := r.db.WithContext(ctx)
	if err := r.markEmailVerified(ctx, userID, now); err != nil {
		return reclaimOutcome{}, err
	}
	passwords := db.Exec(
		`UPDATE user_identities SET password_hash = NULL, updated_at = ? WHERE user_id = ? AND provider = 'credential' AND password_hash IS NOT NULL`, now, userID,
	)
	if passwords.Error != nil {
		return reclaimOutcome{}, fmt.Errorf("clear password: %w", passwords.Error)
	}
	factors := db.Exec(`DELETE FROM user_totp WHERE user_id = ?`, userID)
	if factors.Error != nil {
		return reclaimOutcome{}, fmt.Errorf("remove two-factor authentication: %w", factors.Error)
	}
	if err := r.deleteVerifications(ctx, userID, purposeEmailChange, purposeTwoFactor); err != nil {
		return reclaimOutcome{}, err
	}
	return reclaimOutcome{PasswordsCleared: passwords.RowsAffected, TwoFactorsRemoved: factors.RowsAffected}, nil
}

func (r *Repository) organizationExists(ctx context.Context, id uuid.UUID) (bool, error) {
	var exists bool
	if err := r.db.WithContext(ctx).Raw(`SELECT EXISTS (SELECT 1 FROM organizations WHERE id = ?)`, id).Scan(&exists).Error; err != nil {
		return false, fmt.Errorf("check organization: %w", err)
	}
	return exists, nil
}

// createOrganizationWithID creates the organization unless one with the id exists; created is false when another sign-up got there first.
func (r *Repository) createOrganizationWithID(ctx context.Context, id uuid.UUID, name string) (created bool, err error) {
	ids, err := r.scanIDs(ctx, `INSERT INTO organizations (id, name) VALUES (?, ?) ON CONFLICT (id) DO NOTHING RETURNING id`, id, name)
	if err != nil {
		return false, fmt.Errorf("create organization: %w", err)
	}
	return len(ids) == 1, nil
}
