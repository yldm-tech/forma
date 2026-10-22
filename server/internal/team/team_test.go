package team

import (
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/platform/audit"
	"github.com/yldm-tech/forma/server/internal/platform/config"
)

type org struct {
	id                              uuid.UUID
	owner, manager, member, billing user
}

func (e *env) orgWithRoles() org {
	id := e.org()
	return org{id: id, owner: e.member(id, "owner"), manager: e.member(id, "manager"), member: e.member(id, "member"), billing: e.member(id, "billing")}
}

// forma2's "requires organization.manage to create": owners and managers create teams, members and billing users do not, and USER_MANAGEMENT_MINIMUM_ROLE does not take it away.
func TestCreateTeam(t *testing.T) {
	t.Parallel()
	e := newEnv(t, func(c *config.Config) { c.UserManagementMinimumRole = "disabled" })
	o := e.orgWithRoles()
	e.drain()

	path := "/organizations/" + o.id.String() + "/teams"
	for _, u := range []user{o.member, o.billing} {
		expect(t, e.do(u, http.MethodPost, path, map[string]string{"name": "Design"}), http.StatusForbidden, "forbidden")
	}
	r := e.do(o.manager, http.MethodPost, path, map[string]string{"name": "  Design  "})
	expect(t, r, http.StatusCreated, "")
	id := uuid.MustParse(r.json()["id"].(string))
	var name string
	e.db.Raw(`SELECT name FROM teams WHERE id = ? AND organization_id = ?`, id, o.id).Scan(&name)
	if name != "Design" {
		t.Fatalf("stored name %q, want the trimmed name", name)
	}

	// Names are unique per organization, compared exactly.
	expect(t, e.do(o.owner, http.MethodPost, path, map[string]string{"name": "Design"}), http.StatusBadRequest, CodeTeamNameTaken)
	expect(t, e.do(o.owner, http.MethodPost, path, map[string]string{"name": "design"}), http.StatusCreated, "")
	other := e.org()
	otherOwner := e.member(other, "owner")
	e.drain()
	expect(t, e.do(otherOwner, http.MethodPost, "/organizations/"+other.String()+"/teams", map[string]string{"name": "Design"}), http.StatusCreated, "")

	expect(t, e.do(o.owner, http.MethodPost, path, map[string]string{"name": "   "}), http.StatusBadRequest, "validation_error")
	expect(t, e.do(o.owner, http.MethodPost, "/organizations/not-a-uuid/teams", map[string]string{"name": "X"}), http.StatusBadRequest, "validation_error")
	expect(t, e.do(user{}, http.MethodPost, path, map[string]string{"name": "X"}), http.StatusUnauthorized, "not_authenticated")

	created := e.auditEvents(audit.ActionCreated, audit.TargetTeam)
	var successes, failures int
	for _, ev := range created {
		switch ev["status"] {
		case "success":
			successes++
		case "failure":
			failures++
		}
	}
	if successes != 3 || failures < 3 {
		t.Fatalf("created/team events: %d successes, %d failures", successes, failures)
	}
	first := created[2]
	if first["status"] != "success" || first["organizationId"] != o.id.String() || first["target"].(map[string]any)["id"] != id.String() {
		t.Fatalf("created/team event %v", first)
	}
}

// forma2's getTeams: the caller's teams with their role and member counts, then the rest of the organization's teams; only product members of the organization see them.
func TestListTeams(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	o := e.orgWithRoles()
	alpha, beta, gamma := e.team(o.id, "Alpha"), e.team(o.id, "Beta"), e.team(o.id, "Gamma")
	e.teamMember(alpha, o.member, "admin")
	e.teamMember(alpha, o.manager, "admin")
	e.teamMember(beta, o.member, "contributor")
	e.teamMember(gamma, o.owner, "admin")
	e.team(e.org(), "Elsewhere")
	outsider := e.user("outsider")
	e.drain()

	r := e.do(o.member, http.MethodGet, "/organizations/"+o.id.String()+"/teams", nil)
	expect(t, r, http.StatusOK, "")
	body := r.json()
	mine, other := body["userTeams"].([]any), body["otherTeams"].([]any)
	if len(mine) != 2 || len(other) != 1 {
		t.Fatalf("teams %s", r.Body.String())
	}
	a := mine[0].(map[string]any)
	if a["name"] != "Alpha" || a["userRole"] != "admin" || a["memberCount"] != float64(2) || mine[1].(map[string]any)["userRole"] != "contributor" {
		t.Fatalf("user teams %v", mine)
	}
	if g := other[0].(map[string]any); g["name"] != "Gamma" || g["memberCount"] != float64(1) || g["userRole"] != nil {
		t.Fatalf("other teams %v", other)
	}

	for _, u := range []user{o.billing, outsider} {
		expect(t, e.do(u, http.MethodGet, "/organizations/"+o.id.String()+"/teams", nil), http.StatusForbidden, "forbidden")
	}
}

