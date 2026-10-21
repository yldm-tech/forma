package auth

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/platform/audit"
	"github.com/yldm-tech/forma/server/internal/platform/config"
)

// orgWith creates an organization whose memberships are given as user id to role.
func (e *env) orgWith(members map[uuid.UUID]string) uuid.UUID {
	e.t.Helper()
	org := e.insertID(`INSERT INTO organizations (name) VALUES ('Org') RETURNING id`)
	for user, role := range members {
		e.exec(`INSERT INTO memberships (user_id, organization_id, role, accepted) VALUES (?, ?, ?, true)`, user, org, role)
	}
	return org
}

// forma2's better-auth-account-deletion integration tests: the password is required, sole-owned organizations and sent invites go with the account, and the rest cascades.
func TestDeleteAccount(t *testing.T) {
	e := newEnv(t)
	id := e.seedUser("ada@example.com", strongPassword, userOpts{})
	other := e.seedUser("grace@example.com", strongPassword, userOpts{})
	soleOwned := e.orgWith(map[uuid.UUID]string{id: "owner", other: "member"})
	shared := e.orgWith(map[uuid.UUID]string{id: "owner", other: "owner"})
	membership := e.orgWith(map[uuid.UUID]string{other: "owner", id: "member"})
	e.exec(`INSERT INTO invites (organization_id, email, creator_id, expires_at) VALUES (?, 'x@example.com', ?, now() + interval '1 day')`, shared, id)

	b := e.browser()
	b.signIn("ada@example.com", strongPassword)
	r := b.post("/auth/account/delete", map[string]string{})
	expectStatus(t, r, http.StatusBadRequest, CodePasswordRequired)
	if r.json()["message"] != "Password confirmation is required to delete your account." {
		t.Fatalf("message %v", r.json()["message"])
	}
	expectStatus(t, b.post("/auth/account/delete", map[string]string{"password": "Wrong-Password-1"}), http.StatusBadRequest, CodeInvalidPassword)
	if e.count(`SELECT count(*) FROM users WHERE id = ?`, id) != 1 {
		t.Fatal("deleted without the password")
	}

	r = b.post("/auth/account/delete", map[string]string{"password": strongPassword})
	expectStatus(t, r, http.StatusOK, "")
	if r.json()["redirectUrl"] != "/auth/login" || b.has(sessionCookieName) {
		t.Fatalf("delete: %s, cookies %v", r.Body.String(), b.cookies)
	}
	for _, q := range []string{
		`SELECT count(*) FROM users WHERE id = ?`,
		`SELECT count(*) FROM sessions WHERE user_id = ?`,
		`SELECT count(*) FROM user_identities WHERE user_id = ?`,
		`SELECT count(*) FROM memberships WHERE user_id = ?`,
		`SELECT count(*) FROM invites WHERE creator_id = ?`,
	} {
		if n := e.count(q, id); n != 0 {
			t.Errorf("%s: %d rows left", q, n)
		}
	}
	if e.count(`SELECT count(*) FROM organizations WHERE id = ?`, soleOwned) != 0 {
		t.Error("the sole-owned organization survived")
	}
	if e.count(`SELECT count(*) FROM organizations WHERE id IN (?, ?)`, shared, membership) != 2 {
		t.Error("an organization with another owner was deleted")
	}
	// The SpiceDB projection follows through the outbox triggers.
	if e.count(`SELECT count(*) FROM authz_projection_outbox WHERE target_type = 'user' AND primary_id = ? AND is_revocation`, id.String()) == 0 {
		t.Error("no revocation was queued for the deleted user")
	}
	events := e.auditEvents(audit.ActionDeleted)
	if len(events) != 1 || field(events[0], "target", "id") != id.String() || field(events[0], "changes", "email") != "********" {
		t.Fatalf("deleted events %v", events)
	}
	expectStatus(t, e.browser().post("/auth/sign-in", credentials("ada@example.com", strongPassword)), http.StatusUnauthorized, "")
}

func TestDeleteAccountOnFormaCloudGoesToTheSurvey(t *testing.T) {
	e := newEnv(t, func(c *config.Config) { c.IsFormaCloud = true })
	e.seedUser("ada@example.com", strongPassword, userOpts{})
	b := e.browser()
	b.signIn("ada@example.com", strongPassword)
	r := b.post("/auth/account/delete", map[string]string{"password": strongPassword})
	expectStatus(t, r, http.StatusOK, "")
	if r.json()["redirectUrl"] != cloudAccountDeletionSurveyURL {
		t.Fatalf("redirect %v", r.json()["redirectUrl"])
	}
}

