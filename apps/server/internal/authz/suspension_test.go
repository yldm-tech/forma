package authz

import (
	"errors"
	"testing"

	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
)

func TestSuspensionRefusesTheMemberInThatOrganizationOnly(t *testing.T) {
	e := newTestEnv(t)
	org, other := e.org(), e.org()
	member, colleague, outsider := e.passwordUser(), e.user(), e.user()
	e.member(org, member, "manager")
	e.member(other, member, "owner")
	e.member(org, colleague, "owner")
	ws, otherWS := e.workspace(org), e.workspace(other)
	survey := e.survey(ws)
	key := e.apiKey(org, true, true)
	e.apiKeyWorkspace(key, ws, "read")
	e.drain()

	e.expect(User(member), Workspace(ws), map[Action]bool{WorkspaceRead: true, WorkspaceManage: true})

	e.exec(`UPDATE memberships SET suspended_at = now() WHERE user_id = ? AND organization_id = ?`, member, org)
	// Refused at once, before any projection runs: the SpiceDB relationships are untouched.
	e.expect(User(member), Workspace(ws), map[Action]bool{WorkspaceRead: false, WorkspaceManage: false})
	e.expect(User(member), Survey(survey), map[Action]bool{SurveyRead: false})
	e.expect(User(member), Organization(org), map[Action]bool{OrganizationRead: false, OrganizationManage: false, OrganizationManageAccess: false})
	if ok, err := e.authorizer.CanReadOrganizationDespiteRefusal(e.ctx, User(member), org); err != nil || !ok {
		t.Fatalf("read despite the suspension = %v, %v", ok, err)
	}
	if ok, err := e.authorizer.CanReadOrganizationDespiteRefusal(e.ctx, User(outsider), org); err != nil || ok {
		t.Fatalf("a non-member's read despite a refusal = %v, %v", ok, err)
	}
	// The member's other organization, their colleagues and the organization's API keys are not affected.
	e.expect(User(member), Workspace(otherWS), map[Action]bool{WorkspaceRead: true, WorkspaceManage: true})
	e.expect(User(member), Organization(other), map[Action]bool{OrganizationWrite: true})
	e.expect(User(colleague), Workspace(ws), map[Action]bool{WorkspaceManage: true})
	e.expect(APIKey(key), Workspace(ws), map[Action]bool{WorkspaceRead: true})

	// Require names the suspension and the organization, ahead of the two-factor requirement; a non-member is only told no.
	e.exec(`UPDATE organizations SET two_factor_enforced_at = now() - interval '1 minute' WHERE id = ?`, org)
	var ae *apperr.Error
	if err := e.authorizer.Require(e.ctx, User(member), WorkspaceRead, Workspace(ws)); !errors.As(err, &ae) || ae.Code != CodeMembershipSuspended || ae.Details["organizationId"] != org.String() {
		t.Fatalf("Require = %v", err)
	}
	if err := e.authorizer.Require(e.ctx, User(outsider), WorkspaceRead, Workspace(ws)); err == nil || err.Error() != "forbidden: Not authorized" {
		t.Fatalf("a non-member's Require = %v", err)
	}
	if refusal, err := e.authorizer.Refusal(e.ctx, outsider, org); err != nil || refusal != nil {
		t.Fatalf("a non-member's refusal = %v, %v", refusal, err)
	}
	e.exec(`UPDATE organizations SET two_factor_enforced_at = NULL WHERE id = ?`, org)

	suspended, err := e.authorizer.SuspendedOrganizations(e.ctx, member)
	if err != nil || len(suspended) != 1 || suspended[0] != org {
		t.Fatalf("suspended organizations = %v, %v", suspended, err)
	}
	orgs, err := e.authorizer.LookupOrganizationIDs(e.ctx, User(member))
	if err != nil || len(orgs) != 2 {
		t.Fatalf("organizations = %v, %v; a suspended member still sees the organization", orgs, err)
	}
	workspaces, err := e.authorizer.LookupWorkspaceIDs(e.ctx, User(member), false)
	if err != nil || len(workspaces) != 1 || workspaces[0] != otherWS {
		t.Fatalf("workspaces = %v, %v, want only %s", workspaces, err, otherWS)
	}

	// Restoring gives everything back at once.
	e.exec(`UPDATE memberships SET suspended_at = NULL WHERE user_id = ? AND organization_id = ?`, member, org)
	e.expect(User(member), Workspace(ws), map[Action]bool{WorkspaceRead: true, WorkspaceManage: true})
	workspaces, err = e.authorizer.LookupWorkspaceIDs(e.ctx, User(member), false)
	if err != nil || len(workspaces) != 2 {
		t.Fatalf("workspaces after restoring = %v, %v", workspaces, err)
	}

	// An inactive account is refused for that, not for a suspension.
	e.exec(`UPDATE memberships SET suspended_at = now() WHERE user_id = ? AND organization_id = ?`, member, org)
	e.exec(`UPDATE users SET is_active = false WHERE id = ?`, member)
	if refusal, err := e.authorizer.Refusal(e.ctx, member, org); err != nil || refusal != nil {
		t.Fatalf("an inactive member's refusal = %v, %v", refusal, err)
	}
}
