package authz

import (
	"context"
	"fmt"
	"net/http"
	"slices"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
)

// An organization may require two-factor authentication of its members (organizations.two_factor_enforced_at). The requirement is enforced here, at the PostgreSQL tenant boundary, so it holds for every surface a user acts on, the admin API, v3 and MCP through OAuth alike: once the organization's grace period is over, a member who must enrol and has not is refused every decision in the organization, and the lookups leave its workspaces out. The one exception is CanReadOrganizationDespiteSecondFactor, which only the guide to enrolment (the organization's name and the caller's membership) and leaving the organization use. API keys are not users and are never refused for it.

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

// CanReadOrganizationDespiteSecondFactor is organization.read on the organization with its two-factor requirement set aside: the one decision a member it refuses keeps. Only the organization's own page (its name and the caller's membership, which the guide to enrolment shows), the navigation that sends the member to that guide, and leaving the organization use it. Every other organization.read, the billing summary and AI availability among them, goes through Can and refuses the member like any other decision.
func (a *Authorizer) CanReadOrganizationDespiteSecondFactor(ctx context.Context, actor Actor, organizationID uuid.UUID) (bool, error) {
	return a.can(ctx, actor, OrganizationRead, Organization(organizationID), false)
}

// userAllowed is userActive for a decision in an organization: an existing, active user who, unless secondFactor is false, is not refused by the organization's two-factor requirement.
func userAllowed(ctx context.Context, db *gorm.DB, userID, organizationID uuid.UUID, secondFactor bool) (bool, error) {
	if !secondFactor {
		return userActive(ctx, db, userID)
	}
	var rows []struct {
		IsActive bool
		Blocked  bool
	}
	err := db.WithContext(ctx).Raw(`SELECT u.is_active,
		EXISTS (SELECT 1 FROM organizations o WHERE o.id = ? AND o.two_factor_enforced_at <= now()) AND `+SecondFactorStatusSQL("u.id")+` = ? AS blocked
		FROM users u WHERE u.id = ?`, organizationID, SecondFactorDisabled, userID).Scan(&rows).Error
	if err != nil {
		return false, fmt.Errorf("resolve authorization user: %w", err)
	}
	return len(rows) == 1 && rows[0].IsActive && !rows[0].Blocked, nil
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

// SecondFactorRefusal is the error to answer a user's denied request in the organization with: ErrTwoFactorRequired when the organization's requirement is what refuses them, nil otherwise. Only a member is told; anyone else learns nothing about the organization. Services that turn a denial into their own error call it first, so the browser learns why and can guide the user to enrolment.
func (a *Authorizer) SecondFactorRefusal(ctx context.Context, userID, organizationID uuid.UUID) (*apperr.Error, error) {
	blocked, err := secondFactorBlocked(ctx, a.db, userID)
	if err != nil || !slices.Contains(blocked, organizationID) {
		return nil, err
	}
	// An inactive user is refused for that, not for this.
	active, err := userActive(ctx, a.db, userID)
	if err != nil || !active {
		return nil, err
	}
	return ErrTwoFactorRequired(organizationID), nil
}

// denial is Require's refusal: ErrTwoFactorRequired when the requirement of the resource's organization denied a user, "Not authorized" otherwise.
func (a *Authorizer) denial(ctx context.Context, actor Actor, resource Resource) error {
	denied := apperr.Forbidden("Not authorized")
	if actor.Type != ActorUser {
		return denied
	}
	s, err := resourceScope(ctx, a.db, resource)
	if err != nil {
		return err
	}
	if s == nil {
		return denied
	}
	refusal, err := a.SecondFactorRefusal(ctx, actor.ID, s.organizationID)
	if err != nil {
		return err
	}
	if refusal != nil {
		return refusal
	}
	return denied
}

// withoutSecondFactorBlocked drops from a user's looked-up workspaces those of organizations that refuse the user for want of two-factor authentication.
func withoutSecondFactorBlocked(ctx context.Context, db *gorm.DB, userID uuid.UUID, ids []uuid.UUID) ([]uuid.UUID, error) {
	if len(ids) == 0 {
		return ids, nil
	}
	blocked, err := secondFactorBlocked(ctx, db, userID)
	if err != nil || len(blocked) == 0 {
		return ids, err
	}
	var rows []string
	err = db.WithContext(ctx).Raw(`SELECT id::text FROM workspaces WHERE id = ANY(?::uuid[]) AND NOT (organization_id = ANY(?::uuid[]))`, uuidArray(ids), uuidArray(blocked)).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("filter looked-up workspaces: %w", err)
	}
	kept := make(map[uuid.UUID]bool, len(rows))
	for _, r := range rows {
		kept[uuid.MustParse(r)] = true
	}
	out := make([]uuid.UUID, 0, len(rows))
	// The lookup's order is kept.
	for _, id := range ids {
		if kept[id] {
			out = append(out, id)
		}
	}
	return out, nil
}