// forma2's getTeamDetailsAction requires team.manage: the team's admins and the organization's owners and managers, never a contributor or an admin of a sibling team.
func TestTeamDetails(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	o := e.orgWithRoles()
	contributor := e.member(o.id, "member")
	team, sibling := e.team(o.id, "Alpha"), e.team(o.id, "Beta")
	ws := e.workspace(o.id, "Website")
	e.teamMember(team, o.member, "admin")
	e.teamMember(team, contributor, "contributor")
	e.teamMember(sibling, contributor, "admin")
	e.workspaceTeam(ws, team, "readWrite")
	e.drain()

	for _, u := range []user{o.member, o.owner, o.manager} {
		r := e.do(u, http.MethodGet, "/teams/"+team.String(), nil)
		expect(t, r, http.StatusOK, "")
		d := r.json()
		members, workspaces := d["members"].([]any), d["workspaces"].([]any)
		if d["name"] != "Alpha" || d["organizationId"] != o.id.String() || len(members) != 2 || len(workspaces) != 1 {
			t.Fatalf("details %s", r.Body.String())
		}
		if w := workspaces[0].(map[string]any); w["workspaceId"] != ws.String() || w["workspaceName"] != "Website" || w["permission"] != "readWrite" {
			t.Fatalf("workspace %v", w)
		}
		if m := members[0].(map[string]any); m["userId"] != o.member.id.String() || m["name"] != "member" || m["role"] != "admin" {
			t.Fatalf("member %v", m)
		}
	}
	// An admin of a sibling team gains nothing here.
	expect(t, e.do(contributor, http.MethodGet, "/teams/"+team.String(), nil), http.StatusForbidden, "forbidden")
	expect(t, e.do(o.billing, http.MethodGet, "/teams/"+team.String(), nil), http.StatusForbidden, "forbidden")
	expect(t, e.do(o.owner, http.MethodGet, "/teams/"+uuid.NewString(), nil), http.StatusNotFound, "not_found")
}

func memberList(pairs ...any) []map[string]any {
	var out []map[string]any
	for i := 0; i < len(pairs); i += 2 {
		out = append(out, map[string]any{"userId": pairs[i].(user).id, "role": pairs[i+1]})
	}
	return out
}

func workspaceList(pairs ...any) []map[string]any {
	var out []map[string]any
	for i := 0; i < len(pairs); i += 2 {
		out = append(out, map[string]any{"workspaceId": pairs[i], "permission": pairs[i+1]})
	}
	return out
}

