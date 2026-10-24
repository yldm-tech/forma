package authz

import (
	"testing"

	"github.com/google/uuid"
)

func TestProjectsMembershipRolesAndTheirRevocation(t *testing.T) {
	e := newTestEnv(t)
	org, owner, manager, member, billing := e.org(), e.user(), e.user(), e.user(), e.user()
	e.member(org, owner, "owner")
	e.member(org, manager, "manager")
	e.member(org, member, "member")
	e.member(org, billing, "billing")
	e.drain()

	o := Organization(org)
	e.expect(User(owner), o, map[Action]bool{OrganizationRead: true, OrganizationWrite: true, OrganizationManage: true, OrganizationManageAccess: true, OrganizationManageAPIKeys: true, OrganizationManageBilling: true})
	e.expect(User(manager), o, map[Action]bool{OrganizationRead: true, OrganizationWrite: false, OrganizationManage: true, OrganizationManageAccess: true, OrganizationManageBilling: true})
	e.expect(User(member), o, map[Action]bool{OrganizationRead: true, OrganizationReadAccess: true, OrganizationManage: false, OrganizationManageAccess: false, OrganizationManageBilling: false})
	e.expect(User(billing), o, map[Action]bool{OrganizationRead: true, OrganizationReadAccess: false, OrganizationManageBilling: true, OrganizationManage: false})

	// A demotion is a revocation: the old role's relation is deleted, not left beside the new one.
	e.exec(`UPDATE memberships SET role = 'member' WHERE organization_id = ? AND user_id = ?`, org, manager)
	if e.countOutbox(`target_type = 'membership' AND is_revocation AND processed_at IS NULL`) != 1 {
		t.Fatal("demotion was not enqueued as a revocation")
	}
	e.drain()
	e.expect(User(manager), o, map[Action]bool{OrganizationRead: true, OrganizationManage: false})
	if rels := e.relationships("organization", org); len(rels) != 4 {
		t.Fatalf("organization holds %d relationships, want one per membership: %+v", len(rels), rels)
	}

	e.exec(`DELETE FROM memberships WHERE organization_id = ? AND user_id = ?`, org, member)
	e.drain()
	e.expect(User(member), o, map[Action]bool{OrganizationRead: false})
}

func TestUserManagementMinimumRole(t *testing.T) {
	e := newTestEnv(t)
	org, owner, manager := e.org(), e.user(), e.user()
	e.member(org, owner, "owner")
	e.member(org, manager, "manager")
	e.drain()
	for role, want := range map[string][2]bool{"manager": {true, true}, "owner": {true, false}, "disabled": {false, false}} {
		e.authorizer.userManagementMinimumRole = role
		if got := [2]bool{e.can(User(owner), OrganizationManageAccess, Organization(org)), e.can(User(manager), OrganizationManageAccess, Organization(org))}; got != want {
			t.Errorf("USER_MANAGEMENT_MINIMUM_ROLE=%s: owner, manager = %v, want %v", role, got, want)
		}
	}
}

func TestDeletedOrganizationLosesItsRelationships(t *testing.T) {
	e := newTestEnv(t)
	org, user := e.org(), e.user()
	e.member(org, user, "owner")
	ws := e.workspace(org)
	e.drain()
	if len(e.relationships("organization", org)) == 0 || !e.can(User(user), WorkspaceManage, Workspace(ws)) {
		t.Fatal("setup not projected")
	}
	e.exec(`DELETE FROM organizations WHERE id = ?`, org)
	e.drain()
	if rels := append(e.relationships("organization", org), e.relationships("workspace", ws)...); len(rels) != 0 {
		t.Fatalf("relationships survive the organization: %+v", rels)
	}
}

func TestDeactivatedUserLosesEveryRelationshipAndRegainsThemOnReactivation(t *testing.T) {
	e := newTestEnv(t)
	org, user := e.org(), e.user()
	e.member(org, user, "member")
	team, ws := e.team(org), e.workspace(org)
	e.teamMember(team, user, "contributor")
	e.workspaceTeam(ws, team, "read")
	e.drain()
	if !e.can(User(user), WorkspaceRead, Workspace(ws)) {
		t.Fatal("setup not projected")
	}

	e.exec(`UPDATE users SET is_active = false WHERE id = ?`, user)
	e.drain()
	// Denied by PostgreSQL first, and the graph no longer holds anything for the user either.
	if e.can(User(user), WorkspaceRead, Workspace(ws)) || e.spicedbCheck("organization", org, "read", "user", user) || e.spicedbCheck("team", team, "member", "user", user) {
		t.Fatal("deactivated user kept access")
	}

	e.exec(`UPDATE users SET is_active = true WHERE id = ?`, user)
	if e.countOutbox(`target_type = 'user' AND NOT is_revocation AND processed_at IS NULL`) != 1 {
		t.Fatal("reactivation was not enqueued as a grant")
	}
	e.drain()
	if !e.can(User(user), WorkspaceRead, Workspace(ws)) {
		t.Fatal("reactivated user did not regain access")
	}

	e.exec(`DELETE FROM users WHERE id = ?`, user)
	e.drain()
	if e.spicedbCheck("organization", org, "read", "user", user) || e.spicedbCheck("team", team, "member", "user", user) {
		t.Fatal("deleted user kept relationships")
	}
}

