package org

import (
	"net/http"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/workspace"
)

func (e *env) home(user uuid.UUID) Home {
	e.t.Helper()
	var h Home
	e.ok(e.do(user, http.MethodGet, "/api/app/home", nil), http.StatusOK, &h)
	return h
}

func TestHomeSignedOut(t *testing.T) {
	e := newEnv(t)
	if h := e.home(uuid.Nil); h.Path != "/setup/intro" {
		t.Fatalf("fresh instance home %+v", h)
	}
	e.user()
	if h := e.home(uuid.Nil); h.Path != "/auth/login" {
		t.Fatalf("signed-out home %+v", h)
	}
}

func TestHome(t *testing.T) {
	e := newEnv(t)
	owner, member, stranger := e.user(), e.user(), e.user()
	if h := e.home(stranger); h.Path != "/setup/organization/create" {
		t.Fatalf("home without an organization %+v", h)
	}

	c := e.organization(owner, "Acme")
	// An owner whose onboarding workspace has no survey yet creates the first one.
	if h := e.home(owner); h.Path != "/organizations/"+c.ID.String()+"/workspaces/new/survey" || !slices.Equal(h.WorkspaceIDs, []uuid.UUID{c.WorkspaceID}) {
		t.Fatalf("owner home without a survey %+v", h)
	}
	e.survey(c.WorkspaceID)
	if h := e.home(owner); h.Path != "" || !slices.Equal(h.WorkspaceIDs, []uuid.UUID{c.WorkspaceID}) {
		t.Fatalf("owner home with a survey %+v", h)
	}

	// A member without a team grant can open nothing and lands on the organization.
	e.member(c.ID, member, RoleMember)
	if h := e.home(member); h.Path != "/organizations/"+c.ID.String()+"/landing" || len(h.WorkspaceIDs) != 0 {
		t.Fatalf("member home %+v", h)
	}
	team := e.team(c.ID)
	e.teamMember(team, member)
	e.workspaceTeam(c.WorkspaceID, team, "read")
	// Their own organization comes later than Acme, so Acme's workspace is listed first.
	own := e.organization(member, "Own")
	h := e.home(member)
	if h.Path != "" || !slices.Equal(h.WorkspaceIDs, []uuid.UUID{c.WorkspaceID, own.WorkspaceID}) {
		t.Fatalf("member home with workspaces %+v", h)
	}
}

// A membership the authorization projection has not delivered yet (sign-up, an accepted invite, SSO provisioning) is reported as pending, so the SPA waits for it instead of sending the user to create an organization or into an older one.
func TestHomeWhileTheProjectionLags(t *testing.T) {
	e := newEnv(t)
	owner, joiner, veteran := e.user(), e.user(), e.user()
	acme := e.organization(owner, "Acme")
	old := e.organization(veteran, "Old")
	e.survey(old.WorkspaceID)
	if h := e.home(veteran); h.Pending || !slices.Equal(h.WorkspaceIDs, []uuid.UUID{old.WorkspaceID}) {
		t.Fatalf("settled home %+v", h)
	}

	e.member(acme.ID, joiner, RoleManager)
	e.member(acme.ID, veteran, RoleManager)
	var h Home
	e.ok(e.doUndelivered(joiner, http.MethodGet, "/api/app/home", nil), http.StatusOK, &h)
	if !h.Pending || h.Path != "/setup/organization/create" {
		t.Fatalf("home of a user whose only membership is undelivered %+v", h)
	}
	e.ok(e.doUndelivered(veteran, http.MethodGet, "/api/app/home", nil), http.StatusOK, &h)
	if !h.Pending {
		t.Fatalf("home of a user whose new membership is undelivered %+v", h)
	}
	if h := e.home(joiner); h.Pending || h.Path == "/setup/organization/create" {
		t.Fatalf("home once delivered %+v", h)
	}
	if h := e.home(veteran); h.Pending {
		t.Fatalf("home once delivered %+v", h)
	}
}

