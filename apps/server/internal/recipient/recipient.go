// Package recipient is the rule every survey-response email follows for whom it may name by address: a `to` that is itself an address, as opposed to a respondent field, must belong to someone with access to the survey's workspace. A workflow's send_email step and a survey's follow-ups both send a response to their recipient, so neither may mail a workspace's responses to an outside account, and one addressed to a member stops when that member loses access.
package recipient

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/platform/jsvalue"
	"github.com/yldm-tech/forma/apps/server/internal/platform/userinput"
)

// IsLiteral is forma2's isLiteralEmailRecipient: a `to` that is itself an address, as opposed to the id of the element or hidden field whose answer holds the respondent's.
func IsLiteral(to string) bool {
	return userinput.ValidEmailFormat(to)
}

// Normalize is forma2's normalizeEmailForComparison.
func Normalize(email string) string {
	return strings.ToLower(jsvalue.Trim(email))
}

// Member is someone who can access a workspace, as the recipient pickers list them.
type Member struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// Members is forma2's getWorkspaceMembers: the active users of the workspace's organization that it does not refuse (neither suspended nor refused by its two-factor requirement, authz.MemberNotRefusedSQL) who are its owners or managers, or who are on a team with access to the workspace. It is also the allowlist of literal recipients, so a refused member is not mailed the responses the app refuses them.
func Members(ctx context.Context, db *gorm.DB, workspaceID uuid.UUID) ([]Member, error) {
	var rows []Member
	if err := db.WithContext(ctx).Raw(`SELECT u.name, u.email FROM memberships m
		JOIN workspaces w ON w.organization_id = m.organization_id
		JOIN users u ON u.id = m.user_id
		WHERE w.id = ? AND u.is_active AND `+authz.MemberNotRefusedSQL("m")+` AND u.email <> ''
		AND (m.role IN ('owner', 'manager') OR EXISTS (
			SELECT 1 FROM team_members tm JOIN workspace_teams wt ON wt.team_id = tm.team_id WHERE tm.user_id = u.id AND wt.workspace_id = w.id))
		ORDER BY u.name, u.email`, workspaceID).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load workspace members: %w", err)
	}
	return rows, nil
}

// Allowlist is forma2's getWorkspaceMemberEmails: the addresses a literal recipient may be, normalized for comparison.
func Allowlist(ctx context.Context, db *gorm.DB, workspaceID uuid.UUID) (map[string]bool, error) {
	members, err := Members(ctx, db, workspaceID)
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(members))
	for _, m := range members {
		out[Normalize(m.Email)] = true
	}
	return out, nil
}
