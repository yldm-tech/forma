package org

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
)

// passwordUser is a user who signs in with a password, the kind an organization's two-factor requirement applies to.
func (e *env) passwordUser() uuid.UUID {
	id := e.user()
	e.exec(`INSERT INTO user_identities (user_id, provider, subject, password_hash) VALUES (?, 'credential', ?, '$2a$10$hash')`, id, id.String())
	return id
}

// ssoUser is a user without a password who signs in through an external identity provider, whom the requirement exempts.
func (e *env) ssoUser() uuid.UUID {
	id := e.user()
	e.linkSSO(id)
	return id
}

// linkSSO gives the user an external identity. A password user keeps their password (SSO recovery of a verified account leaves it), signs in with it alone and so stays subject to the requirement.
func (e *env) linkSSO(user uuid.UUID) {
	e.exec(`INSERT INTO user_identities (user_id, provider, issuer, subject) VALUES (?, 'openid', 'https://idp.example.com', ?)`, user, user.String())
}

// enrol gives the user a verified authenticator.
func (e *env) enrol(user uuid.UUID) {
	e.exec(`INSERT INTO user_totp (user_id, secret_ciphertext, backup_codes_ciphertext, verified) VALUES (?, '\x00', '\x00', true)`, user)
}

// enforce moves the end of the organization's grace period by the given offset from now; a negative one is a grace period that is over.
func (e *env) enforce(org uuid.UUID, offset time.Duration) {
	e.exec(`UPDATE organizations SET two_factor_enforced_at = now() + make_interval(secs => ?) WHERE id = ?`, offset.Seconds(), org)
}

func requirement(required bool, days *int) map[string]any {
	body := map[string]any{"requireTwoFactor": required}
	if days != nil {
		body["twoFactorGracePeriodDays"] = *days
	}
	return body
}

func TestRequireTwoFactor(t *testing.T) {
	e := newEnv(t)
	owner, manager := e.passwordUser(), e.passwordUser()
	c := e.organization(owner, "Acme")
	e.member(c.ID, manager, RoleManager)
	week, zero, tooLong, negative := 7, 0, MaxTwoFactorGracePeriodDays+1, -1

	// The requirement would refuse an owner without two-factor authentication at once, so they must turn it on first.
	e.fails(e.do(owner, http.MethodPatch, orgPath(c.ID, ""), requirement(true, &week)), http.StatusForbidden, CodeEnableTwoFactorFirst, "")
	if e.count(`SELECT count(*) FROM organizations WHERE id = ? AND two_factor_enforced_at IS NOT NULL`, c.ID) != 0 {
		t.Fatal("the requirement was stored for an owner without two-factor authentication")
	}

	e.enrol(owner)
	var o Organization
	e.ok(e.do(owner, http.MethodPatch, orgPath(c.ID, ""), requirement(true, &week)), http.StatusOK, &o)
	if o.TwoFactorEnforcedAt == nil || time.Until(*o.TwoFactorEnforcedAt) < 7*24*time.Hour-time.Minute || time.Until(*o.TwoFactorEnforcedAt) > 7*24*time.Hour {
		t.Fatalf("enforced at %v, want a week from now", o.TwoFactorEnforcedAt)
	}
	var d Detail
	e.ok(e.do(manager, http.MethodGet, orgPath(c.ID, ""), nil), http.StatusOK, &d)
	if d.TwoFactorEnforcedAt == nil || d.TwoFactorStatus != authz.SecondFactorDisabled || d.TwoFactorBlocked {
		t.Fatalf("a manager in the grace period sees %v %q %v", d.TwoFactorEnforcedAt, d.TwoFactorStatus, d.TwoFactorBlocked)
	}

	// Owners only: managers manage the organization but not its security policy.
	e.fails(e.do(manager, http.MethodPatch, orgPath(c.ID, ""), requirement(false, nil)), http.StatusForbidden, apperr.CodeForbidden, "")

	for _, body := range []map[string]any{requirement(true, nil), requirement(false, &week), requirement(true, &tooLong), requirement(true, &negative)} {
		e.fails(e.do(owner, http.MethodPatch, orgPath(c.ID, ""), body), http.StatusBadRequest, apperr.CodeValidation, "")
	}

	e.ok(e.do(owner, http.MethodPatch, orgPath(c.ID, ""), requirement(true, &zero)), http.StatusOK, &o)
	if o.TwoFactorEnforcedAt == nil || time.Until(*o.TwoFactorEnforcedAt) > time.Minute {
		t.Fatalf("enforced at %v, want now", o.TwoFactorEnforcedAt)
	}
	e.ok(e.do(owner, http.MethodPatch, orgPath(c.ID, ""), requirement(false, nil)), http.StatusOK, &o)
	if o.TwoFactorEnforcedAt != nil {
		t.Fatalf("enforced at %v after turning the requirement off", o.TwoFactorEnforcedAt)
	}

	// An owner with a password and an SSO identity signs in with the password alone, so they must turn it on first too.
	linked := e.passwordUser()
	e.linkSSO(linked)
	e.member(c.ID, linked, RoleOwner)
	e.fails(e.do(linked, http.MethodPatch, orgPath(c.ID, ""), requirement(true, &week)), http.StatusForbidden, CodeEnableTwoFactorFirst, "")

	// An owner who signs in through SSO cannot enrol here and is exempt, so they may require it of the others.
	sso := e.ssoUser()
	e.member(c.ID, sso, RoleOwner)
	e.ok(e.do(sso, http.MethodPatch, orgPath(c.ID, ""), requirement(true, &week)), http.StatusOK, &o)

	events := e.auditEvents()
	last := events[len(events)-1]
	if last["action"] != "updated" || last["target"].(map[string]any)["type"] != "organization" {
		t.Fatalf("audit event %v", last)
	}
}

