package org

import (
	"database/sql"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
)

func suspensionPath(org, user uuid.UUID) string {
	return memberPath(org, user) + "/suspension"
}

// suspendedAt is the membership's suspension time, nil while the member has access.
func (e *env) suspendedAt(org, user uuid.UUID) *time.Time {
	e.t.Helper()
	var at []sql.NullTime
	if err := e.db.Raw(`SELECT suspended_at FROM memberships WHERE organization_id = ? AND user_id = ?`, org, user).Scan(&at).Error; err != nil {
		e.t.Fatal(err)
	}
	if len(at) == 0 || !at[0].Valid {
		return nil
	}
	return &at[0].Time
}

// The rules of a suspension, one case each: those of a role change.
func TestSuspensionRules(t *testing.T) {
	type fixture struct {
		org                                      uuid.UUID
		owner, owner2, manager, member, outsider uuid.UUID
	}
	setup := func(e *env) fixture {
		f := fixture{owner: e.user(), owner2: e.user(), manager: e.user(), member: e.user(), outsider: e.user()}
		f.org = e.organization(f.owner, "Acme").ID
		e.member(f.org, f.owner2, RoleOwner)
		e.member(f.org, f.manager, RoleManager)
		e.member(f.org, f.member, RoleMember)
		return f
	}
	cases := []struct {
		name      string
		configure func(*config.Config)
		actor     func(f fixture) uuid.UUID
		target    func(f fixture) uuid.UUID
		status    int
		code      string
		message   string
	}{
		{name: "a manager suspends a member", actor: func(f fixture) uuid.UUID { return f.manager }, target: func(f fixture) uuid.UUID { return f.member }, status: http.StatusOK},
		{name: "an owner suspends another owner", actor: func(f fixture) uuid.UUID { return f.owner }, target: func(f fixture) uuid.UUID { return f.owner2 }, status: http.StatusOK},
		{name: "nobody suspends themselves", actor: func(f fixture) uuid.UUID { return f.owner }, target: func(f fixture) uuid.UUID { return f.owner },
			status: http.StatusForbidden, code: apperr.CodeOperationNotAllowed, message: "You cannot suspend or restore your own access"},
		{name: "a manager may not touch an owner", actor: func(f fixture) uuid.UUID { return f.manager }, target: func(f fixture) uuid.UUID { return f.owner },
			status: http.StatusForbidden, code: apperr.CodeOperationNotAllowed, message: "Only owners can suspend or restore an owner"},
		{name: "a member has no user management", actor: func(f fixture) uuid.UUID { return f.member }, target: func(f fixture) uuid.UUID { return f.manager },
			status: http.StatusForbidden, code: apperr.CodeOperationNotAllowed, message: "User management is not allowed for your role"},
		{name: "USER_MANAGEMENT_MINIMUM_ROLE=owner shuts managers out", configure: func(c *config.Config) { c.UserManagementMinimumRole = "owner" },
			actor: func(f fixture) uuid.UUID { return f.manager }, target: func(f fixture) uuid.UUID { return f.member },
			status: http.StatusForbidden, code: apperr.CodeOperationNotAllowed, message: "User management is not allowed for your role"},
		{name: "a non-member", actor: func(f fixture) uuid.UUID { return f.outsider }, target: func(f fixture) uuid.UUID { return f.member },
			status: http.StatusForbidden, code: apperr.CodeOperationNotAllowed},
		{name: "a target outside the organization", actor: func(f fixture) uuid.UUID { return f.owner }, target: func(f fixture) uuid.UUID { return f.outsider },
			status: http.StatusNotFound, code: apperr.CodeNotFound, message: "Not a member of this organization"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var configure []func(*config.Config)
			if tc.configure != nil {
				configure = append(configure, tc.configure)
			}
			e := newEnv(t, configure...)
			f := setup(e)
			rec := e.do(tc.actor(f), http.MethodPost, suspensionPath(f.org, tc.target(f)), nil)
			if tc.status != http.StatusOK {
				e.fails(rec, tc.status, tc.code, tc.message)
				if e.suspendedAt(f.org, tc.target(f)) != nil {
					t.Fatal("suspended on a refused request")
				}
				return
			}
			var m Membership
			e.ok(rec, http.StatusOK, &m)
			if m.SuspendedAt == nil || e.suspendedAt(f.org, tc.target(f)) == nil {
				t.Fatalf("membership %+v, want it suspended", m)
			}
			// Restoring follows the same rules and clears the suspension.
			e.ok(e.do(tc.actor(f), http.MethodDelete, suspensionPath(f.org, tc.target(f)), nil), http.StatusOK, &m)
			if m.SuspendedAt != nil || e.suspendedAt(f.org, tc.target(f)) != nil {
				t.Fatalf("membership %+v, want it restored", m)
			}
		})
	}
}

