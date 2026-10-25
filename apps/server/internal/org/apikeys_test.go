package org

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// apiKey stores a key userID created in org, as the API keys module does, and returns its id. Authentication is not exercised here, so the hashes are placeholders.
func (e *env) apiKey(org, userID uuid.UUID) uuid.UUID {
	e.t.Helper()
	return e.insert(`INSERT INTO api_keys (organization_id, created_by, label, hashed_key, lookup_hash) VALUES (?, ?, 'key', 'x', ?) RETURNING id`, org, userID, uuid.NewString())
}

// disabledReason is the key's disabled_reason, or "" while it is enabled.
func (e *env) disabledReason(key uuid.UUID) string {
	e.t.Helper()
	var reason *string
	e.scalar(&reason, `SELECT disabled_reason FROM api_keys WHERE id = ?`, key)
	if reason == nil {
		return ""
	}
	return *reason
}

// Owner decision D-AO02: a member who is removed, leaves, or is demoted below manager loses the API keys they created in the organization, which are disabled in the same transaction with one audit event each. Their keys elsewhere, the keys of other creators and a promotion leave keys alone.
func TestMembershipChangesDisableTheMembersAPIKeys(t *testing.T) {
	e := newEnv(t)
	owner, removed, leaver, demoted, promoted := e.user(), e.user(), e.user(), e.user(), e.user()
	c := e.organization(owner, "Acme")
	other := e.organization(leaver, "Other")
	for _, u := range []uuid.UUID{removed, leaver, demoted, promoted} {
		e.member(c.ID, u, RoleManager)
	}
	keys := map[string]uuid.UUID{
		"removed":          e.apiKey(c.ID, removed),
		"leaver":           e.apiKey(c.ID, leaver),
		"leaver elsewhere": e.apiKey(other.ID, leaver),
		"demoted":          e.apiKey(c.ID, demoted),
		"promoted":         e.apiKey(c.ID, promoted),
		"owner":            e.apiKey(c.ID, owner),
	}

	e.ok(e.do(owner, http.MethodDelete, memberPath(c.ID, removed), nil), http.StatusNoContent, nil)
	e.ok(e.do(leaver, http.MethodPost, orgPath(c.ID, "/leave"), nil), http.StatusNoContent, nil)
	e.ok(e.do(owner, http.MethodPatch, memberPath(c.ID, demoted), map[string]string{"role": RoleMember}), http.StatusOK, nil)
	e.ok(e.do(owner, http.MethodPatch, memberPath(c.ID, promoted), map[string]string{"role": RoleOwner}), http.StatusOK, nil)

	want := map[string]string{"removed": "creator_removed", "leaver": "creator_left", "demoted": "creator_demoted", "leaver elsewhere": "", "promoted": "", "owner": ""}
	for name, key := range keys {
		if got := e.disabledReason(key); got != want[name] {
			t.Errorf("%s key: disabled_reason %q, want %q", name, got, want[name])
		}
	}
	if n := e.count(`SELECT count(*) FROM api_keys WHERE disabled_reason IS NOT NULL AND disabled_at IS NULL`); n != 0 {
		t.Fatalf("%d keys have a reason but no time", n)
	}

	// One event per disabled key, on the key, by the user who made the change.
	type event struct{ actor, reason string }
	got := map[string]event{}
	for _, rec := range e.auditEvents() {
		if rec["action"] != "disabled" {
			continue
		}
		target, _ := rec["target"].(map[string]any)
		actor, _ := rec["actor"].(map[string]any)
		changes, _ := rec["changes"].(map[string]any)
		if target["type"] != "apiKey" || rec["status"] != "success" || rec["organizationId"] != c.ID.String() || actor["type"] != "user" {
			t.Fatalf("disabled event %v", rec)
		}
		got[target["id"].(string)] = event{actor: actor["id"].(string), reason: changes["disabledReason"].(string)}
	}
	wantEvents := map[string]event{
		keys["removed"].String(): {owner.String(), "creator_removed"},
		keys["leaver"].String():  {leaver.String(), "creator_left"},
		keys["demoted"].String(): {owner.String(), "creator_demoted"},
	}
	if len(got) != len(wantEvents) {
		t.Fatalf("disabled events %v, want %v", got, wantEvents)
	}
	for id, w := range wantEvents {
		if got[id] != w {
			t.Errorf("event for %s = %+v, want %+v", id, got[id], w)
		}
	}

	// A key is disabled once: the same role sent again, or a later removal, finds nothing left to disable and records nothing.
	before := len(e.auditEvents())
	e.ok(e.do(owner, http.MethodPatch, memberPath(c.ID, demoted), map[string]string{"role": RoleMember}), http.StatusOK, nil)
	e.ok(e.do(owner, http.MethodDelete, memberPath(c.ID, demoted), nil), http.StatusNoContent, nil)
	for _, rec := range e.auditEvents()[before:] {
		if rec["action"] == "disabled" {
			t.Fatalf("a disabled key was disabled again: %v", rec)
		}
	}
	if got := e.disabledReason(keys["demoted"]); got != "creator_demoted" {
		t.Fatalf("the first reason was overwritten with %q", got)
	}
}

// A refused change disables nothing: the last owner cannot be demoted, so their keys stay enabled.
func TestRefusedMembershipChangeKeepsTheKeys(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	c := e.organization(owner, "Acme")
	key := e.apiKey(c.ID, owner)
	if rec := e.do(owner, http.MethodPatch, memberPath(c.ID, owner), map[string]string{"role": RoleMember}); rec.Code == http.StatusOK {
		t.Fatalf("the last owner was demoted: %s", rec.Body.String())
	}
	if got := e.disabledReason(key); got != "" {
		t.Fatalf("a refused demotion disabled the key (%s)", got)
	}
}

// Only a change from a role that manages keys to one that does not disables them. A key whose creator was already below manager (one created before keys were disabled on demotion, say) stays enabled when that role is sent again, and no demotion is recorded for it.
func TestUnchangedRoleBelowManagerKeepsTheCreatorsKeys(t *testing.T) {
	e := newEnv(t)
	owner, member := e.user(), e.user()
	c := e.organization(owner, "Acme")
	e.member(c.ID, member, RoleMember)
	key := e.apiKey(c.ID, member)

	e.ok(e.do(owner, http.MethodPatch, memberPath(c.ID, member), map[string]string{"role": RoleMember}), http.StatusOK, nil)

	if got := e.disabledReason(key); got != "" {
		t.Fatalf("an unchanged role disabled the key (%s)", got)
	}
	for _, rec := range e.auditEvents() {
		if rec["action"] == "disabled" {
			t.Fatalf("a role write that demoted nobody recorded %v", rec)
		}
	}
}
