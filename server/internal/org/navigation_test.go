package org

import (
	"net/http"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/platform/config"
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
	// The owner's first organization has no survey yet: onboarding.
	if h := e.home(owner); h.Path != "/organizations/"+c.ID.String()+"/workspaces/new/survey" {
		t.Fatalf("onboarding home %+v", h)
	}
	e.survey(c.WorkspaceID)
	if h := e.home(owner); h.Path != "" || !slices.Equal(h.WorkspaceIDs, []uuid.UUID{c.WorkspaceID}) {
		t.Fatalf("home after onboarding %+v", h)
	}

	// A member without a team grant can open nothing and lands on the organization.
	e.member(c.ID, member, RoleMember)
	if h := e.home(member); h.Path != "/organizations/"+c.ID.String()+"/landing" || len(h.WorkspaceIDs) != 0 {
		t.Fatalf("member home %+v", h)
	}
	team := e.team(c.ID)
	e.teamMember(team, member, "contributor")
	e.workspaceTeam(c.WorkspaceID, team, "read")
	// Their own organization comes later than Acme, so Acme's workspace is listed first.
	own := e.organization(member, "Own")
	h := e.home(member)
	if h.Path != "" || !slices.Equal(h.WorkspaceIDs, []uuid.UUID{c.WorkspaceID, own.WorkspaceID}) {
		t.Fatalf("member home with workspaces %+v", h)
	}
}

func TestHomeOnCloudStartsWithThePlan(t *testing.T) {
	e := newEnv(t, func(c *config.Config) { c.IsFormaCloud = true })
	owner := e.user()
	c := e.organization(owner, "Acme")
	if h := e.home(owner); h.Path != "/organizations/"+c.ID.String()+"/workspaces/new/plan" {
		t.Fatalf("cloud onboarding home %+v", h)
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
