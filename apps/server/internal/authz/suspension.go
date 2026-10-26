package authz

import (
	"context"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
)

// An owner or manager may suspend a member's access to the organization (memberships.suspended_at, owner decision D-AO01). A suspension belongs to one membership: the user keeps their account, their sessions and every other organization. Like the two-factor requirement it is enforced at the PostgreSQL tenant boundary (refusal.go), so it holds for the admin API, v3 and MCP through OAuth alike, from the next request on and without waiting for the SpiceDB projection, which a suspension leaves alone so that restoring it gives every relationship back at once.

// CodeMembershipSuspended is the refusal of a member whose access to the organization is suspended; Details.organizationId names the organization.
const CodeMembershipSuspended = "organization_membership_suspended"

// ErrMembershipSuspended is that refusal for the organization.
func ErrMembershipSuspended(organizationID uuid.UUID) *apperr.Error {
	return &apperr.Error{
		Status:  http.StatusForbidden,
		Code:    CodeMembershipSuspended,
		Message: "Your access to this organization is suspended. Ask one of its owners or managers to restore it.",
		Details: map[string]any{"organizationId": organizationID.String()},
	}
}

// SuspendedOrganizations lists the organizations that suspended the user's membership.
func (a *Authorizer) SuspendedOrganizations(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error) {
	return suspendedOrganizations(ctx, a.db, userID)
}

func suspendedOrganizations(ctx context.Context, db *gorm.DB, userID uuid.UUID) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	err := db.WithContext(ctx).Raw(`SELECT organization_id FROM memberships WHERE user_id = ? AND suspended_at IS NOT NULL ORDER BY organization_id`, userID).Scan(&ids).Error
	if err != nil {
		return nil, fmt.Errorf("resolve membership suspensions: %w", err)
	}
	return ids, nil
}