func TestTwoFactorRequirementRefusesMembersWithoutIt(t *testing.T) {
	e := newEnv(t)
	owner, member, enrolled, sso, outsider := e.passwordUser(), e.passwordUser(), e.passwordUser(), e.ssoUser(), e.passwordUser()
	e.enrol(owner)
	e.enrol(enrolled)
	c := e.organization(owner, "Acme")
	other := e.organization(member, "Elsewhere")
	for _, u := range []uuid.UUID{member, enrolled, sso} {
		e.member(c.ID, u, RoleManager)
	}
	workspacePath := "/api/app/workspaces/" + c.WorkspaceID.String()

	// During the grace period nobody is refused yet; the member is only told.
	e.enforce(c.ID, time.Hour)
	e.ok(e.do(member, http.MethodGet, workspacePath, nil), http.StatusOK, nil)

	e.enforce(c.ID, -time.Minute)
	for _, path := range []string{workspacePath, orgPath(c.ID, "/members"), orgPath(c.ID, "/workspaces")} {
		rec := e.do(member, http.MethodGet, path, nil)
		e.fails(rec, http.StatusForbidden, authz.CodeTwoFactorRequired, "")
		if want := `"organizationId":"` + c.ID.String() + `"`; !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("%s: %s, want the organization in the details", path, rec.Body.String())
		}
	}
	// Writes are refused too.
	e.fails(e.do(member, http.MethodPatch, orgPath(c.ID, ""), map[string]any{"displayTimeZone": "Asia/Tokyo"}), http.StatusForbidden, authz.CodeTwoFactorRequired, "")

	// The organization's page stays readable, so the browser can name it on the guide to enrolment, and its routes lead there.
	var d Detail
	e.ok(e.do(member, http.MethodGet, orgPath(c.ID, ""), nil), http.StatusOK, &d)
	if !d.TwoFactorBlocked || d.TwoFactorStatus != authz.SecondFactorDisabled {
		t.Fatalf("detail %q %v, want blocked", d.TwoFactorStatus, d.TwoFactorBlocked)
	}
	if !limited(d.Organization) || d.TwoFactorEnforcedAt == nil {
		t.Fatalf("detail %+v, want the organization limited to its name and requirement", d.Organization)
	}
	var nav Navigation
	e.ok(e.do(member, http.MethodGet, orgPath(c.ID, "/navigation"), nil), http.StatusOK, &nav)
	if nav.Path != TwoFactorPath(c.ID) || nav.CanCreateWorkspace {
		t.Fatalf("navigation %+v", nav)
	}
	var list listResponse
	e.ok(e.do(member, http.MethodGet, "/api/app/organizations", nil), http.StatusOK, &list)
	if len(list.Organizations) != 2 {
		t.Fatalf("organizations %+v, want both still listed", list.Organizations)
	}
	for _, o := range list.Organizations {
		if limited(o.Organization) != (o.ID == c.ID) || (o.ID == c.ID && o.TwoFactorEnforcedAt == nil) {
			t.Fatalf("listed %s %+v: only the refusing organization is limited, its requirement kept", o.Name, o.Organization)
		}
	}
	// Its workspaces are left out of the root page's; the member's own organization is unaffected.
	var home Home
	e.ok(e.do(member, http.MethodGet, "/api/app/home", nil), http.StatusOK, &home)
	if home.Path != "" || len(home.WorkspaceIDs) != 1 || home.WorkspaceIDs[0] != other.WorkspaceID {
		t.Fatalf("home %+v, want only %s", home, other.WorkspaceID)
	}
	e.ok(e.do(member, http.MethodGet, "/api/app/workspaces/"+other.WorkspaceID.String(), nil), http.StatusOK, nil)

	// Exempt and enrolled members keep their access; a non-member learns nothing about the requirement.
	e.ok(e.do(enrolled, http.MethodGet, workspacePath, nil), http.StatusOK, nil)
	e.ok(e.do(sso, http.MethodGet, workspacePath, nil), http.StatusOK, nil)
	e.ok(e.do(owner, http.MethodGet, workspacePath, nil), http.StatusOK, nil)
	e.fails(e.do(outsider, http.MethodGet, workspacePath, nil), http.StatusForbidden, apperr.CodeForbidden, "")

	// Turning two-factor authentication on gives the access back at once.
	e.enrol(member)
	e.ok(e.do(member, http.MethodGet, workspacePath, nil), http.StatusOK, nil)
	e.ok(e.do(member, http.MethodGet, orgPath(c.ID, "/navigation"), nil), http.StatusOK, &nav)
	if nav.Path == TwoFactorPath(c.ID) {
		t.Fatalf("navigation %+v after enrolling", nav)
	}
}