// forma2's updateTeamDetailsAction: a team admin manages members, but any change of workspace access needs organization.manage; the lists replace the stored ones and SpiceDB follows.
func TestUpdateTeam(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	o := e.orgWithRoles()
	newcomer := e.member(o.id, "member")
	team := e.team(o.id, "Alpha")
	e.team(o.id, "Taken")
	ws, victim := e.workspace(o.id, "Website"), e.workspace(o.id, "Finance")
	e.teamMember(team, o.member, "admin")
	e.teamMember(team, o.manager, "admin")
	e.workspaceTeam(ws, team, "read")
	e.drain()
	path := "/teams/" + team.String()

	// The team admin swaps the manager for the newcomer, keeping the grants as they are.
	r := e.do(o.member, http.MethodPut, path, map[string]any{
		"name": "Alpha", "members": memberList(o.member, "admin", newcomer, "contributor"), "workspaces": workspaceList(ws, "read"),
	})
	expect(t, r, http.StatusOK, "")
	if n := e.count(`SELECT count(*) FROM team_members WHERE team_id = ?`, team); n != 2 {
		t.Fatalf("%d team members, want 2", n)
	}
	e.drain()
	if !e.can(newcomer, "workspace.read", ws) || e.can(newcomer, "workspace.write", ws) {
		t.Fatal("the newcomer does not hold exactly the team's read access")
	}

	// Escalating, adding or removing a grant is the organization's call.
	for _, grants := range [][]map[string]any{workspaceList(ws, "manage"), workspaceList(ws, "read", victim, "manage"), workspaceList(victim, "read"), workspaceList(ws, "read", ws, "read")} {
		expect(t, e.do(o.member, http.MethodPut, path, map[string]any{"name": "Alpha", "members": memberList(o.member, "admin"), "workspaces": grants}), http.StatusForbidden, "forbidden")
	}
	if n := e.count(`SELECT count(*) FROM workspace_teams WHERE team_id = ? AND workspace_id = ? AND permission = 'read'`, team, ws); n != 1 || e.count(`SELECT count(*) FROM team_members WHERE team_id = ?`, team) != 2 {
		t.Fatal("a refused update changed the team")
	}

	// The owner may change everything at once; unchanged rows are not rewritten.
	before := e.count(`SELECT count(*) FROM authz_projection_outbox`)
	r = e.do(o.owner, http.MethodPut, path, map[string]any{
		"name": "Alpha Squad", "members": memberList(o.member, "admin", newcomer, "contributor"), "workspaces": workspaceList(ws, "read", victim, "manage"),
	})
	expect(t, r, http.StatusOK, "")
	if r.json()["name"] != "Alpha Squad" || len(r.json()["workspaces"].([]any)) != 2 {
		t.Fatalf("updated %s", r.Body.String())
	}
	if added := e.count(`SELECT count(*) FROM authz_projection_outbox`) - before; added != 1 {
		t.Fatalf("%d outbox events, want only the new grant's", added)
	}
	e.drain()
	if !e.can(newcomer, "workspace.manage", victim) {
		t.Fatal("the new grant was not projected")
	}

	// Removing the newcomer revokes what they had through the team.
	expect(t, e.do(o.owner, http.MethodPut, path, map[string]any{
		"name": "Alpha Squad", "members": memberList(o.member, "admin"), "workspaces": workspaceList(ws, "read", victim, "manage"),
	}), http.StatusOK, "")
	e.drain()
	if e.can(newcomer, "workspace.read", ws) || e.can(newcomer, "workspace.read", victim) {
		t.Fatal("a removed member kept the team's access")
	}

	foreignWorkspace := e.workspace(e.org(), "Theirs")
	outsider := e.user("outsider")
	cases := map[string]struct {
		body   map[string]any
		status int
		code   string
	}{
		"a user outside the organization":  {map[string]any{"name": "Alpha Squad", "members": memberList(o.member, "admin", outsider, "contributor"), "workspaces": workspaceList(ws, "read", victim, "manage")}, http.StatusBadRequest, CodeMembersNotInOrg},
		"a repeated member":                {map[string]any{"name": "Alpha Squad", "members": memberList(o.member, "admin", o.member, "contributor"), "workspaces": workspaceList(ws, "read", victim, "manage")}, http.StatusBadRequest, CodeMembersNotInOrg},
		"another organization's workspace": {map[string]any{"name": "Alpha Squad", "members": memberList(o.member, "admin"), "workspaces": workspaceList(ws, "read", foreignWorkspace, "read")}, http.StatusBadRequest, CodeWorkspacesNotInOrg},
		"a rename onto another team":       {map[string]any{"name": "Taken", "members": memberList(o.member, "admin"), "workspaces": workspaceList(ws, "read", victim, "manage")}, http.StatusBadRequest, CodeTeamNameTaken},
		"no members":                       {map[string]any{"name": "Alpha Squad", "members": []any{}, "workspaces": workspaceList(ws, "read")}, http.StatusBadRequest, "validation_error"},
		"no workspaces":                    {map[string]any{"name": "Alpha Squad", "members": memberList(o.member, "admin"), "workspaces": []any{}}, http.StatusBadRequest, "validation_error"},
		"an unknown team role":             {map[string]any{"name": "Alpha Squad", "members": memberList(o.member, "owner"), "workspaces": workspaceList(ws, "read", victim, "manage")}, http.StatusBadRequest, "validation_error"},
		"an unknown permission":            {map[string]any{"name": "Alpha Squad", "members": memberList(o.member, "admin"), "workspaces": workspaceList(ws, "write")}, http.StatusBadRequest, "validation_error"},
		"a blank name":                     {map[string]any{"name": " ", "members": memberList(o.member, "admin"), "workspaces": workspaceList(ws, "read", victim, "manage")}, http.StatusBadRequest, "validation_error"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			expect(t, e.do(o.owner, http.MethodPut, path, tc.body), tc.status, tc.code)
		})
	}
	var name string
	e.db.Raw(`SELECT name FROM teams WHERE id = ?`, team).Scan(&name)
	if name != "Alpha Squad" || e.count(`SELECT count(*) FROM team_members WHERE team_id = ?`, team) != 1 {
		t.Fatal("a refused update changed the team")
	}

	// Contributors and billing users cannot update.
	expect(t, e.do(o.billing, http.MethodPut, path, map[string]any{"name": "X", "members": memberList(o.member, "admin"), "workspaces": workspaceList(ws, "read", victim, "manage")}), http.StatusForbidden, "forbidden")
	expect(t, e.do(o.owner, http.MethodPut, "/teams/"+uuid.NewString(), map[string]any{"name": "X", "members": memberList(o.member, "admin"), "workspaces": workspaceList(ws, "read")}), http.StatusNotFound, "not_found")

	updated := e.auditEvents(audit.ActionUpdated, audit.TargetTeam)
	if len(updated) == 0 || updated[0]["status"] != "success" || updated[0]["changes"] == nil {
		t.Fatalf("updated/team events %v", updated)
	}
}

