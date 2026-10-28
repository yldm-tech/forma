package org

import (
	"net/http"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
)

const creationPath = "/api/app/organization-creation"

// inviteOnly closes public sign-up: neither Forma Cloud nor an end-to-end run.
func inviteOnly(c *config.Config) { c.E2ETesting, c.IsFormaCloud = false, false }

// creationAllowed reads what the SPA is told about offering "Create organization".
func (e *env) creationAllowed(user uuid.UUID) bool {
	e.t.Helper()
	var res creationResponse
	e.ok(e.do(user, http.MethodGet, creationPath, nil), http.StatusOK, &res)
	return res.Allowed
}

func (e *env) bareOrganization(name string) uuid.UUID {
	return e.insert(`INSERT INTO organizations (name) VALUES (?) RETURNING id`, name)
}

// On an invite-only instance only an active owner of an organization may create another one: invitees in any other role, a user without an organization (an SSO sign-in with no default organization, or someone removed from theirs) and an owner whose access is suspended are refused with a stable code, and the SPA is told not to offer it.
func TestOrganizationCreationOnInviteOnlyInstance(t *testing.T) {
	e := newEnv(t, inviteOnly)
	acme := e.bareOrganization("Acme")

	owner := e.user()
	e.member(acme, owner, RoleOwner)
	if !e.creationAllowed(owner) {
		t.Fatal("an owner is not offered to create an organization")
	}
	created := e.organization(owner, "Beta")
	if e.count(`SELECT count(*) FROM memberships WHERE organization_id = ? AND user_id = ? AND role = 'owner'`, created.ID, owner) != 1 {
		t.Fatal("the owner did not become the new organization's owner")
	}

	refused := map[string]uuid.UUID{"no organization": e.user()}
	for _, role := range []string{RoleManager, RoleMember, RoleBilling} {
		u := e.user()
		e.member(acme, u, role)
		refused[role] = u
	}
	suspended := e.user()
	e.member(acme, suspended, RoleOwner)
	e.exec(`UPDATE memberships SET suspended_at = now() WHERE user_id = ? AND organization_id = ?`, suspended, acme)
	refused["suspended owner"] = suspended
	unaccepted := e.user()
	e.exec(`INSERT INTO memberships (user_id, organization_id, role, accepted) VALUES (?, ?, 'owner', false)`, unaccepted, acme)
	refused["unaccepted owner"] = unaccepted

	before := e.count(`SELECT count(*) FROM organizations`)
	for name, u := range refused {
		if e.creationAllowed(u) {
			t.Errorf("%s: offered to create an organization", name)
		}
		rec := e.do(u, http.MethodPost, "/api/app/organizations", map[string]string{"name": "Mine"})
		e.fails(rec, http.StatusForbidden, CodeOrganizationCreationRestricted, "")
	}
	if after := e.count(`SELECT count(*) FROM organizations`); after != before {
		t.Fatalf("%d organizations after the refusals, want %d", after, before)
	}

	// A manager who is also the owner of an organization of their own may create more.
	manager := refused[RoleManager]
	e.member(e.bareOrganization("Manager's own"), manager, RoleOwner)
	if !e.creationAllowed(manager) {
		t.Fatal("an owner elsewhere is not offered to create an organization")
	}
	e.organization(manager, "Gamma")

	// A refusal is audited as a failed creation.
	failed := 0
	for _, ev := range e.auditEvents() {
		if ev["action"] == "created" && ev["target"].(map[string]any)["type"] == "organization" && ev["status"] == "failure" {
			failed++
		}
	}
	if failed != len(refused) {
		t.Fatalf("%d failed creations audited, want %d", failed, len(refused))
	}

	e.fails(e.do(uuid.Nil, http.MethodGet, creationPath, nil), http.StatusUnauthorized, apperr.CodeNotAuthenticated, "")
}

// Where public sign-up is open (Forma Cloud, an end-to-end run), anyone signed in may create organizations, as before.
func TestOrganizationCreationWhereSignupIsOpen(t *testing.T) {
	for name, configure := range map[string]func(*config.Config){
		"forma cloud": func(c *config.Config) { c.E2ETesting, c.IsFormaCloud = false, true },
		"e2e testing": func(c *config.Config) { c.E2ETesting, c.IsFormaCloud = true, false },
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t, configure)
			acme := e.bareOrganization("Acme")
			member := e.user()
			e.member(acme, member, RoleMember)
			for _, u := range []uuid.UUID{e.user(), member} {
				if !e.creationAllowed(u) {
					t.Fatal("not offered to create an organization")
				}
				e.organization(u, "Mine")
			}
		})
	}
}

// An invite-only instance with no organization lets whoever signs in create the first, as sign-up does for its first user: on an SSO-only instance without a default organization nothing else would ever create one. Once it exists the rule is back: a second user without an organization is refused.
func TestOrganizationCreationBootstrapsAnInviteOnlyInstance(t *testing.T) {
	e := newEnv(t, inviteOnly)
	first, second := e.user(), e.user()
	if !e.creationAllowed(first) {
		t.Fatal("the first user of an instance without organizations is not offered to create one")
	}
	created := e.organization(first, "Acme")
	if e.count(`SELECT count(*) FROM memberships WHERE organization_id = ? AND user_id = ? AND role = 'owner' AND accepted`, created.ID, first) != 1 {
		t.Fatal("the first user did not become the organization's owner")
	}
	if e.creationAllowed(second) {
		t.Fatal("a second user without an organization is offered to create one")
	}
	e.fails(e.do(second, http.MethodPost, "/api/app/organizations", map[string]string{"name": "Mine"}), http.StatusForbidden, CodeOrganizationCreationRestricted, "")
	if !e.creationAllowed(first) {
		t.Fatal("the first organization's owner is not offered to create another")
	}
}

// Users racing for an invite-only instance's first organization are serialized: exactly one of them gets it.
func TestOrganizationCreationBootstrapRace(t *testing.T) {
	e := newEnv(t, inviteOnly)
	const racers = 6
	users := make([]uuid.UUID, racers)
	for i := range users {
		users[i] = e.user()
	}
	e.authz.Deliver(t)
	codes := make(chan int, racers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for _, u := range users {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			codes <- e.doUndelivered(u, http.MethodPost, "/api/app/organizations", map[string]string{"name": "First"}).Code
		}()
	}
	close(start)
	wg.Wait()
	close(codes)
	created := 0
	for code := range codes {
		switch code {
		case http.StatusCreated:
			created++
		case http.StatusForbidden:
		default:
			t.Errorf("unexpected status %d", code)
		}
	}
	if n := e.count(`SELECT count(*) FROM organizations`); created != 1 || n != 1 {
		t.Fatalf("%d creations answered 201 and %d organizations exist, want 1 and 1", created, n)
	}
}