// A member whose only organization refuses them is sent from the root page to enrol.
func TestHomeGuidesARefusedMemberToEnrol(t *testing.T) {
	e := newEnv(t)
	owner, member := e.passwordUser(), e.passwordUser()
	e.enrol(owner)
	c := e.organization(owner, "Acme")
	e.member(c.ID, member, RoleMember)
	e.enforce(c.ID, -time.Minute)

	var home Home
	e.ok(e.do(member, http.MethodGet, "/api/app/home", nil), http.StatusOK, &home)
	if home.Path != TwoFactorPath(c.ID) || len(home.WorkspaceIDs) != 0 {
		t.Fatalf("home %+v", home)
	}
}

func TestMembersShowTwoFactorToManagers(t *testing.T) {
	e := newEnv(t)
	owner, member, sso, linked := e.passwordUser(), e.passwordUser(), e.ssoUser(), e.passwordUser()
	e.linkSSO(linked)
	e.enrol(owner)
	c := e.organization(owner, "Acme")
	for _, u := range []uuid.UUID{member, sso, linked} {
		e.member(c.ID, u, RoleMember)
	}

	var res membersResponse
	e.ok(e.do(owner, http.MethodGet, orgPath(c.ID, "/members"), nil), http.StatusOK, &res)
	got := map[uuid.UUID]string{}
	for _, m := range res.Members {
		got[m.UserID] = m.TwoFactor
	}
	if got[owner] != authz.SecondFactorEnabled || got[member] != authz.SecondFactorDisabled || got[sso] != authz.SecondFactorSSO || got[linked] != authz.SecondFactorDisabled {
		t.Fatalf("two-factor statuses %v", got)
	}

	// A plain member does not learn who is unprotected.
	var seen membersResponse
	e.ok(e.do(member, http.MethodGet, orgPath(c.ID, "/members"), nil), http.StatusOK, &seen)
	for _, m := range seen.Members {
		if m.TwoFactor != "" {
			t.Fatalf("a member sees %+v", m)
		}
	}
}

// A member the requirement refuses may leave the organization rather than turn it on.
func TestARefusedMemberMayLeave(t *testing.T) {
	e := newEnv(t)
	owner, member := e.passwordUser(), e.passwordUser()
	e.enrol(owner)
	c := e.organization(owner, "Acme")
	e.organization(member, "Elsewhere")
	e.member(c.ID, member, RoleMember)
	e.enforce(c.ID, -time.Minute)

	e.fails(e.do(member, http.MethodGet, "/api/app/workspaces/"+c.WorkspaceID.String(), nil), http.StatusForbidden, authz.CodeTwoFactorRequired, "")
	e.ok(e.do(member, http.MethodPost, orgPath(c.ID, "/leave"), nil), http.StatusNoContent, nil)
	if e.count(`SELECT count(*) FROM memberships WHERE user_id = ? AND organization_id = ?`, member, c.ID) != 0 {
		t.Fatal("the membership is still there")
	}
}