// forma2's onboarding gate: only an owner or manager of the user's first organization is sent into the new-workspace flow, and only while that organization's onboarding workspace (the oldest one they can open) has no survey, archived ones included.
func TestHomeOnboardingGate(t *testing.T) {
	e := newEnv(t)
	owner, manager, member := e.user(), e.user(), e.user()
	c := e.organization(owner, "Acme")
	e.member(c.ID, manager, RoleManager)
	e.member(c.ID, member, RoleMember)
	onboarding := "/organizations/" + c.ID.String() + "/workspaces/new/survey"

	if h := e.home(manager); h.Path != onboarding {
		t.Fatalf("manager home without a survey %+v", h)
	}
	// A member cannot create the first survey, so the gate does not apply and they land on the organization.
	if h := e.home(member); h.Path != "/organizations/"+c.ID.String()+"/landing" {
		t.Fatalf("member home without a survey %+v", h)
	}

	// A survey in a newer workspace does not complete the onboarding of the oldest one.
	var second workspace.Workspace
	e.ok(e.do(owner, http.MethodPost, orgPath(c.ID, "/workspaces"), map[string]string{"name": "Second"}), http.StatusCreated, &second)
	e.survey(second.ID)
	if h := e.home(owner); h.Path != onboarding || !slices.Equal(h.WorkspaceIDs, []uuid.UUID{c.WorkspaceID, second.ID}) {
		t.Fatalf("owner home with a survey only in a newer workspace %+v", h)
	}

	e.survey(c.WorkspaceID)
	e.exec(`UPDATE surveys SET archived_at = now() WHERE workspace_id = ?`, c.WorkspaceID)
	if h := e.home(owner); h.Path != "" {
		t.Fatalf("owner home with an archived survey %+v", h)
	}

	// The gate looks at the first organization only: a newer organization without a survey does not redirect.
	e.organization(owner, "Later")
	if h := e.home(owner); h.Path != "" {
		t.Fatalf("owner home with a newer organization without a survey %+v", h)
	}
}

func TestHomeOnCloudStartsWithThePlan(t *testing.T) {
	e := newEnv(t, func(c *config.Config) { c.IsFormaCloud = true })
	owner := e.user()
	c := e.organization(owner, "Acme")
	if h := e.home(owner); h.Path != "/organizations/"+c.ID.String()+"/workspaces/new/plan" || !slices.Equal(h.WorkspaceIDs, []uuid.UUID{c.WorkspaceID}) {
		t.Fatalf("cloud owner home without a survey %+v", h)
	}
	e.survey(c.WorkspaceID)
	if h := e.home(owner); h.Path != "" || !slices.Equal(h.WorkspaceIDs, []uuid.UUID{c.WorkspaceID}) {
		t.Fatalf("cloud owner home with a survey %+v", h)
	}
}

func TestNavigate(t *testing.T) {
	for _, cloud := range []bool{false, true} {
		t.Run(map[bool]string{false: "self-hosted", true: "cloud"}[cloud], func(t *testing.T) {
			e := newEnv(t, func(c *config.Config) { c.IsFormaCloud = cloud })
			owner, member, billing, outsider := e.user(), e.user(), e.user(), e.user()
			c := e.organization(owner, "Acme")
			e.member(c.ID, member, RoleMember)
			e.member(c.ID, billing, RoleBilling)
			path := orgPath(c.ID, "/navigation")

			var nav Navigation
			e.ok(e.do(owner, http.MethodGet, path, nil), http.StatusOK, &nav)
			if nav.Path != "/workspaces/"+c.WorkspaceID.String()+"/" || !nav.CanCreateWorkspace {
				t.Fatalf("owner navigation %+v", nav)
			}
			e.ok(e.do(member, http.MethodGet, path, nil), http.StatusOK, &nav)
			if nav.Path != "/organizations/"+c.ID.String()+"/landing" || nav.CanCreateWorkspace {
				t.Fatalf("member navigation %+v", nav)
			}
			e.ok(e.do(billing, http.MethodGet, path, nil), http.StatusOK, &nav)
			want := "/account/settings/profile"
			if cloud {
				want = "/organizations/" + c.ID.String() + "/settings/billing"
			}
			if nav.Path != want {
				t.Fatalf("billing navigation (cloud %v) %+v", cloud, nav)
			}
			e.fails(e.do(outsider, http.MethodGet, path, nil), http.StatusNotFound, apperr.CodeNotFound, "")
		})
	}
}