func TestDeleteAccountNeedsASession(t *testing.T) {
	e := newEnv(t)
	expectStatus(t, e.browser().post("/auth/account/delete", map[string]string{"password": strongPassword}), http.StatusUnauthorized, apperr.CodeNotAuthenticated)
}

// forma2's better-auth-account-deletion-request tests: a user without a password confirms by mail; the link deletes only from that user's own session.
func TestDeleteAccountByEmailLink(t *testing.T) {
	e := newEnv(t)
	sso := e.seedUser("sso@example.com", "", userOpts{noPassword: true, sso: true})
	other := e.seedUser("grace@example.com", strongPassword, userOpts{})
	b := e.sessionFor(sso)

	r := b.post("/auth/account/delete/request", nil)
	expectStatus(t, r, http.StatusOK, "")
	if r.json()["emailSent"] != true {
		t.Fatalf("body %s", r.Body.String())
	}
	msgs := e.sent("sso@example.com")
	if len(msgs) != 1 || msgs[0].Subject != subjectDeletion || !strings.Contains(msgs[0].HTML, webApp+deleteAccountPath+"?token=") || !strings.Contains(msgs[0].HTML, "60 minutes") {
		t.Fatalf("mails %+v", msgs)
	}
	token := e.lastToken("sso@example.com", subjectDeletion)

	// Another user's session cannot use the link, and does not spend it.
	expectStatus(t, e.sessionFor(other).post("/auth/account/delete/confirm", map[string]string{"token": token}), http.StatusBadRequest, CodeInvalidToken)
	expectStatus(t, e.browser().post("/auth/account/delete/confirm", map[string]string{"token": token}), http.StatusUnauthorized, apperr.CodeNotAuthenticated)

	r = b.post("/auth/account/delete/confirm", map[string]string{"token": token})
	expectStatus(t, r, http.StatusOK, "")
	if r.json()["redirectUrl"] != "/auth/login" || e.count(`SELECT count(*) FROM users WHERE id = ?`, sso) != 0 {
		t.Fatalf("confirm: %s", r.Body.String())
	}
	if e.count(`SELECT count(*) FROM users WHERE id = ?`, other) != 1 {
		t.Fatal("the other user was deleted")
	}
}

func TestDeleteAccountLinkRules(t *testing.T) {
	e := newEnv(t)
	password := e.browser()
	e.seedUser("ada@example.com", strongPassword, userOpts{})
	password.signIn("ada@example.com", strongPassword)
	r := password.post("/auth/account/delete/request", nil)
	expectStatus(t, r, http.StatusForbidden, apperr.CodeForbidden)
	if r.json()["message"] != "Password confirmation is required to delete this account." {
		t.Fatalf("message %v", r.json()["message"])
	}

	sso := e.seedUser("sso@example.com", "", userOpts{noPassword: true, sso: true})
	b := e.sessionFor(sso)
	expectStatus(t, b.post("/auth/account/delete/request", nil), http.StatusOK, "")
	token := e.lastToken("sso@example.com", subjectDeletion)
	e.advance(time.Hour + time.Second)
	expectStatus(t, b.post("/auth/account/delete/confirm", map[string]string{"token": token}), http.StatusBadRequest, CodeInvalidToken)
	if e.count(`SELECT count(*) FROM users WHERE id = ?`, sso) != 1 {
		t.Fatal("an expired link deleted the account")
	}

	e.mail.Err = errors.New("smtp down")
	expectStatus(t, b.post("/auth/account/delete/request", nil), http.StatusBadRequest, CodeEmailSendFailed)
}

// action:account-delete allows five deletion mails per user per hour; Better Auth's /delete-user allowed five password attempts per address per hour.
func TestDeleteAccountRateLimits(t *testing.T) {
	e := newEnv(t)
	sso := e.sessionFor(e.seedUser("sso@example.com", "", userOpts{noPassword: true, sso: true}))
	e.seedUser("ada@example.com", strongPassword, userOpts{})
	b := e.browser()
	b.signIn("ada@example.com", strongPassword)
	e.enableRateLimits()
	for i := 0; i < 5; i++ {
		expectStatus(t, sso.post("/auth/account/delete/request", nil), http.StatusOK, "")
		expectStatus(t, b.post("/auth/account/delete", map[string]string{"password": "Guess-Number-1"}), http.StatusBadRequest, CodeInvalidPassword)
	}
	expectStatus(t, sso.post("/auth/account/delete/request", nil), http.StatusTooManyRequests, "")
	expectStatus(t, b.post("/auth/account/delete", map[string]string{"password": strongPassword}), http.StatusTooManyRequests, "")
}
