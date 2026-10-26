package authz

import (
	"context"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
)

// An organization refuses a member it still counts as one in two cases: it suspended their membership (suspension.go), or its two-factor requirement applies to them and they have not enrolled (twofactor.go). Both are enforced at the PostgreSQL tenant boundary, in every decision a user asks in the organization (userAllowed) and in the lookups, which leave its workspaces out; organizations stay listed, so the member can find the one that refuses them and learn why. API keys are not users and are refused neither way here; apikey.Authenticator refuses a key whose creator's membership is suspended.

// CanReadOrganizationDespiteRefusal is organization.read on the organization with its refusals of the member set aside: the one decision a refused member keeps. Only the organization's own page (its name and the caller's membership, which the guide to enrolment and the suspension notice show), the navigation that sends the member to them, and leaving the organization use it. Every other organization.read, the billing summary and AI availability among them, goes through Can and refuses the member like any other decision.
func (a *Authorizer) CanReadOrganizationDespiteRefusal(ctx context.Context, actor Actor, organizationID uuid.UUID) (bool, error) {
	return a.can(ctx, actor, OrganizationRead, Organization(organizationID), false)
}

// userAllowed is userActive for a decision in an organization: an existing, active user who, unless refusals is false, is neither suspended by the organization nor refused by its two-factor requirement.
func userAllowed(ctx context.Context, db *gorm.DB, userID, organizationID uuid.UUID, refusals bool) (bool, error) {
	if !refusals {
		return userActive(ctx, db, userID)
	}
	var rows []struct {
		IsActive  bool
		Blocked   bool
		Suspended bool
	}
	err := db.WithContext(ctx).Raw(`SELECT u.is_active,
		EXISTS (SELECT 1 FROM organizations o WHERE o.id = ? AND o.two_factor_enforced_at <= now()) AND `+SecondFactorStatusSQL("u.id")+` = ? AS blocked,
		EXISTS (SELECT 1 FROM memberships m WHERE m.user_id = u.id AND m.organization_id = ? AND m.suspended_at IS NOT NULL) AS suspended
		FROM users u WHERE u.id = ?`, organizationID, SecondFactorDisabled, organizationID, userID).Scan(&rows).Error
	if err != nil {
		return false, fmt.Errorf("resolve authorization user: %w", err)
	}
	return len(rows) == 1 && rows[0].IsActive && !rows[0].Blocked && !rows[0].Suspended, nil
}

// Refusal is the error to answer a user's denied request in the organization with: ErrMembershipSuspended when the organization suspended them, else ErrTwoFactorRequired when its two-factor requirement is what refuses them, nil otherwise. A suspension comes first: turning on two-factor authentication would not help a suspended member. Only a member is told; anyone else learns nothing about the organization. Services that turn a denial into their own error call it first, so the browser learns why and can show the member the way out.
func (a *Authorizer) Refusal(ctx context.Context, userID, organizationID uuid.UUID) (*apperr.Error, error) {
	// An inactive user is refused for that, not for these.
	active, err := userActive(ctx, a.db, userID)
	if err != nil || !active {
		return nil, err
	}
	suspended, err := suspendedOrganizations(ctx, a.db, userID)
	if err != nil {
		return nil, err
	}
	if slices.Contains(suspended, organizationID) {
		return ErrMembershipSuspended(organizationID), nil
	}
	blocked, err := secondFactorBlocked(ctx, a.db, userID)
	if err != nil {
		return nil, err
	}
	if slices.Contains(blocked, organizationID) {
		return ErrTwoFactorRequired(organizationID), nil
	}
	return nil, nil
}

// denial is Require's refusal: the organization's Refusal when one of its refusals denied a user, "Not authorized" otherwise.
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
	refusal, err := a.Refusal(ctx, actor.ID, s.organizationID)
	if err != nil {
		return err
	}
	if refusal != nil {
		return refusal
	}
	return denied
}

// withoutRefusedOrganizations drops from a user's looked-up workspaces those of organizations that refuse the user: that suspended them, or whose two-factor requirement refuses them.
func withoutRefusedOrganizations(ctx context.Context, db *gorm.DB, userID uuid.UUID, ids []uuid.UUID) ([]uuid.UUID, error) {
	if len(ids) == 0 {
		return ids, nil
	}
	suspended, err := suspendedOrganizations(ctx, db, userID)
	if err != nil {
		return nil, err
	}
	blocked, err := secondFactorBlocked(ctx, db, userID)
	if err != nil {
		return nil, err
	}
	refused := slices.Concat(suspended, blocked)
	if len(refused) == 0 {
		return ids, nil
	}
	var rows []string
	err = db.WithContext(ctx).Raw(`SELECT id::text FROM workspaces WHERE id = ANY(?::uuid[]) AND NOT (organization_id = ANY(?::uuid[]))`, uuidArray(ids), uuidArray(refused)).Scan(&rows).Error
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
