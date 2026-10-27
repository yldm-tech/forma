package apikey

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/apikey/disable"
	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
)

// listed returns the key of org's list with id, failing the test when it is not listed.
func (e *appEnv) listed(org, id uuid.UUID, token string) View {
	e.t.Helper()
	res := e.do(http.MethodGet, keysPath(org), token, nil)
	if res.status != http.StatusOK {
		e.t.Fatalf("list = %d %s", res.status, res.body)
	}
	var keys []View
	res.decode(e.t, &keys)
	for _, k := range keys {
		if k.ID == id {
			return k
		}
	}
	e.t.Fatalf("key %s is not listed: %s", id, res.body)
	return View{}
}

// The list says when a key was last used, who created it and whether they still belong to the organization, and whether the key was disabled and why (F5, owner decision D-AO02). A disabled key never authenticates again and its refused uses are not recorded as uses.
func TestListShowsUseCreatorAndDisabledKeys(t *testing.T) {
	e := newAppEnv(t)
	org, owner, manager := e.org(), e.user(), e.user()
	e.member(org, owner, "owner")
	e.member(org, manager, "manager")
	e.exec(`UPDATE users SET name = 'Grace' WHERE id = ?`, manager)
	ws := e.workspace(org, "Growth")
	e.drain()
	ownerToken, managerToken := e.session(owner), e.session(manager)

	res := e.do(http.MethodPost, keysPath(org), managerToken, createBody("Manager key", readAccess, grant(ws, PermissionRead)))
	if res.status != http.StatusCreated {
		t.Fatalf("create = %d %s", res.status, res.body)
	}
	var created Created
	res.decode(t, &created)

	k := e.listed(org, created.ID, ownerToken)
	if k.CreatedBy == nil || *k.CreatedBy != (Creator{ID: manager, Name: "Grace"}) || k.LastUsedAt != nil || k.DisabledAt != nil || k.DisabledReason != nil {
		t.Fatalf("new key = %+v", k)
	}

	if e.authenticate(map[string]string{"X-Api-Key": created.Key}, Options{}) == nil {
		t.Fatal("the key does not authenticate")
	}
	k = e.listed(org, created.ID, ownerToken)
	if k.LastUsedAt == nil || !k.LastUsedAt.Equal(e.now()) {
		t.Fatalf("lastUsedAt = %v, want %v", k.LastUsedAt, e.now())
	}
	used := *k.LastUsedAt

	// The manager is removed: the organization module deletes the membership and disables their keys in one transaction.
	e.exec(`DELETE FROM memberships WHERE organization_id = ? AND user_id = ?`, org, manager)
	if _, err := disable.CreatedBy(e.ctx, e.db, manager, &org, disable.ReasonCreatorRemoved); err != nil {
		t.Fatal(err)
	}
	k = e.listed(org, created.ID, ownerToken)
	if k.CreatedBy == nil || *k.CreatedBy != (Creator{ID: manager, Name: "Grace", FormerMember: true}) {
		t.Fatalf("creator after removal = %+v, want a former member", k.CreatedBy)
	}
	if k.DisabledAt == nil || k.DisabledReason == nil || *k.DisabledReason != disable.ReasonCreatorRemoved {
		t.Fatalf("disabled = %v %v", k.DisabledAt, k.DisabledReason)
	}

	e.mu.Lock()
	e.clock = e.clock.Add(time.Hour)
	e.mu.Unlock()
	for _, opts := range []Options{{}, {AllowOrganizationOnly: true}} {
		if e.authenticate(map[string]string{"X-Api-Key": created.Key}, opts) != nil {
			t.Fatal("a disabled key authenticates")
		}
		if e.authenticate(map[string]string{"Authorization": "Bearer " + created.Key}, opts) != nil {
			t.Fatal("a disabled key authenticates as a bearer token")
		}
	}
	if at := e.lastUsedAt(created.ID); at == nil || !at.Equal(used) {
		t.Fatalf("a refused use was recorded: %v, want %v", at, used)
	}
	if warned := e.logs.records(t, "A disabled API key was used"); len(warned) == 0 || warned[0]["apiKeyId"] != created.ID.String() {
		t.Fatalf("no warning names the disabled key: %v", warned)
	}

	// Once the account is gone the creator is unknown; the key still reads as disabled.
	e.exec(`DELETE FROM users WHERE id = ?`, manager)
	k = e.listed(org, created.ID, ownerToken)
	if k.CreatedBy != nil || k.DisabledReason == nil {
		t.Fatalf("after the creator's account was deleted: %+v", k)
	}

	// A disabled key can still be relabelled and deleted.
	if res := e.do(http.MethodPatch, keyPath(org, created.ID), ownerToken, map[string]string{"label": "Old"}); res.status != http.StatusOK {
		t.Fatalf("relabel a disabled key = %d %s", res.status, res.body)
	}
	if res := e.do(http.MethodDelete, keyPath(org, created.ID), ownerToken, nil); res.status != http.StatusNoContent {
		t.Fatalf("delete a disabled key = %d %s", res.status, res.body)
	}
}

// SpiceDB follows a demotion only once the outbox delivers it, so a manager demoted a moment ago still passes the authorization check; the create itself refuses them, in the transaction that would insert the key, so no key outlives the demotion that should have disabled it.
func TestCreateIsRefusedOnceTheCreatorLostTheRole(t *testing.T) {
	e := newAppEnv(t)
	org, manager := e.org(), e.user()
	e.member(org, manager, "manager")
	e.drain()
	token := e.session(manager)

	e.exec(`UPDATE memberships SET role = 'member' WHERE organization_id = ? AND user_id = ?`, org, manager)
	res := e.do(http.MethodPost, keysPath(org), token, createBody("Late", readAccess))
	if res.status != http.StatusForbidden || res.code(t) != apperr.CodeForbidden {
		t.Fatalf("create after the demotion = %d %s", res.status, res.body)
	}
	if n := e.count(`SELECT count(*) FROM api_keys WHERE organization_id = ?`, org); n != 0 {
		t.Fatalf("%d keys created", n)
	}
}

