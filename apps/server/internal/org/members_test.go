package org

import (
	"net/http"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
)

func memberPath(org, user uuid.UUID) string {
	return orgPath(org, "/members/"+user.String())
}

func (e *env) role(org, user uuid.UUID) string {
	e.t.Helper()
	var role string
	if err := e.db.Raw(`SELECT role FROM memberships WHERE organization_id = ? AND user_id = ?`, org, user).Row().Scan(&role); err != nil {
		return ""
	}
	return role
}

func TestListMembers(t *testing.T) {
	e := newEnv(t)
	owner, member, billing, outsider := e.user(), e.user(), e.user(), e.user()
	c := e.organization(owner, "Acme")
	e.member(c.ID, member, RoleMember)
	e.member(c.ID, billing, RoleBilling)
	e.exec(`UPDATE users SET is_active = false WHERE id = ?`, billing)

	var res membersResponse
	e.ok(e.do(member, http.MethodGet, orgPath(c.ID, "/members"), nil), http.StatusOK, &res)
	if len(res.Members) != 3 || res.Members[0].UserID != owner || res.Members[0].Role != RoleOwner || res.Members[2].IsActive {
		t.Fatalf("members %+v", res.Members)
	}
	// The members page needs organization.read_access, which billing members lack.
	e.fails(e.do(outsider, http.MethodGet, orgPath(c.ID, "/members"), nil), http.StatusForbidden, apperr.CodeForbidden, "")
	e.exec(`UPDATE users SET is_active = true WHERE id = ?`, billing)
	e.fails(e.do(billing, http.MethodGet, orgPath(c.ID, "/members"), nil), http.StatusForbidden, apperr.CodeForbidden, "")
}

