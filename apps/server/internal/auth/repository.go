package auth

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Verification purposes (the verifications.purpose check constraint).
const (
	purposeEmailVerification = "email_verification"
	purposeEmailChange       = "email_change"
	purposePasswordReset     = "password_reset"
	purposeTwoFactor         = "two_factor"
	purposeSignupIntent      = "signup_intent"
	purposeAccountDeletion   = "account_deletion"
)

// Repository reads and writes the identity tables: users, user_identities, sessions and verifications, plus the rows a new account's organization starts with.
type Repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// transaction runs fn with a repository bound to one transaction.
func (r *Repository) transaction(ctx context.Context, fn func(tx *Repository) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(&Repository{db: tx})
	})
}

// account is a user with what the authentication rules need to know about how they sign in.
type account struct {
	ID            uuid.UUID
	Name          string
	Email         string
	EmailVerified bool
	Locale        string
	IsActive      bool
	CreatedAt     time.Time
	LastLoginAt   sql.NullTime
	// PasswordHash is the credential identity's bcrypt hash; empty when the user has no password.
	PasswordHash sql.NullString
	// HasCredentialIdentity is true while the user has a credential identity, even one whose password SSO recovery removed.
	HasCredentialIdentity bool
	HasExternalIdentity   bool
	TwoFactorEnabled      bool
}

// hasPassword reports whether the user has a credential identity.
func (a *account) hasPassword() bool {
	return a.PasswordHash.Valid && a.PasswordHash.String != ""
}

// emailIdentity is forma2's identityProvider == "email": an account that signs in with a password and was never bound to an external identity provider. Account linking is off, so a password user only gains an external identity by moving to SSO.
func (a *account) emailIdentity() bool {
	return a.hasPassword() && !a.HasExternalIdentity
}

// ssoOnly is an account that signs in only through an external identity provider, which then owns its second factor, so two-factor authentication is neither set up nor removed here. An account that has external identities and a password (SSO recovery keeps the password of a verified account) still signs in with the password alone, so it can turn two-factor authentication on and off like a password account; an organization's two-factor requirement applies to it too.
func (a *account) ssoOnly() bool {
	return a.HasExternalIdentity && !a.hasPassword()
}

// identityProvider names the account's sign-in method the way forma2's User.identityProvider did, for the SPA.
func (a *account) identityProvider() string {
	if a.emailIdentity() {
		return "email"
	}
	return "sso"
}

const accountQuery = `
SELECT u.id, u.name, u.email, u.email_verified, u.locale, u.is_active, u.created_at, u.last_login_at,
       (SELECT i.password_hash FROM user_identities i WHERE i.user_id = u.id AND i.provider = 'credential' ORDER BY i.created_at LIMIT 1) AS password_hash,
       EXISTS (SELECT 1 FROM user_identities i WHERE i.user_id = u.id AND i.provider = 'credential') AS has_credential_identity,
       EXISTS (SELECT 1 FROM user_identities i WHERE i.user_id = u.id AND i.provider <> 'credential') AS has_external_identity,
       EXISTS (SELECT 1 FROM user_totp t WHERE t.user_id = u.id AND t.verified) AS two_factor_enabled
FROM users u`

// accountByEmail finds a user by address, ignoring case as the unique index does. It returns nil when there is none.
func (r *Repository) accountByEmail(ctx context.Context, email string) (*account, error) {
	return r.findAccount(ctx, accountQuery+` WHERE lower(u.email) = lower(?)`, email)
}

// accountByID returns nil when the user does not exist.
func (r *Repository) accountByID(ctx context.Context, id uuid.UUID) (*account, error) {
	return r.findAccount(ctx, accountQuery+` WHERE u.id = ?`, id)
}

// lockAccount reads the user for update inside a transaction, so concurrent changes to the same account serialize.
func (r *Repository) lockAccount(ctx context.Context, id uuid.UUID) (*account, error) {
	return r.findAccount(ctx, accountQuery+` WHERE u.id = ? FOR NO KEY UPDATE OF u`, id)
}

