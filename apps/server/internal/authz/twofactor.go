package authz

import (
	"context"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
)

// An organization may require two-factor authentication of its members (organizations.two_factor_enforced_at). The requirement is enforced here, at the PostgreSQL tenant boundary, so it holds for every surface a user acts on, the admin API, v3 and MCP through OAuth alike: once the organization's grace period is over, a member who must enrol and has not is refused every decision in the organization, and the lookups leave its workspaces out. The one exception is CanReadOrganizationDespiteRefusal, which only the guide to enrolment (the organization's name and the caller's membership) and leaving the organization use. API keys are not users and are never refused for it. A suspension (suspension.go) is the organization's other refusal of a member, and follows the same rules.

// A user's two-factor status, as SecondFactorStatusSQL computes it.
const (
	// SecondFactorEnabled is a user with a verified authenticator.
	SecondFactorEnabled = "enabled"
	// SecondFactorDisabled is a user who can sign in with a password and has no verified authenticator: the one a requirement applies to, whether or not the account also has external identities, since the password alone still signs it in.
	SecondFactorDisabled = "disabled"
	// SecondFactorSSO is a user without a password, who can only sign in through an external identity provider and so cannot enrol here; a requirement exempts them.
	SecondFactorSSO = "sso"
)

// SecondFactorStatusSQL is a SQL expression for the two-factor status of the user whose id the column userID holds ("u.id"). What counts is what a sign-in can use: an account with a credential password signs in with it alone (auth's checkCredentials), external identities or not, so only an account without one is "sso".
func SecondFactorStatusSQL(userID string) string {
	return `(CASE WHEN EXISTS (SELECT 1 FROM user_totp t WHERE t.user_id = ` + userID + ` AND t.verified) THEN '` + SecondFactorEnabled + `'
		WHEN EXISTS (SELECT 1 FROM user_identities i WHERE i.user_id = ` + userID + ` AND i.provider = 'credential' AND i.password_hash <> '') THEN '` + SecondFactorDisabled + `'
		ELSE '` + SecondFactorSSO + `' END)`
}

// CodeTwoFactorRequired is the refusal of a member whose organization requires two-factor authentication they have not turned on; Details.organizationId names the organization.
const CodeTwoFactorRequired = "organization_requires_two_factor"

// ErrTwoFactorRequired is that refusal for the organization.
func ErrTwoFactorRequired(organizationID uuid.UUID) *apperr.Error {
	return &apperr.Error{
		Status:  http.StatusForbidden,
		Code:    CodeTwoFactorRequired,
		Message: "This organization requires two-factor authentication. Turn it on in your profile to continue.",
		Details: map[string]any{"organizationId": organizationID.String()},
	}
}

// SecondFactorBlockedOrganizations lists the organizations of the user that refuse them for want of two-factor authentication: those whose grace period is over, while the user must enrol and has not.
func (a *Authorizer) SecondFactorBlockedOrganizations(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error) {
	return secondFactorBlocked(ctx, a.db, userID)
}

func secondFactorBlocked(ctx context.Context, db *gorm.DB, userID uuid.UUID) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	err := db.WithContext(ctx).Raw(`SELECT m.organization_id FROM memberships m JOIN organizations o ON o.id = m.organization_id
		JOIN users u ON u.id = m.user_id
		WHERE m.user_id = ? AND o.two_factor_enforced_at <= now() AND `+SecondFactorStatusSQL("u.id")+` = ?
		ORDER BY m.organization_id`, userID, SecondFactorDisabled).Scan(&ids).Error
	if err != nil {
		return nil, fmt.Errorf("resolve two-factor requirements: %w", err)
	}
	return ids, nil
}