// The rules of forma2's updateMembershipAction, one case each.
func TestChangeRole(t *testing.T) {
	type fixture struct {
		org                              uuid.UUID
		owner, owner2, manager, manager2 uuid.UUID
		member, billing, outsider        uuid.UUID
	}
	setup := func(e *env) fixture {
		f := fixture{owner: e.user(), owner2: e.user(), manager: e.user(), manager2: e.user(), member: e.user(), billing: e.user(), outsider: e.user()}
		f.org = e.organization(f.owner, "Acme").ID
		e.member(f.org, f.manager, RoleManager)
		e.member(f.org, f.manager2, RoleManager)
		e.member(f.org, f.member, RoleMember)
		e.member(f.org, f.billing, RoleBilling)
		return f
	}
	cases := []struct {
		name      string
		configure func(*config.Config)
		prepare   func(e *env, f fixture)
		actor     func(f fixture) uuid.UUID
		target    func(f fixture) uuid.UUID
		role      string
		status    int
		code      string
		message   string
	}{
		{name: "owner promotes a member", actor: func(f fixture) uuid.UUID { return f.owner }, target: func(f fixture) uuid.UUID { return f.member }, role: RoleManager, status: http.StatusOK},
		{name: "manager demotes another manager", actor: func(f fixture) uuid.UUID { return f.manager }, target: func(f fixture) uuid.UUID { return f.manager2 }, role: RoleMember, status: http.StatusOK},
		{name: "manager may only assign member", actor: func(f fixture) uuid.UUID { return f.manager }, target: func(f fixture) uuid.UUID { return f.member }, role: RoleManager,
			status: http.StatusForbidden, code: apperr.CodeOperationNotAllowed, message: "Managers can only assign users to the member role"},
		{name: "manager may not touch an owner", actor: func(f fixture) uuid.UUID { return f.manager }, target: func(f fixture) uuid.UUID { return f.owner }, role: RoleMember,
			status: http.StatusForbidden, code: apperr.CodeOperationNotAllowed, message: "Only owners can change the role of an owner"},
		{name: "billing is a Cloud role", actor: func(f fixture) uuid.UUID { return f.owner }, target: func(f fixture) uuid.UUID { return f.member }, role: RoleBilling,
			status: http.StatusBadRequest, code: apperr.CodeValidation, message: "Billing role is not allowed"},
		{name: "billing on Cloud", configure: func(c *config.Config) { c.IsFormaCloud = true }, actor: func(f fixture) uuid.UUID { return f.owner }, target: func(f fixture) uuid.UUID { return f.member }, role: RoleBilling, status: http.StatusOK},
		{name: "last owner cannot be demoted", actor: func(f fixture) uuid.UUID { return f.owner }, target: func(f fixture) uuid.UUID { return f.owner }, role: RoleManager,
			status: http.StatusBadRequest, code: apperr.CodeValidation, message: "You cannot demote the last owner of the organization"},
		{name: "an owner is demoted while another active owner remains", prepare: func(e *env, f fixture) { e.member(f.org, f.owner2, RoleOwner) },
			actor: func(f fixture) uuid.UUID { return f.owner }, target: func(f fixture) uuid.UUID { return f.owner2 }, role: RoleMember, status: http.StatusOK},
		{name: "an inactive second owner does not count", prepare: func(e *env, f fixture) {
			e.member(f.org, f.owner2, RoleOwner)
			e.exec(`UPDATE users SET is_active = false WHERE id = ?`, f.owner2)
		}, actor: func(f fixture) uuid.UUID { return f.owner }, target: func(f fixture) uuid.UUID { return f.owner }, role: RoleMember,
			status: http.StatusBadRequest, code: apperr.CodeValidation, message: "You cannot demote the last owner of the organization"},
		{name: "keeping an owner's role", actor: func(f fixture) uuid.UUID { return f.owner }, target: func(f fixture) uuid.UUID { return f.owner }, role: RoleOwner, status: http.StatusOK},
		{name: "a member has no user management", actor: func(f fixture) uuid.UUID { return f.member }, target: func(f fixture) uuid.UUID { return f.billing }, role: RoleMember,
			status: http.StatusForbidden, code: apperr.CodeOperationNotAllowed, message: "User management is not allowed for your role"},
		{name: "USER_MANAGEMENT_MINIMUM_ROLE=owner shuts managers out", configure: func(c *config.Config) { c.UserManagementMinimumRole = "owner" },
			actor: func(f fixture) uuid.UUID { return f.manager }, target: func(f fixture) uuid.UUID { return f.member }, role: RoleMember,
			status: http.StatusForbidden, code: apperr.CodeOperationNotAllowed, message: "User management is not allowed for your role"},
		{name: "USER_MANAGEMENT_MINIMUM_ROLE=disabled shuts owners out", configure: func(c *config.Config) { c.UserManagementMinimumRole = "disabled" },
			actor: func(f fixture) uuid.UUID { return f.owner }, target: func(f fixture) uuid.UUID { return f.member }, role: RoleManager,
			status: http.StatusForbidden, code: apperr.CodeOperationNotAllowed, message: "User management is not allowed for your role"},
		{name: "a non-member", actor: func(f fixture) uuid.UUID { return f.outsider }, target: func(f fixture) uuid.UUID { return f.member }, role: RoleManager,
			status: http.StatusForbidden, code: apperr.CodeForbidden, message: "User not a member of this organization"},
		{name: "a target outside the organization", actor: func(f fixture) uuid.UUID { return f.owner }, target: func(f fixture) uuid.UUID { return f.outsider }, role: RoleMember,
			status: http.StatusNotFound, code: apperr.CodeNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var configure []func(*config.Config)
			if tc.configure != nil {
				configure = append(configure, tc.configure)
			}
			e := newEnv(t, configure...)
			f := setup(e)
			if tc.prepare != nil {
				tc.prepare(e, f)
			}
			before := e.role(f.org, tc.target(f))
			rec := e.do(tc.actor(f), http.MethodPatch, memberPath(f.org, tc.target(f)), map[string]string{"role": tc.role})
			if tc.status != http.StatusOK {
				e.fails(rec, tc.status, tc.code, tc.message)
				if got := e.role(f.org, tc.target(f)); got != before {
					t.Fatalf("role changed to %q on a refused request", got)
				}
				return
			}
			var m Membership
			e.ok(rec, http.StatusOK, &m)
			if m.Role != tc.role || e.role(f.org, tc.target(f)) != tc.role {
				t.Fatalf("role %q, want %q", m.Role, tc.role)
			}
		})
	}
}