// A suspended member keeps their account and their other organizations; in this one they keep only its name, their membership and the way out, and they are told why.
func TestASuspendedMemberIsRefusedOnlyThatOrganization(t *testing.T) {
	e := newEnv(t)
	owner, member := e.user(), e.user()
	c := e.organization(owner, "Acme")
	other := e.organization(member, "Elsewhere")
	// A survey takes the member's own organization past onboarding, so the root page lists workspaces.
	e.survey(other.WorkspaceID)
	e.member(c.ID, member, RoleManager)
	workspacePath := "/api/app/workspaces/" + c.WorkspaceID.String()
	e.ok(e.do(member, http.MethodGet, workspacePath, nil), http.StatusOK, nil)

	var m Membership
	e.ok(e.do(owner, http.MethodPost, suspensionPath(c.ID, member), nil), http.StatusOK, &m)
	since := e.suspendedAt(c.ID, member)
	// Suspending again changes nothing, not even the time.
	e.ok(e.do(owner, http.MethodPost, suspensionPath(c.ID, member), nil), http.StatusOK, &m)
	if at := e.suspendedAt(c.ID, member); since == nil || at == nil || !at.Equal(*since) {
		t.Fatalf("suspended at %v, then %v", since, at)
	}
	events := e.auditEvents()
	if last := events[len(events)-1]; last["action"] != "updated" || last["target"].(map[string]any)["type"] != "membership" || last["status"] != "success" {
		t.Fatalf("audit event %v", last)
	}

	for _, path := range []string{workspacePath, orgPath(c.ID, "/members"), orgPath(c.ID, "/workspaces")} {
		rec := e.do(member, http.MethodGet, path, nil)
		e.fails(rec, http.StatusForbidden, authz.CodeMembershipSuspended, "")
		if want := `"organizationId":"` + c.ID.String() + `"`; !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("%s: %s, want the organization in the details", path, rec.Body.String())
		}
	}
	e.fails(e.do(member, http.MethodPatch, orgPath(c.ID, ""), map[string]any{"displayTimeZone": "Asia/Tokyo"}), http.StatusForbidden, authz.CodeMembershipSuspended, "")
	e.fails(e.do(member, http.MethodPost, suspensionPath(c.ID, owner), nil), http.StatusForbidden, apperr.CodeOperationNotAllowed, "")

	var d Detail
	e.ok(e.do(member, http.MethodGet, orgPath(c.ID, ""), nil), http.StatusOK, &d)
	if d.Membership.SuspendedAt == nil {
		t.Fatalf("detail membership %+v, want it suspended", d.Membership)
	}
	var nav Navigation
	e.ok(e.do(member, http.MethodGet, orgPath(c.ID, "/navigation"), nil), http.StatusOK, &nav)
	if nav.Path != SuspendedPath(c.ID) || nav.CanCreateWorkspace {
		t.Fatalf("navigation %+v", nav)
	}
	var list listResponse
	e.ok(e.do(member, http.MethodGet, "/api/app/organizations", nil), http.StatusOK, &list)
	suspended := map[uuid.UUID]bool{}
	for _, o := range list.Organizations {
		suspended[o.ID] = o.SuspendedAt != nil
	}
	if len(list.Organizations) != 2 || !suspended[c.ID] || suspended[other.ID] {
		t.Fatalf("organizations %+v, want both listed and only Acme suspended", list.Organizations)
	}
	var home Home
	e.ok(e.do(member, http.MethodGet, "/api/app/home", nil), http.StatusOK, &home)
	if home.Path != "" || len(home.WorkspaceIDs) != 1 || home.WorkspaceIDs[0] != other.WorkspaceID {
		t.Fatalf("home %+v, want only %s", home, other.WorkspaceID)
	}
	e.ok(e.do(member, http.MethodGet, "/api/app/workspaces/"+other.WorkspaceID.String(), nil), http.StatusOK, nil)

	// The owner sees the suspension on the members page.
	var members membersResponse
	e.ok(e.do(owner, http.MethodGet, orgPath(c.ID, "/members"), nil), http.StatusOK, &members)
	for _, row := range members.Members {
		if (row.UserID == member) != (row.SuspendedAt != nil) || !row.IsActive {
			t.Fatalf("member row %+v", row)
		}
	}
	// Restoring gives the access back at once.
	e.ok(e.do(owner, http.MethodDelete, suspensionPath(c.ID, member), nil), http.StatusOK, &m)
	e.ok(e.do(member, http.MethodGet, workspacePath, nil), http.StatusOK, nil)
	e.ok(e.do(member, http.MethodGet, orgPath(c.ID, "/navigation"), nil), http.StatusOK, &nav)
	if nav.Path == SuspendedPath(c.ID) {
		t.Fatalf("navigation %+v after restoring", nav)
	}
}