// A key whose creator the organization suspended stops authenticating while the suspension lasts, without being disabled, and works again once it is restored (owner decision D-AO01); the list says why. A suspended manager cannot create a key either.
func TestAKeyOfASuspendedCreatorWaitsForTheRestore(t *testing.T) {
	e := newAppEnv(t)
	org, owner, manager := e.org(), e.user(), e.user()
	e.member(org, owner, "owner")
	e.member(org, manager, "manager")
	e.exec(`UPDATE users SET name = 'Grace' WHERE id = ?`, manager)
	ws := e.workspace(org, "Growth")
	e.drain()
	ownerToken, managerToken := e.session(owner), e.session(manager)

	res := e.do(http.MethodPost, keysPath(org), managerToken, createBody("Manager key", readAccess, grant(ws, PermissionRead)))
	if res.status != http.StatusCreated {
		t.Fatalf("create = %d %s", res.status, res.body)
	}
	var created Created
	res.decode(t, &created)
	headers := map[string]string{"X-Api-Key": created.Key}
	if e.authenticate(headers, Options{}) == nil {
		t.Fatal("the key does not authenticate")
	}

	e.exec(`UPDATE memberships SET suspended_at = now() WHERE organization_id = ? AND user_id = ?`, org, manager)
	for _, opts := range []Options{{}, {AllowOrganizationOnly: true}} {
		if e.authenticate(headers, opts) != nil {
			t.Fatal("a suspended creator's key authenticates")
		}
	}
	if warned := e.logs.records(t, "An API key of a suspended member was used"); len(warned) == 0 || warned[0]["apiKeyId"] != created.ID.String() {
		t.Fatalf("no warning names the key: %v", warned)
	}
	k := e.listed(org, created.ID, ownerToken)
	if k.CreatedBy == nil || *k.CreatedBy != (Creator{ID: manager, Name: "Grace", Suspended: true}) || k.DisabledAt != nil {
		t.Fatalf("key of a suspended creator = %+v, creator %+v", k, k.CreatedBy)
	}
	res = e.do(http.MethodPost, keysPath(org), managerToken, createBody("Late", readAccess))
	if res.status != http.StatusForbidden {
		t.Fatalf("create while suspended = %d %s", res.status, res.body)
	}

	e.exec(`UPDATE memberships SET suspended_at = NULL WHERE organization_id = ? AND user_id = ?`, org, manager)
	if e.authenticate(headers, Options{}) == nil {
		t.Fatal("the key does not authenticate after the restore")
	}
	if k := e.listed(org, created.ID, ownerToken); k.CreatedBy == nil || k.CreatedBy.Suspended {
		t.Fatalf("creator after the restore = %+v", k.CreatedBy)
	}
}

// A key whose creator's account the instance's operator deactivated (forma user deactivate) stops authenticating while the account is off, without being disabled, and works again once it is reactivated, as a suspension does; the list says why (AUTH-02).
func TestAKeyOfADeactivatedCreatorWaitsForTheReactivation(t *testing.T) {
	e := newAppEnv(t)
	org, owner, manager := e.org(), e.user(), e.user()
	e.member(org, owner, "owner")
	e.member(org, manager, "manager")
	e.exec(`UPDATE users SET name = 'Grace' WHERE id = ?`, manager)
	ws := e.workspace(org, "Growth")
	e.drain()
	ownerToken, managerToken := e.session(owner), e.session(manager)

	res := e.do(http.MethodPost, keysPath(org), managerToken, createBody("Manager key", readAccess, grant(ws, PermissionRead)))
	if res.status != http.StatusCreated {
		t.Fatalf("create = %d %s", res.status, res.body)
	}
	var created Created
	res.decode(t, &created)
	if e.authenticate(map[string]string{"X-Api-Key": created.Key}, Options{}) == nil {
		t.Fatal("the key does not authenticate")
	}

	e.exec(`UPDATE users SET is_active = false WHERE id = ?`, manager)
	for _, opts := range []Options{{}, {AllowOrganizationOnly: true}} {
		if e.authenticate(map[string]string{"X-Api-Key": created.Key}, opts) != nil {
			t.Fatal("a deactivated creator's key authenticates")
		}
		if e.authenticate(map[string]string{"Authorization": "Bearer " + created.Key}, opts) != nil {
			t.Fatal("a deactivated creator's key authenticates as a bearer token")
		}
	}
	if warned := e.logs.records(t, "An API key of a deactivated user was used"); len(warned) == 0 || warned[0]["apiKeyId"] != created.ID.String() {
		t.Fatalf("no warning names the key: %v", warned)
	}
	k := e.listed(org, created.ID, ownerToken)
	if k.CreatedBy == nil || *k.CreatedBy != (Creator{ID: manager, Name: "Grace", Deactivated: true}) || k.DisabledAt != nil {
		t.Fatalf("key of a deactivated creator = %+v, creator %+v", k, k.CreatedBy)
	}

	e.exec(`UPDATE users SET is_active = true WHERE id = ?`, manager)
	if e.authenticate(map[string]string{"X-Api-Key": created.Key}, Options{}) == nil {
		t.Fatal("the key does not authenticate after the reactivation")
	}
	if k := e.listed(org, created.ID, ownerToken); k.CreatedBy == nil || k.CreatedBy.Deactivated {
		t.Fatalf("creator after the reactivation = %+v", k.CreatedBy)
	}
}