func TestProjectsTeamsWorkspacesAndTheirGrants(t *testing.T) {
	e := newTestEnv(t)
	org, owner, admin, contributor, outsider, billing := e.org(), e.user(), e.user(), e.user(), e.user(), e.user()
	e.member(org, owner, "owner")
	e.member(org, admin, "member")
	e.member(org, contributor, "member")
	e.member(org, outsider, "member")
	e.member(org, billing, "billing")
	team, ws, other := e.team(org), e.workspace(org), e.workspace(org)
	e.teamMember(team, admin, "admin")
	e.teamMember(team, contributor, "contributor")
	e.teamMember(team, billing, "contributor")
	e.workspaceTeam(ws, team, "readWrite")
	e.drain()

	w := Workspace(ws)
	e.expect(User(owner), w, map[Action]bool{WorkspaceManage: true, WorkspaceShare: true})
	e.expect(User(contributor), w, map[Action]bool{WorkspaceRead: true, WorkspaceWrite: true, WorkspaceManage: false})
	e.expect(User(outsider), w, map[Action]bool{WorkspaceRead: false})
	// A billing member gets nothing through a team.
	e.expect(User(billing), w, map[Action]bool{WorkspaceRead: false})
	e.expect(User(contributor), Workspace(other), map[Action]bool{WorkspaceRead: false})
	e.expect(User(admin), Team(team), map[Action]bool{TeamRead: true, TeamManage: true, TeamDelete: false})
	e.expect(User(contributor), Team(team), map[Action]bool{TeamRead: true, TeamManage: false})
	e.expect(User(owner), Team(team), map[Action]bool{TeamManage: true, TeamDelete: true})

	// Surveys and responses are evaluated on their workspace.
	survey := e.survey(ws)
	response := e.response(survey)
	e.expect(User(contributor), Survey(survey), map[Action]bool{SurveyRead: true, SurveyWrite: true, SurveyDelete: true, SurveyPublish: true, SurveyManage: false, SurveyResponseExport: true})
	e.expect(User(contributor), Response(response), map[Action]bool{ResponseRead: true, ResponseWrite: true, ResponseManage: false})
	e.expect(User(outsider), Survey(survey), map[Action]bool{SurveyRead: false})

	// Downgrading the grant revokes write.
	e.exec(`UPDATE workspace_teams SET permission = 'read' WHERE workspace_id = ? AND team_id = ?`, ws, team)
	e.drain()
	e.expect(User(contributor), w, map[Action]bool{WorkspaceRead: true, WorkspaceWrite: false})

	// Moving a team member to another team revokes the old pair.
	second := e.team(org)
	e.exec(`UPDATE team_members SET team_id = ? WHERE team_id = ? AND user_id = ?`, second, team, contributor)
	e.drain()
	e.expect(User(contributor), w, map[Action]bool{WorkspaceRead: false})

	// Deleting the team removes its grants everywhere.
	e.exec(`DELETE FROM teams WHERE id = ?`, team)
	e.drain()
	e.expect(User(admin), w, map[Action]bool{WorkspaceRead: false})
	if rels := e.relationships("team", team); len(rels) != 0 {
		t.Fatalf("deleted team keeps %+v", rels)
	}

	e.exec(`DELETE FROM workspaces WHERE id = ?`, ws)
	e.drain()
	if rels := e.relationships("workspace", ws); len(rels) != 0 {
		t.Fatalf("deleted workspace keeps %+v", rels)
	}
}

func TestCrossOrganizationTeamGrantIsNeverProjected(t *testing.T) {
	e := newTestEnv(t)
	orgA, orgB, user := e.org(), e.org(), e.user()
	e.member(orgA, user, "member")
	team := e.team(orgA)
	e.teamMember(team, user, "contributor")
	foreign := e.workspace(orgB)
	e.workspaceTeam(foreign, team, "manage")
	e.drain()
	if e.spicedbCheck("workspace", foreign, "read", "user", user) {
		t.Fatal("a team of another organization reached the workspace")
	}
}

func TestMovingAWorkspaceReplacesItsOrganizationParent(t *testing.T) {
	e := newTestEnv(t)
	orgA, orgB, ownerA, ownerB := e.org(), e.org(), e.user(), e.user()
	e.member(orgA, ownerA, "owner")
	e.member(orgB, ownerB, "owner")
	ws := e.workspace(orgA)
	e.drain()
	e.exec(`UPDATE workspaces SET organization_id = ? WHERE id = ?`, orgB, ws)
	e.drain()
	if e.spicedbCheck("workspace", ws, "manage", "user", ownerA) || !e.can(User(ownerB), WorkspaceManage, Workspace(ws)) {
		t.Fatal("the old organization kept its parent edge")
	}
}