func TestPromotionMakesTheUserAdminOfTheirTeams(t *testing.T) {
	e := newEnv(t)
	owner, member := e.user(), e.user()
	c := e.organization(owner, "Acme")
	other := e.organization(e.user(), "Other")
	e.member(c.ID, member, RoleMember)
	e.member(other.ID, member, RoleMember)
	team, foreignTeam := e.team(c.ID), e.team(other.ID)
	e.teamMember(team, member)
	e.teamMember(foreignTeam, member)

	e.ok(e.do(owner, http.MethodPatch, memberPath(c.ID, member), map[string]string{"role": RoleManager}), http.StatusOK, nil)
	if e.count(`SELECT count(*) FROM team_members WHERE team_id = ? AND user_id = ? AND role = 'admin'`, team, member) != 1 {
		t.Fatal("team membership was not promoted to admin")
	}
	if e.count(`SELECT count(*) FROM team_members WHERE team_id = ? AND user_id = ? AND role = 'contributor'`, foreignTeam, member) != 1 {
		t.Fatal("a team of another organization was touched")
	}
	events := e.auditEvents()
	last := events[len(events)-1]
	if last["action"] != "updated" || last["target"].(map[string]any)["id"] != member.String()+"-"+c.ID.String() {
		t.Fatalf("audit event %v", last)
	}
}

// Demotion from manager to member takes back the team admin seats promotion gave, in the organization only; a plain member's team admin seat survives their role being set to member again.
func TestDemotionMakesTheUserContributorOfTheirTeams(t *testing.T) {
	e := newEnv(t)
	owner, manager, member := e.user(), e.user(), e.user()
	c := e.organization(owner, "Acme")
	other := e.organization(e.user(), "Other")
	e.member(c.ID, manager, RoleManager)
	e.member(other.ID, manager, RoleManager)
	e.member(c.ID, member, RoleMember)
	team, foreignTeam := e.team(c.ID), e.team(other.ID)
	e.exec(`INSERT INTO team_members (team_id, user_id, role) VALUES (?, ?, 'admin'), (?, ?, 'admin'), (?, ?, 'admin')`, team, manager, foreignTeam, manager, team, member)

	e.ok(e.do(owner, http.MethodPatch, memberPath(c.ID, manager), map[string]string{"role": RoleMember}), http.StatusOK, nil)
	e.ok(e.do(owner, http.MethodPatch, memberPath(c.ID, member), map[string]string{"role": RoleMember}), http.StatusOK, nil)
	if e.count(`SELECT count(*) FROM team_members WHERE team_id = ? AND user_id = ? AND role = 'contributor'`, team, manager) != 1 {
		t.Fatal("the demoted manager is still a team admin")
	}
	if e.count(`SELECT count(*) FROM team_members WHERE team_id = ? AND user_id = ? AND role = 'admin'`, foreignTeam, manager) != 1 {
		t.Fatal("a team of another organization was touched")
	}
	if e.count(`SELECT count(*) FROM team_members WHERE team_id = ? AND user_id = ? AND role = 'admin'`, team, member) != 1 {
		t.Fatal("a plain member lost the team admin seat they were given")
	}
}

func TestOwnersDemotingEachOtherConcurrentlyKeepAnOwner(t *testing.T) {
	e := newEnv(t)
	a, b := e.user(), e.user()
	c := e.organization(a, "Acme")
	e.member(c.ID, b, RoleOwner)
	e.authz.Deliver(t)

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, pair := range [][2]uuid.UUID{{a, b}, {b, a}} {
		wg.Go(func() {
			_, errs[i] = e.svc.ChangeRole(t.Context(), pair[0], c.ID, pair[1], RoleMember)
		})
	}
	wg.Wait()
	if n := e.count(`SELECT count(*) FROM memberships WHERE organization_id = ? AND role = 'owner'`, c.ID); n != 1 {
		t.Fatalf("%d owners remain (errors %v)", n, errs)
	}
	if (errs[0] == nil) == (errs[1] == nil) {
		t.Fatalf("want exactly one demotion to succeed, got %v", errs)
	}
}

