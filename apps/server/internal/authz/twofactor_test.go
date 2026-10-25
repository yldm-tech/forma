package authz

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
)

// passwordUser is a user who signs in with a password, the kind an organization's two-factor requirement applies to.
func (e *testEnv) passwordUser() uuid.UUID {
	id := e.user()
	e.exec(`INSERT INTO user_identities (user_id, provider, subject, password_hash) VALUES (?, 'credential', ?, '$2a$10$hash')`, id, id.String())
	return id
}

func TestTwoFactorRequirementRefusesOnlyMembersWhoMustEnrol(t *testing.T) {
	e := newTestEnv(t)
	org, other := e.org(), e.org()
	member, enrolled, sso, keyless := e.passwordUser(), e.passwordUser(), e.user(), e.passwordUser()
	e.exec(`INSERT INTO user_identities (user_id, provider, issuer, subject) VALUES (?, 'saml', 'https://idp.example.com', ?)`, sso, sso.String())
	e.exec(`INSERT INTO user_totp (user_id, secret_ciphertext, backup_codes_ciphertext, verified) VALUES (?, '\x00', '\x00', true)`, enrolled)
	for _, u := range []uuid.UUID{member, enrolled, sso} {
		e.member(org, u, "manager")
	}
	e.member(other, member, "manager")
	ws, otherWS := e.workspace(org), e.workspace(other)
	key := e.apiKey(org, true, true)
	e.apiKeyWorkspace(key, ws, "read")
	e.drain()

	// During the grace period nothing is refused.
	e.exec(`UPDATE organizations SET two_factor_enforced_at = now() + interval '1 day' WHERE id = ?`, org)
	e.expect(User(member), Workspace(ws), map[Action]bool{WorkspaceRead: true})

	e.exec(`UPDATE organizations SET two_factor_enforced_at = now() - interval '1 minute' WHERE id = ?`, org)
	e.expect(User(member), Workspace(ws), map[Action]bool{WorkspaceRead: false, WorkspaceManage: false})
	e.expect(User(member), Organization(org), map[Action]bool{OrganizationRead: true, OrganizationManage: false})
	e.expect(User(member), Workspace(otherWS), map[Action]bool{WorkspaceRead: true})
	for _, u := range []uuid.UUID{enrolled, sso} {
		e.expect(User(u), Workspace(ws), map[Action]bool{WorkspaceRead: true})
	}
	e.expect(APIKey(key), Workspace(ws), map[Action]bool{WorkspaceRead: true})

	// Require names the requirement and the organization; a non-member is only told no.
	err := e.authorizer.Require(e.ctx, User(member), WorkspaceRead, Workspace(ws))
	var ae *apperr.Error
	if !errors.As(err, &ae) || ae.Code != CodeTwoFactorRequired || ae.Details["organizationId"] != org.String() {
		t.Fatalf("Require = %v", err)
	}
	if err := e.authorizer.Require(e.ctx, User(keyless), WorkspaceRead, Workspace(ws)); err == nil || err.Error() != "forbidden: Not authorized" {
		t.Fatalf("a non-member's Require = %v", err)
	}
	if refusal, err := e.authorizer.SecondFactorRefusal(e.ctx, keyless, org); err != nil || refusal != nil {
		t.Fatalf("a non-member's refusal = %v, %v", refusal, err)
	}

	blocked, err := e.authorizer.SecondFactorBlockedOrganizations(e.ctx, member)
	if err != nil || len(blocked) != 1 || blocked[0] != org {
		t.Fatalf("blocked organizations = %v, %v", blocked, err)
	}
	orgs, err := e.authorizer.LookupOrganizationIDs(e.ctx, User(member))
	if err != nil || len(orgs) != 2 {
		t.Fatalf("organizations = %v, %v; a refused member still sees the organization", orgs, err)
	}
	workspaces, err := e.authorizer.LookupWorkspaceIDs(e.ctx, User(member), false)
	if err != nil || len(workspaces) != 1 || workspaces[0] != otherWS {
		t.Fatalf("workspaces = %v, %v, want only %s", workspaces, err, otherWS)
	}

	// An inactive member is refused for that, not for want of two-factor authentication.
	e.exec(`UPDATE users SET is_active = false WHERE id = ?`, member)
	if refusal, err := e.authorizer.SecondFactorRefusal(e.ctx, member, org); err != nil || refusal != nil {
		t.Fatalf("an inactive member's refusal = %v, %v", refusal, err)
	}
}