// A member of one organization reaches nothing of another: every read, write and delete of its organization, members and workspaces is refused, and nothing changes.
func TestCrossTenantDenial(t *testing.T) {
	e := newEnv(t)
	alice, bob, bobMember := e.user(), e.user(), e.user()
	mine := e.organization(alice, "Alice's")
	theirs := e.organization(bob, "Bob's")
	e.member(theirs.ID, bobMember, RoleMember)
	second := theirs.WorkspaceID
	e.ok(e.do(bob, http.MethodPost, orgPath(theirs.ID, "/workspaces"), map[string]string{"name": "Second"}), http.StatusCreated, nil)
	ws := "/api/app/workspaces/" + second.String()

	forbidden := []struct {
		method, path string
		body         any
	}{
		{http.MethodPatch, orgPath(theirs.ID, ""), map[string]any{"name": "Pwned"}},
		{http.MethodPatch, orgPath(theirs.ID, ""), map[string]any{"displayTimeZone": "UTC"}},
		{http.MethodDelete, orgPath(theirs.ID, ""), nil},
		{http.MethodGet, orgPath(theirs.ID, "/members"), nil},
		{http.MethodPatch, memberPath(theirs.ID, bobMember), map[string]string{"role": RoleOwner}},
		{http.MethodDelete, memberPath(theirs.ID, bobMember), nil},
		{http.MethodPost, orgPath(theirs.ID, "/leave"), nil},
		{http.MethodGet, orgPath(theirs.ID, "/workspaces"), nil},
		{http.MethodPost, orgPath(theirs.ID, "/workspaces"), map[string]string{"name": "Planted"}},
		{http.MethodGet, ws, nil},
		{http.MethodPatch, ws, map[string]any{"name": "Pwned"}},
		{http.MethodPost, ws + "/delete", map[string]string{"confirmationName": "My workspace"}},
	}
	for _, r := range forbidden {
		rec := e.do(alice, r.method, r.path, r.body)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s: %d %s, want 403", r.method, r.path, rec.Code, rec.Body.String())
		}
	}
	notFound := []string{orgPath(theirs.ID, ""), orgPath(theirs.ID, "/navigation"), orgPath(theirs.ID, "/onboarding"), "/api/app/environments/" + second.String()}
	for _, p := range notFound {
		if rec := e.do(alice, http.MethodGet, p, nil); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: %d %s, want 404", p, rec.Code, rec.Body.String())
		}
	}
	var list listResponse
	e.ok(e.do(alice, http.MethodGet, "/api/app/organizations", nil), http.StatusOK, &list)
	if len(list.Organizations) != 1 || list.Organizations[0].ID != mine.ID {
		t.Fatalf("alice lists %+v", list.Organizations)
	}

	var name string
	e.scalar(&name, `SELECT name FROM organizations WHERE id = ?`, theirs.ID)
	if name != "Bob's" || e.role(theirs.ID, bobMember) != RoleMember || e.role(theirs.ID, alice) != "" ||
		e.count(`SELECT count(*) FROM workspaces WHERE organization_id = ?`, theirs.ID) != 2 || e.count(`SELECT count(*) FROM workspaces WHERE name IN ('Pwned', 'Planted')`) != 0 {
		t.Fatal("a cross-tenant request changed something")
	}
}