// forma2's "requires team.delete to delete": only owners and managers delete, and the team's members and grants go with it.
func TestDeleteTeam(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	o := e.orgWithRoles()
	team := e.team(o.id, "Alpha")
	ws := e.workspace(o.id, "Website")
	e.teamMember(team, o.member, "admin")
	e.workspaceTeam(ws, team, "manage")
	creator := e.member(o.id, "owner")
	invite := e.insert(`INSERT INTO invites (organization_id, email, creator_id, expires_at) VALUES (?, 'x@example.com', ?, now() + interval '7 days') RETURNING id`, o.id, creator.id)
	e.exec(`INSERT INTO invite_teams (invite_id, team_id) VALUES (?, ?)`, invite, team)
	e.drain()
	if !e.can(o.member, "workspace.manage", ws) {
		t.Fatal("setup not projected")
	}

	// A team admin manages the team but cannot delete it.
	expect(t, e.do(o.member, http.MethodDelete, "/teams/"+team.String(), nil), http.StatusForbidden, "forbidden")
	expect(t, e.do(o.manager, http.MethodDelete, "/teams/"+team.String(), nil), http.StatusNoContent, "")
	if e.count(`SELECT count(*) FROM teams WHERE id = ?`, team)+e.count(`SELECT count(*) FROM team_members WHERE team_id = ?`, team)+e.count(`SELECT count(*) FROM workspace_teams WHERE team_id = ?`, team)+e.count(`SELECT count(*) FROM invite_teams WHERE team_id = ?`, team) != 0 {
		t.Fatal("the team or its rows survived")
	}
	if e.count(`SELECT count(*) FROM invites WHERE id = ?`, invite) != 1 {
		t.Fatal("the pending invite went with the team")
	}
	e.drain()
	if e.can(o.member, "workspace.read", ws) {
		t.Fatal("access through the deleted team survived")
	}
	expect(t, e.do(o.owner, http.MethodDelete, "/teams/"+team.String(), nil), http.StatusNotFound, "not_found")
	deleted := e.auditEvents(audit.ActionDeleted, audit.TargetTeam)
	if len(deleted) != 3 || deleted[0]["status"] != "failure" || deleted[1]["status"] != "success" || deleted[1]["changes"] == nil || deleted[2]["status"] != "failure" {
		t.Fatalf("deleted/team events %v", deleted)
	}
}

// forma2's getTeamRoleAction returns the caller's own role, or none.
func TestTeamRole(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	o := e.orgWithRoles()
	team := e.team(o.id, "Alpha")
	e.teamMember(team, o.member, "contributor")

	r := e.do(o.member, http.MethodGet, "/teams/"+team.String()+"/role", nil)
	expect(t, r, http.StatusOK, "")
	if r.Body.String() != `{"role":"contributor"}` {
		t.Fatalf("role %s", r.Body.String())
	}
	r = e.do(o.owner, http.MethodGet, "/teams/"+team.String()+"/role", nil)
	if r.Body.String() != `{"role":null}` {
		t.Fatalf("role %s", r.Body.String())
	}
}

// forma2's getTeamsByWorkspaceId behind the workspace page's gate: whoever reads the workspace sees the teams with access to it.
func TestWorkspaceTeams(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	o := e.orgWithRoles()
	ws, other := e.workspace(o.id, "Website"), e.workspace(o.id, "Finance")
	readers, writers := e.team(o.id, "Readers"), e.team(o.id, "Writers")
	e.teamMember(readers, o.member, "contributor")
	e.teamMember(writers, o.manager, "admin")
	e.workspaceTeam(ws, readers, "read")
	e.workspaceTeam(ws, writers, "readWrite")
	e.workspaceTeam(other, writers, "manage")
	e.drain()

	r := e.do(o.member, http.MethodGet, "/workspaces/"+ws.String()+"/teams", nil)
	expect(t, r, http.StatusOK, "")
	want := `[{"id":"` + readers.String() + `","name":"Readers","permission":"read","memberCount":1},{"id":"` + writers.String() + `","name":"Writers","permission":"readWrite","memberCount":1}]`
	if r.Body.String() != want {
		t.Fatalf("workspace teams %s, want %s", r.Body.String(), want)
	}
	expect(t, e.do(o.member, http.MethodGet, "/workspaces/"+other.String()+"/teams", nil), http.StatusForbidden, "forbidden")
	expect(t, e.do(o.billing, http.MethodGet, "/workspaces/"+ws.String()+"/teams", nil), http.StatusForbidden, "forbidden")
}