// A member whose only organization suspended them is told so by the root page; a suspended member may still leave an organization that is not their only one.
func TestHomeTellsAMemberEveryOrganizationSuspended(t *testing.T) {
	e := newEnv(t)
	owner, member := e.user(), e.user()
	c := e.organization(owner, "Acme")
	e.member(c.ID, member, RoleMember)
	e.ok(e.do(owner, http.MethodPost, suspensionPath(c.ID, member), nil), http.StatusOK, nil)

	var home Home
	e.ok(e.do(member, http.MethodGet, "/api/app/home", nil), http.StatusOK, &home)
	if home.Path != SuspendedPath(c.ID) || len(home.WorkspaceIDs) != 0 {
		t.Fatalf("home %+v", home)
	}

	own := e.organization(member, "Own")
	e.survey(own.WorkspaceID)
	home = Home{}
	e.ok(e.do(member, http.MethodGet, "/api/app/home", nil), http.StatusOK, &home)
	if home.Path != "" || len(home.WorkspaceIDs) != 1 || home.WorkspaceIDs[0] != own.WorkspaceID {
		t.Fatalf("home %+v with an organization of their own", home)
	}
	e.ok(e.do(member, http.MethodPost, orgPath(c.ID, "/leave"), nil), http.StatusNoContent, nil)
	if e.role(c.ID, member) != "" {
		t.Fatal("the suspended member could not leave")
	}
}

// An owner who is suspended does not count toward the active owners a role change, a removal or another suspension must leave, and may be demoted or removed while another active owner remains.
func TestASuspendedOwnerIsNotAnActiveOwner(t *testing.T) {
	e := newEnv(t)
	owner, owner2 := e.user(), e.user()
	c := e.organization(owner, "Acme")
	e.member(c.ID, owner2, RoleOwner)
	e.ok(e.do(owner, http.MethodPost, suspensionPath(c.ID, owner2), nil), http.StatusOK, nil)

	e.fails(e.do(owner, http.MethodPatch, memberPath(c.ID, owner), map[string]string{"role": RoleMember}), http.StatusBadRequest, apperr.CodeValidation, "You cannot demote the last owner of the organization")
	e.ok(e.do(owner, http.MethodPatch, memberPath(c.ID, owner2), map[string]string{"role": RoleManager}), http.StatusOK, nil)
	e.ok(e.do(owner, http.MethodPatch, memberPath(c.ID, owner2), map[string]string{"role": RoleOwner}), http.StatusOK, nil)
	e.ok(e.do(owner, http.MethodDelete, memberPath(c.ID, owner2), nil), http.StatusNoContent, nil)
}

// Two owners suspending each other at once cannot leave the organization without an active owner: the guard and the write share a serializable transaction.
func TestOwnersSuspendingEachOtherConcurrentlyKeepAnOwner(t *testing.T) {
	e := newEnv(t)
	a, b := e.user(), e.user()
	c := e.organization(a, "Acme")
	e.member(c.ID, b, RoleOwner)
	e.authz.Deliver(t)

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, pair := range [][2]uuid.UUID{{a, b}, {b, a}} {
		wg.Go(func() {
			_, errs[i] = e.svc.SetSuspension(t.Context(), pair[0], c.ID, pair[1], true)
		})
	}
	wg.Wait()
	if n := e.count(`SELECT count(*) FROM memberships WHERE organization_id = ? AND role = 'owner' AND suspended_at IS NULL`, c.ID); n != 1 {
		t.Fatalf("%d active owners remain (errors %v)", n, errs)
	}
	if (errs[0] == nil) == (errs[1] == nil) {
		t.Fatalf("want exactly one suspension to succeed, got %v", errs)
	}
}