func TestProjectsAPIKeysAndTheirGrants(t *testing.T) {
	e := newTestEnv(t)
	org, other, manager := e.org(), e.org(), e.user()
	e.member(org, manager, "manager")
	ws, ws2, foreign := e.workspace(org), e.workspace(org), e.workspace(other)
	reader := e.apiKey(org, true, false)
	writer := e.apiKey(org, false, true)
	e.apiKeyWorkspace(reader, ws, "write")
	e.apiKeyWorkspace(reader, foreign, "manage")
	e.drain()

	o := Organization(org)
	e.expect(APIKey(reader), o, map[Action]bool{OrganizationRead: true, OrganizationReadAccess: true, OrganizationManageAccess: false})
	// Write implies read; API keys are exempt from USER_MANAGEMENT_MINIMUM_ROLE.
	e.authorizer.userManagementMinimumRole = "disabled"
	e.expect(APIKey(writer), o, map[Action]bool{OrganizationRead: true, OrganizationReadAccess: true, OrganizationManageAccess: true})
	e.authorizer.userManagementMinimumRole = "manager"
	e.expect(APIKey(reader), Workspace(ws), map[Action]bool{WorkspaceRead: true, WorkspaceWrite: true, WorkspaceManage: false})
	e.expect(APIKey(reader), Workspace(ws2), map[Action]bool{WorkspaceRead: false})
	// A key is denied outside its organization whatever the graph holds, and a cross-organization grant is not even projected.
	e.expect(APIKey(reader), Workspace(foreign), map[Action]bool{WorkspaceRead: false})
	e.expect(APIKey(reader), Organization(other), map[Action]bool{OrganizationRead: false})
	if e.spicedbCheck("workspace", foreign, "read", "api_key", reader) {
		t.Fatal("cross-organization key grant was projected")
	}
	e.expect(User(manager), APIKeyResource(reader), map[Action]bool{APIKeyRead: true, APIKeyManage: true})

	ids, err := e.authorizer.LookupWorkspaceIDs(e.ctx, APIKey(reader), true)
	if err != nil || len(ids) != 1 || ids[0] != ws {
		t.Fatalf("LookupWorkspaceIDs = %v, %v", ids, err)
	}

	e.exec(`UPDATE api_keys SET organization_access = '{"accessControl":{"read":false,"write":false}}' WHERE id = ?`, writer)
	e.exec(`DELETE FROM api_key_workspaces WHERE api_key_id = ? AND workspace_id = ?`, reader, ws)
	e.drain()
	e.expect(APIKey(writer), o, map[Action]bool{OrganizationRead: false})
	e.expect(APIKey(reader), Workspace(ws), map[Action]bool{WorkspaceRead: false})

	e.exec(`DELETE FROM api_keys WHERE id = ?`, reader)
	e.drain()
	if e.spicedbCheck("organization", org, "read", "api_key", reader) || len(e.relationships("api_key", reader)) != 0 {
		t.Fatal("deleted key kept relationships")
	}
}

func TestMissingActorsAndResourcesAreDenials(t *testing.T) {
	e := newTestEnv(t)
	org, user := e.org(), e.user()
	e.member(org, user, "owner")
	e.drain()
	e.expect(User(uuid.New()), Organization(org), map[Action]bool{OrganizationRead: false})
	e.expect(User(user), Organization(uuid.New()), map[Action]bool{OrganizationRead: false})
	e.expect(User(user), Survey(uuid.New()), map[Action]bool{SurveyRead: false})
	e.expect(APIKey(uuid.New()), Organization(org), map[Action]bool{OrganizationRead: false})
	if err := e.authorizer.Require(e.ctx, User(uuid.New()), OrganizationRead, Organization(org)); err == nil || err.Error() != "forbidden: Not authorized" {
		t.Fatalf("Require = %v", err)
	}
	if _, err := e.authorizer.Can(e.ctx, User(user), WorkspaceRead, Organization(org)); err == nil {
		t.Fatal("an invalid action/resource combination answered")
	} else if ae, ok := AsError(err); !ok || ae.Code != CodeInternal || ae.Operation != "authorization" {
		t.Fatalf("invalid combination = %+v", err)
	}
}

func TestLookupsApplyTheActorRules(t *testing.T) {
	e := newTestEnv(t)
	orgA, orgB, user := e.org(), e.org(), e.user()
	e.member(orgA, user, "owner")
	e.member(orgB, user, "member")
	wsA := e.workspace(orgA)
	e.workspace(orgB)
	e.drain()

	orgs, err := e.authorizer.LookupOrganizationIDs(e.ctx, User(user))
	if err != nil || len(orgs) != 2 {
		t.Fatalf("LookupOrganizationIDs = %v, %v", orgs, err)
	}
	workspaces, err := e.authorizer.LookupWorkspaceIDs(e.ctx, User(user), false)
	if err != nil || len(workspaces) != 1 || workspaces[0] != wsA {
		t.Fatalf("LookupWorkspaceIDs = %v, %v", workspaces, err)
	}
	// An inactive user's tuples linger until the deactivation is delivered, and the list must not show them.
	e.exec(`UPDATE users SET is_active = false WHERE id = ?`, user)
	if orgs, err := e.authorizer.LookupOrganizationIDs(e.ctx, User(user)); err != nil || len(orgs) != 0 {
		t.Fatalf("inactive user lookup = %v, %v", orgs, err)
	}
}