func TestRemoveMember(t *testing.T) {
	e := newEnv(t)
	owner, owner2, manager, member, outsider := e.user(), e.user(), e.user(), e.user(), e.user()
	c := e.organization(owner, "Acme")
	e.member(c.ID, owner2, RoleOwner)
	e.member(c.ID, manager, RoleManager)
	e.member(c.ID, member, RoleMember)
	team := e.team(c.ID)
	e.teamMember(team, member)

	e.fails(e.do(owner, http.MethodDelete, memberPath(c.ID, owner), nil), http.StatusForbidden, apperr.CodeOperationNotAllowed, "You cannot delete yourself from the organization")
	e.fails(e.do(manager, http.MethodDelete, memberPath(c.ID, owner), nil), http.StatusForbidden, apperr.CodeOperationNotAllowed, "You cannot delete the owner of the organization")
	e.fails(e.do(member, http.MethodDelete, memberPath(c.ID, manager), nil), http.StatusForbidden, apperr.CodeOperationNotAllowed, "User management is not allowed for your role")
	e.fails(e.do(outsider, http.MethodDelete, memberPath(c.ID, member), nil), http.StatusForbidden, apperr.CodeOperationNotAllowed, "")
	e.fails(e.do(owner, http.MethodDelete, memberPath(c.ID, outsider), nil), http.StatusNotFound, apperr.CodeNotFound, "Not a member of this organization")

	e.ok(e.do(manager, http.MethodDelete, memberPath(c.ID, member), nil), http.StatusNoContent, nil)
	if e.role(c.ID, member) != "" || e.count(`SELECT count(*) FROM team_members WHERE user_id = ?`, member) != 0 {
		t.Fatal("membership or team membership survived")
	}
	// The guard counts active owners only: with the first owner deactivated, the second is the last active one, so the first may not be removed.
	e.exec(`UPDATE users SET is_active = false WHERE id = ?`, owner)
	e.fails(e.do(owner2, http.MethodDelete, memberPath(c.ID, owner), nil), http.StatusBadRequest, apperr.CodeValidation, "You cannot delete the last owner of the organization")
	e.exec(`UPDATE users SET is_active = true WHERE id = ?`, owner)
	e.ok(e.do(owner2, http.MethodDelete, memberPath(c.ID, owner), nil), http.StatusNoContent, nil)
}

func TestLeave(t *testing.T) {
	e := newEnv(t)
	owner, member, outsider := e.user(), e.user(), e.user()
	c := e.organization(owner, "Acme")
	e.member(c.ID, member, RoleMember)
	leave := orgPath(c.ID, "/leave")

	// Owners never leave, even with another owner beside them.
	e.member(c.ID, outsider, RoleOwner)
	e.fails(e.do(owner, http.MethodPost, leave, nil), http.StatusForbidden, apperr.CodeOperationNotAllowed, "You cannot leave an organization you own")
	e.exec(`DELETE FROM memberships WHERE organization_id = ? AND user_id = ?`, c.ID, outsider)

	e.fails(e.do(member, http.MethodPost, leave, nil), http.StatusBadRequest, apperr.CodeValidation, "You cannot leave the only organization you are a member of")
	e.fails(e.do(outsider, http.MethodPost, leave, nil), http.StatusForbidden, apperr.CodeForbidden, "")

	e.organization(member, "Own")
	team := e.team(c.ID)
	e.teamMember(team, member)
	e.ok(e.do(member, http.MethodPost, leave, nil), http.StatusNoContent, nil)
	if e.role(c.ID, member) != "" || e.count(`SELECT count(*) FROM team_members WHERE user_id = ?`, member) != 0 {
		t.Fatal("membership or team membership survived leaving")
	}
	events := e.auditEvents()
	if last := events[len(events)-1]; last["action"] != "deleted" || last["target"].(map[string]any)["type"] != "membership" || last["status"] != "success" {
		t.Fatalf("audit event %v", last)
	}
}