func (r *Repository) findAccount(ctx context.Context, query string, arg any) (*account, error) {
	var rows []account
	if err := r.db.WithContext(ctx).Raw(query, arg).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load account: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// scanIDs runs a query whose rows are one uuid each.
func (r *Repository) scanIDs(ctx context.Context, query string, args ...any) ([]uuid.UUID, error) {
	rows, err := r.db.WithContext(ctx).Raw(query, args...).Rows()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// scanID runs a query that returns exactly one uuid.
func (r *Repository) scanID(ctx context.Context, query string, args ...any) (uuid.UUID, error) {
	ids, err := r.scanIDs(ctx, query, args...)
	if err != nil {
		return uuid.Nil, err
	}
	if len(ids) != 1 {
		return uuid.Nil, fmt.Errorf("expected one row, got %d", len(ids))
	}
	return ids[0], nil
}

// hasAnyUser reports whether the instance has a user; a fresh instance admits its first administrator without an invite.
func (r *Repository) hasAnyUser(ctx context.Context) (bool, error) {
	var exists bool
	if err := r.db.WithContext(ctx).Raw(`SELECT EXISTS (SELECT 1 FROM users)`).Scan(&exists).Error; err != nil {
		return false, fmt.Errorf("count users: %w", err)
	}
	return exists, nil
}

// firstUserLocked takes the transaction-scoped lock that serializes first-administrator sign-ups and reports whether the instance still has no user.
func (r *Repository) firstUserLocked(ctx context.Context) (bool, error) {
	if err := r.db.WithContext(ctx).Exec(`SELECT pg_advisory_xact_lock(hashtext('forma.first-administrator-signup'))`).Error; err != nil {
		return false, fmt.Errorf("lock first sign-up: %w", err)
	}
	hasUser, err := r.hasAnyUser(ctx)
	return !hasUser, err
}

// insertUser creates a user unless the address is taken. created is false, and nothing is written, for a duplicate.
func (r *Repository) insertUser(ctx context.Context, name, email, locale string) (id uuid.UUID, created bool, err error) {
	ids, err := r.scanIDs(ctx,
		`INSERT INTO users (name, email, locale) VALUES (?, ?, ?) ON CONFLICT ((lower(email))) DO NOTHING RETURNING id`,
		name, email, locale,
	)
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("insert user: %w", err)
	}
	if len(ids) == 0 {
		return uuid.Nil, false, nil
	}
	return ids[0], true, nil
}

// setPassword stores a new hash on the user's credential identity, creating the identity when the user has none.
func (r *Repository) setPassword(ctx context.Context, userID uuid.UUID, hash string, now time.Time) error {
	res := r.db.WithContext(ctx).Exec(
		`UPDATE user_identities SET password_hash = ?, updated_at = ? WHERE user_id = ? AND provider = 'credential'`,
		hash, now, userID,
	)
	if res.Error != nil {
		return fmt.Errorf("update password: %w", res.Error)
	}
	if res.RowsAffected > 0 {
		return nil
	}
	if err := r.db.WithContext(ctx).Exec(
		`INSERT INTO user_identities (user_id, provider, subject, password_hash) VALUES (?, 'credential', ?, ?)`,
		userID, userID.String(), hash,
	).Error; err != nil {
		return fmt.Errorf("insert credential identity: %w", err)
	}
	return nil
}

func (r *Repository) markEmailVerified(ctx context.Context, userID uuid.UUID, now time.Time) error {
	if err := r.db.WithContext(ctx).Exec(
		`UPDATE users SET email_verified = true, updated_at = ? WHERE id = ?`, now, userID,
	).Error; err != nil {
		return fmt.Errorf("mark email verified: %w", err)
	}
	return nil
}

// updateEmail moves the account to a new address. taken is true, and nothing changes, when another account holds the address.
func (r *Repository) updateEmail(ctx context.Context, userID uuid.UUID, email string, verified bool, now time.Time) (taken bool, err error) {
	res := r.db.WithContext(ctx).Exec(
		`UPDATE users SET email = ?, email_verified = email_verified OR ?, updated_at = ?
		 WHERE id = ? AND NOT EXISTS (SELECT 1 FROM users o WHERE lower(o.email) = lower(?) AND o.id <> ?)`,
		email, verified, now, userID, email, userID,
	)
	if res.Error != nil {
		return false, fmt.Errorf("update email: %w", res.Error)
	}
	return res.RowsAffected == 0, nil
}

// emailTaken reports whether any account uses the address.
func (r *Repository) emailTaken(ctx context.Context, email string) (bool, error) {
	var exists bool
	if err := r.db.WithContext(ctx).Raw(`SELECT EXISTS (SELECT 1 FROM users WHERE lower(email) = lower(?))`, email).Scan(&exists).Error; err != nil {
		return false, fmt.Errorf("check email: %w", err)
	}
	return exists, nil
}

func (r *Repository) touchLastLogin(ctx context.Context, userID uuid.UUID, now time.Time) error {
	if err := r.db.WithContext(ctx).Exec(`UPDATE users SET last_login_at = ? WHERE id = ?`, now, userID).Error; err != nil {
		return fmt.Errorf("update last login: %w", err)
	}
	return nil
}

// defaultWorkspaceName and the rows below are what forma2's createWorkspace gave every new workspace (apps/web/modules/workspaces/settings/lib/workspace.ts).
const defaultWorkspaceName = "My workspace"

var defaultContactAttributeKeys = []struct {
	key, name, description string
	unique                 bool
}{
	{"userId", "User Id", "The user id of a contact", true},
	{"email", "Email", "The email of a contact", true},
	{"firstName", "First Name", "Your contact's first name", false},
	{"lastName", "Last Name", "Your contact's last name", false},
	{"language", "Language", "The language preference of a contact", false},
}

// provisionOrganization gives a new user what forma2's sign-up gave an uninvited one: "<name>'s Organization" with the user as its owner, a default workspace, and organization alerts switched off for that organization. timeZone, the browser's zone when it sent a valid one, becomes the organization's display and survey scheduling zone, as for an organization created from the switcher; nil leaves both unset (UTC and the instance's SURVEY_SCHEDULING_TIME_ZONE).
func (r *Repository) provisionOrganization(ctx context.Context, userID uuid.UUID, userName string, timeZone *string) (uuid.UUID, error) {
	orgID, err := r.scanID(ctx,
		`INSERT INTO organizations (name, display_time_zone, survey_scheduling_time_zone) VALUES (?, ?, ?) RETURNING id`,
		userName+"'s Organization", timeZone, timeZone,
	)
	if err != nil {
		return uuid.Nil, fmt.Errorf("create organization: %w", err)
	}
	if err := r.addMembership(ctx, userID, orgID, "owner"); err != nil {
		return uuid.Nil, err
	}
	if err := r.createDefaultWorkspace(ctx, orgID); err != nil {
		return uuid.Nil, err
	}
	if err := r.unsubscribeOrganizationAlerts(ctx, userID, orgID); err != nil {
		return uuid.Nil, err
	}
	return orgID, nil
}

// addMembership makes the user an accepted member with the role. A membership that already exists is left as it is, so a retried write converges.
func (r *Repository) addMembership(ctx context.Context, userID, orgID uuid.UUID, role string) error {
	if err := r.db.WithContext(ctx).Exec(
		`INSERT INTO memberships (user_id, organization_id, role, accepted) VALUES (?, ?, ?, true) ON CONFLICT (user_id, organization_id) DO NOTHING`, userID, orgID, role,
	).Error; err != nil {
		return fmt.Errorf("create membership: %w", err)
	}
	return nil
}

// createDefaultWorkspace adds the workspace forma2's createWorkspace gave a new organization.
func (r *Repository) createDefaultWorkspace(ctx context.Context, orgID uuid.UUID) error {
	db := r.db.WithContext(ctx)
	workspaceID, err := r.scanID(ctx,
		`INSERT INTO workspaces (organization_id, name, config) VALUES (?, ?, '{"channel": null, "industry": null}') RETURNING id`,
		orgID, defaultWorkspaceName,
	)
	if err != nil {
		return fmt.Errorf("create workspace: %w", err)
	}
	if err := db.Exec(`INSERT INTO languages (workspace_id, code) VALUES (?, 'en-US')`, workspaceID).Error; err != nil {
		return fmt.Errorf("create workspace language: %w", err)
	}
	for _, k := range defaultContactAttributeKeys {
		if err := db.Exec(
			`INSERT INTO contact_attribute_keys (workspace_id, key, name, description, type, is_unique) VALUES (?, ?, ?, ?, 'default', ?)`,
			workspaceID, k.key, k.name, k.description, k.unique,
		).Error; err != nil {
			return fmt.Errorf("create contact attribute key %s: %w", k.key, err)
		}
	}
	return nil
}

// unsubscribeOrganizationAlerts switches the organization's alerts off for the user, as forma2 did for every organization a user was added to on sign-up. An organization already in the list is not added twice.
func (r *Repository) unsubscribeOrganizationAlerts(ctx context.Context, userID, orgID uuid.UUID) error {
	if err := r.db.WithContext(ctx).Exec(
		`UPDATE users SET notification_settings = jsonb_build_object(
		     'alert', COALESCE(notification_settings -> 'alert', '{}'::jsonb),
		     'unsubscribedOrganizationIds', CASE
		         WHEN COALESCE(notification_settings -> 'unsubscribedOrganizationIds', '[]'::jsonb) @> jsonb_build_array(?::text)
		         THEN notification_settings -> 'unsubscribedOrganizationIds'
		         ELSE COALESCE(notification_settings -> 'unsubscribedOrganizationIds', '[]'::jsonb) || jsonb_build_array(?::text)
		     END)
		 WHERE id = ?`,
		orgID.String(), orgID.String(), userID,
	).Error; err != nil {
		return fmt.Errorf("update notification settings: %w", err)
	}
	return nil
}

// soleOwnedOrganizations lists the organizations the user owns with no other active owner; they cannot outlive the account. A deactivated owner can never sign in again and a suspended one cannot act in the organization, so an organization left to one would have nobody to administer it (the org module's otherActiveOwners rule).
func (r *Repository) soleOwnedOrganizations(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error) {
	ids, err := r.scanIDs(ctx,
		`SELECT m.organization_id FROM memberships m
		 WHERE m.user_id = ? AND m.role = 'owner'
		   AND NOT EXISTS (
		     SELECT 1 FROM memberships o JOIN users u ON u.id = o.user_id
		     WHERE o.organization_id = m.organization_id AND o.role = 'owner' AND o.user_id <> m.user_id AND u.is_active AND o.suspended_at IS NULL)`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list sole-owned organizations: %w", err)
	}
	return ids, nil
}

// soleOwnedOrganization is one organization that would go with the account, as the deletion preview names it.
type soleOwnedOrganization struct {
	ID             uuid.UUID `json:"id"`
	Name           string    `json:"name"`
	WorkspaceCount int       `json:"workspaceCount"`
}

// soleOwnedOrganizationSummaries names the organizations soleOwnedOrganizations finds, with how many workspaces each holds, by name. It reads the same set the deletion removes, so the preview and the deletion cannot drift apart.
func (r *Repository) soleOwnedOrganizationSummaries(ctx context.Context, userID uuid.UUID) ([]soleOwnedOrganization, error) {
	ids, err := r.soleOwnedOrganizations(ctx, userID)
	if err != nil {
		return nil, err
	}
	orgs := []soleOwnedOrganization{}
	if len(ids) == 0 {
		return orgs, nil
	}
	if err := r.db.WithContext(ctx).Raw(
		`SELECT o.id, o.name, (SELECT count(*) FROM workspaces w WHERE w.organization_id = o.id) AS workspace_count
		 FROM organizations o WHERE o.id IN ? ORDER BY o.name, o.id`,
		ids,
	).Scan(&orgs).Error; err != nil {
		return nil, fmt.Errorf("load sole-owned organizations: %w", err)
	}
	return orgs, nil
}

// stripeCustomers are the Stripe customers of the organizations.
func (r *Repository) stripeCustomers(ctx context.Context, organizationIDs []uuid.UUID) ([]string, error) {
	if len(organizationIDs) == 0 {
		return nil, nil
	}
	var ids []string
	if err := r.db.WithContext(ctx).Raw(`SELECT stripe_customer_id FROM organization_billing WHERE organization_id IN ? AND stripe_customer_id IS NOT NULL`, organizationIDs).Scan(&ids).Error; err != nil {
		return nil, fmt.Errorf("load Stripe customers: %w", err)
	}
	return ids, nil
}

// deleteAccount removes the organizations only this user owns, the invites they sent (which do not cascade), and the user; memberships, identities, sessions and tokens cascade with the user row.
func (r *Repository) deleteAccount(ctx context.Context, userID uuid.UUID, soleOwned []uuid.UUID) error {
	db := r.db.WithContext(ctx)
	if len(soleOwned) > 0 {
		if err := db.Exec(`DELETE FROM organizations WHERE id IN ?`, soleOwned).Error; err != nil {
			return fmt.Errorf("delete sole-owned organizations: %w", err)
		}
	}
	if err := db.Exec(`DELETE FROM invites WHERE creator_id = ?`, userID).Error; err != nil {
		return fmt.Errorf("delete created invites: %w", err)
	}
	if err := db.Exec(`DELETE FROM users WHERE id = ?`, userID).Error; err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	return nil
}

// isMember reports whether the user belongs to the organization.
func (r *Repository) isMember(ctx context.Context, userID, organizationID uuid.UUID) (bool, error) {
	var exists bool
	err := r.db.WithContext(ctx).Raw(
		`SELECT EXISTS (SELECT 1 FROM memberships WHERE user_id = ? AND organization_id = ?)`, userID, organizationID,
	).Scan(&exists).Error
	if err != nil {
		return false, fmt.Errorf("check membership: %w", err)
	}
	return exists, nil
}

// verification is one single-use token. The token itself is never stored, only its SHA-256.
type verification struct {
	ID         uuid.UUID
	UserID     uuid.NullUUID
	Email      string
	ExpiresAt  time.Time
	ConsumedAt sql.NullTime
}

func (r *Repository) insertVerification(ctx context.Context, purpose string, userID uuid.UUID, email string, tokenHash []byte, expiresAt time.Time) error {
	if err := r.db.WithContext(ctx).Exec(
		`INSERT INTO verifications (purpose, user_id, email, token_hash, expires_at) VALUES (?, ?, ?, ?, ?)`,
		purpose, userID, email, tokenHash, expiresAt,
	).Error; err != nil {
		return fmt.Errorf("insert %s token: %w", purpose, err)
	}
	return nil
}

// countLiveVerifications counts the user's tokens of purpose that are neither used nor expired at now.
func (r *Repository) countLiveVerifications(ctx context.Context, userID uuid.UUID, purpose string, now time.Time) (int64, error) {
	var n int64
	if err := r.db.WithContext(ctx).Raw(
		`SELECT count(*) FROM verifications WHERE user_id = ? AND purpose = ? AND consumed_at IS NULL AND expires_at > ?`,
		userID, purpose, now,
	).Scan(&n).Error; err != nil {
		return 0, fmt.Errorf("count %s tokens: %w", purpose, err)
	}
	return n, nil
}

// deleteVerification deletes one token.
func (r *Repository) deleteVerification(ctx context.Context, purpose string, tokenHash []byte) error {
	if err := r.db.WithContext(ctx).Exec(`DELETE FROM verifications WHERE token_hash = ? AND purpose = ?`, tokenHash, purpose).Error; err != nil {
		return fmt.Errorf("delete %s token: %w", purpose, err)
	}
	return nil
}

// findVerification returns the token whatever its state, or nil when it does not exist.
func (r *Repository) findVerification(ctx context.Context, purpose string, tokenHash []byte) (*verification, error) {
	var rows []verification
	err := r.db.WithContext(ctx).Raw(
		`SELECT id, user_id, email, expires_at, consumed_at FROM verifications WHERE token_hash = ? AND purpose = ?`,
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

// consumeVerification marks a live token used and returns it, or nil when the token is unknown, expired or already used. The update is the claim: of two concurrent redemptions exactly one gets the row.
func (r *Repository) consumeVerification(ctx context.Context, purpose string, tokenHash []byte, now time.Time) (*verification, error) {
	var rows []verification
	err := r.db.WithContext(ctx).Raw(
		`UPDATE verifications SET consumed_at = ?
		 WHERE token_hash = ? AND purpose = ? AND consumed_at IS NULL AND expires_at > ?
		 RETURNING id, user_id, email, expires_at, consumed_at`,
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

// consumeUserVerification is consumeVerification restricted to tokens issued to userID; a token naming anyone else is left untouched.
func (r *Repository) consumeUserVerification(ctx context.Context, purpose string, tokenHash []byte, userID uuid.UUID, now time.Time) (*verification, error) {
	var rows []verification
	err := r.db.WithContext(ctx).Raw(
		`UPDATE verifications SET consumed_at = ?
		 WHERE token_hash = ? AND purpose = ? AND user_id = ? AND consumed_at IS NULL AND expires_at > ?
		 RETURNING id, user_id, email, expires_at, consumed_at`,
		now, tokenHash, purpose, userID, now,
	).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("consume %s token: %w", purpose, err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// deleteVerifications revokes every outstanding token of the given purposes issued to the user.
func (r *Repository) deleteVerifications(ctx context.Context, userID uuid.UUID, purposes ...string) error {
	if err := r.db.WithContext(ctx).Exec(
		`DELETE FROM verifications WHERE user_id = ? AND purpose IN ?`, userID, purposes,
	).Error; err != nil {
		return fmt.Errorf("revoke tokens: %w", err)
	}
	return nil
}
