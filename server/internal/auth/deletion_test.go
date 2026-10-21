package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
	e := newEnv(t)
	expectStatus(t, e.browser().post("/auth/account/delete", map[string]string{"password": strongPassword}), http.StatusUnauthorized, apperr.CodeNotAuthenticated)
}

// forma2's better-auth-account-deletion-request tests: a user without a password confirms by mail; the link deletes only from that user's own session.
func TestDeleteAccountByEmailLink(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	e.seedUser("ada@example.com", strongPassword, userOpts{})
	signedIn := e.browser()
	signedIn.signIn("ada@example.com", strongPassword)
	e.enableRateLimits()
	e.inOneWindow(time.Hour, func(run int, ip string) error {
		sso := e.sessionFor(e.seedUser(fmt.Sprintf("sso%d@example.com", run), "", userOpts{noPassword: true, sso: true}))
		b := e.browser()
		b.ip = ip
		b.cookies = signedIn.cookies
		if err := repeat(5, func() response { return sso.post("/auth/account/delete/request", nil) }, http.StatusOK, ""); err != nil {
			return err
		}
		if err := statusErr(sso.post("/auth/account/delete/request", nil), http.StatusTooManyRequests, ""); err != nil {
			return err
		}
		guess := func() response {
			return b.post("/auth/account/delete", map[string]string{"password": "Guess-Number-1"})
		}
		if err := repeat(5, guess, http.StatusBadRequest, CodeInvalidPassword); err != nil {
			return err
		}
		return statusErr(b.post("/auth/account/delete", map[string]string{"password": strongPassword}), http.StatusTooManyRequests, "")
	})
}

// A deactivated co-owner can never sign in again, so an organization whose only active owner deletes their account goes with it, as the organization module's owner guards count only active owners.
func TestDeleteAccountWithOnlyADeactivatedCoOwner(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	id := e.seedUser("ada@example.com", strongPassword, userOpts{})
	inactive := e.seedUser("grace@example.com", strongPassword, userOpts{})
	active := e.seedUser("alan@example.com", strongPassword, userOpts{})
	e.exec(`UPDATE users SET is_active = false WHERE id = ?`, inactive)
	abandoned := e.orgWith(map[uuid.UUID]string{id: "owner", inactive: "owner"})
	kept := e.orgWith(map[uuid.UUID]string{id: "owner", inactive: "owner", active: "owner"})

	b := e.browser()
	b.signIn("ada@example.com", strongPassword)
	expectStatus(t, b.post("/auth/account/delete", map[string]string{"password": strongPassword}), http.StatusOK, "")
	if e.count(`SELECT count(*) FROM organizations WHERE id = ?`, abandoned) != 0 {
		t.Error("an organization left with only a deactivated owner survived")
	}
	if e.count(`SELECT count(*) FROM organizations WHERE id = ?`, kept) != 1 {
		t.Error("an organization with another active owner was deleted")
	}
}

// The deletion's sole-owner check and a co-owner's concurrent change to the same organization's owners are both Serializable, so PostgreSQL aborts one of them: the organization never ends up without an active owner. The co-owner's transaction is held open across the deletion to force the interleaving.
func TestDeleteAccountRacesTheOtherOwner(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		// change is what the co-owner does in their own Serializable transaction, after reading the owners as the organization module's guards do.
		change func(tx *gorm.DB, coOwner uuid.UUID) error
	}{
		{"the co-owner demotes themself", func(tx *gorm.DB, coOwner uuid.UUID) error {
			return tx.Exec(`UPDATE memberships SET role = 'manager' WHERE user_id = ?`, coOwner).Error
		}},
		{"the co-owner deletes their account", func(tx *gorm.DB, coOwner uuid.UUID) error {
			repo := &Repository{db: tx}
			soleOwned, err := repo.soleOwnedOrganizations(context.Background(), coOwner)
			if err != nil {
				return err
			}
			return repo.deleteAccount(context.Background(), coOwner, soleOwned)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t)
			id := e.seedUser("ada@example.com", strongPassword, userOpts{})
			coOwner := e.seedUser("grace@example.com", strongPassword, userOpts{})
			org := e.orgWith(map[uuid.UUID]string{id: "owner", coOwner: "owner"})
			b := e.browser()
			b.signIn("ada@example.com", strongPassword)

			tx := e.db.Begin(&sql.TxOptions{Isolation: sql.LevelSerializable})
			if tx.Error != nil {
				t.Fatal(tx.Error)
			}
			var owners int64
			if err := tx.Raw(`SELECT count(*) FROM memberships m JOIN users u ON u.id = m.user_id WHERE m.organization_id = ? AND m.role = 'owner' AND u.is_active`, org).Scan(&owners).Error; err != nil || owners != 2 {
				t.Fatalf("%d owners, %v", owners, err)
			}
			if err := tc.change(tx, coOwner); err != nil {
				t.Fatal(err)
			}
			deleted := b.post("/auth/account/delete", map[string]string{"password": strongPassword})
			committed := tx.Commit().Error
			t.Logf("deletion answered %d, the co-owner's commit returned %v", deleted.Code, committed)

			if e.count(`SELECT count(*) FROM organizations WHERE id = ?`, org) == 1 &&
				e.count(`SELECT count(*) FROM memberships m JOIN users u ON u.id = m.user_id WHERE m.organization_id = ? AND m.role = 'owner' AND u.is_active`, org) == 0 {
				t.Fatalf("the organization was left without an active owner (deletion %d, co-owner commit %v)", deleted.Code, committed)
			}
		})
	}
}
