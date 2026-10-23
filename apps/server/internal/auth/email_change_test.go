package auth

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/platform/audit"
	"github.com/yldm-tech/forma/server/internal/platform/config"
)

// sessionFor signs a browser in without going through the sign-in endpoint, for users who have no password.
func (e *env) sessionFor(id uuid.UUID) *browser {
	e.t.Helper()
	sess, err := e.sessions.create(e.t.Context(), e.db, id, clientMeta{})
	if err != nil {
		e.t.Fatal(err)
	}
	b := e.browser()
	b.cookies[sessionCookieName] = &http.Cookie{Name: sessionCookieName, Value: sess.Value}
	return b
}

// changeEmail requests an email change; an empty password is left out of the request.
func changeEmail(b *browser, email, password string) response {
	b.e.t.Helper()
	body := map[string]string{"email": email}
	if password != "" {
		body["password"] = password
	}
	return b.post("/auth/email/change", body)
}

func (e *env) emailOf(id uuid.UUID) (string, bool) {
	e.t.Helper()
	acc, err := e.svc.repo.accountByID(e.t.Context(), id)
	if err != nil || acc == nil {
		e.t.Fatalf("account: %v", err)
	}
	return acc.Email, acc.EmailVerified
}

// forma2's handleEmailUpdate and verifyEmailChangeAction: the change is confirmed from the new mailbox, in any browser, and the link works once.
func TestEmailChange(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	id := e.seedUser("ada@example.com", strongPassword, userOpts{unverified: true})
	b := e.browser()
	sess := e.sessionFor(id)
	expectStatus(t, b.post("/auth/email/change", map[string]string{"email": "new@example.com", "password": strongPassword}), http.StatusUnauthorized, apperr.CodeNotAuthenticated)

	expectStatus(t, changeEmail(sess, "  New@Example.com ", strongPassword), http.StatusBadRequest, apperr.CodeValidation)
	expectStatus(t, changeEmail(sess, "New@Example.com", strongPassword), http.StatusNoContent, "")
	if email, _ := e.emailOf(id); email != "ada@example.com" {
		t.Fatal("the address changed before confirmation")
	}
	if len(e.sent("ada@example.com")) != 0 {
		t.Fatal("the old address was written to")
	}
	msgs := e.sent("new@example.com")
	if len(msgs) != 1 || msgs[0].Subject != subjectEmailChange || !strings.Contains(msgs[0].HTML, webApp+verifyEmailChangePath+"?token=") {
		t.Fatalf("mails %+v", msgs)
	}
	token := e.lastToken("new@example.com", subjectEmailChange)

	r := e.browser().post("/auth/email/verify", map[string]string{"token": token})
	expectStatus(t, r, http.StatusOK, "")
	if r.json()["email"] != "new@example.com" {
		t.Fatalf("body %s", r.Body.String())
	}
	if email, verified := e.emailOf(id); email != "new@example.com" || !verified {
		t.Fatalf("account now %s verified=%v", email, verified)
	}
	expectStatus(t, e.browser().post("/auth/email/verify", map[string]string{"token": token}), http.StatusBadRequest, CodeInvalidToken)
	// forma2 kept sessions across an email change.
	expectStatus(t, sess.post("/test/protected", nil), http.StatusOK, "")
	e.browser().signIn("new@example.com", strongPassword)

	var audited bool
	for _, ev := range e.auditEvents(audit.ActionUpdated) {
		if field(ev, "target", "id") == id.String() && field(ev, "changes", "emailVerified") == true {
			audited = true
		}
	}
	if !audited {
		t.Fatal("the confirmed change was not audited")
	}
}

func TestEmailChangeRules(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	id := e.seedUser("ada@example.com", strongPassword, userOpts{})
	e.seedUser("taken@example.com", strongPassword, userOpts{})
	sess := e.sessionFor(id)

	// The same address, in any case, is a no-op.
	expectStatus(t, changeEmail(sess, "ADA@example.com", ""), http.StatusNoContent, "")
	r := changeEmail(sess, "new@example.com", "")
	expectStatus(t, r, http.StatusBadRequest, CodePasswordRequired)
	if r.json()["message"] != "Password is required to update email." {
		t.Fatalf("message %v", r.json()["message"])
	}
	r = changeEmail(sess, "new@example.com", "Wrong-Password-1")
	expectStatus(t, r, http.StatusForbidden, apperr.CodeForbidden)
	if r.json()["message"] != "Incorrect credentials" {
		t.Fatalf("message %v", r.json()["message"])
	}
	// An address another account holds is ignored without a word.
	expectStatus(t, changeEmail(sess, "Taken@example.com", strongPassword), http.StatusNoContent, "")
	if len(e.mail.Messages()) != 0 {
		t.Fatal("mail was sent for a taken address")
	}

	// A confirmation password, when sent, must meet the password policy, as in forma2's ZUserPersonalInfoUpdateInput.
	r = changeEmail(sess, "new@example.com", "weak")
	expectStatus(t, r, http.StatusBadRequest, apperr.CodeValidation)

	sso := e.sessionFor(e.seedUser("sso@example.com", "", userOpts{noPassword: true, sso: true}))
	r = changeEmail(sso, "elsewhere@example.com", "Anything-1")
	expectStatus(t, r, http.StatusForbidden, apperr.CodeOperationNotAllowed)
	if r.json()["message"] != "Email update is not allowed for non-credential users." {
		t.Fatalf("message %v", r.json()["message"])
	}

	e.mail.Err = errors.New("smtp down")
	expectStatus(t, changeEmail(sess, "new@example.com", strongPassword), http.StatusBadRequest, CodeEmailSendFailed)
}

// With EMAIL_VERIFICATION_DISABLED the address changes at once, keeping its verification state.
func TestEmailChangeWithoutVerification(t *testing.T) {
	t.Parallel()
	e := newEnv(t, func(c *config.Config) { c.EmailVerificationDisabled = true })
	id := e.seedUser("ada@example.com", strongPassword, userOpts{unverified: true})
	expectStatus(t, changeEmail(e.sessionFor(id), "new@example.com", strongPassword), http.StatusNoContent, "")
	if email, verified := e.emailOf(id); email != "new@example.com" || verified {
		t.Fatalf("account now %s verified=%v", email, verified)
	}
	if len(e.mail.Messages()) != 0 {
		t.Fatal("mail was sent")
	}
}

// ENG-2106: a password reset or change, or another email change, kills every outstanding change link; the link also expires after a day.
func TestEmailChangeLinksDie(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	id := e.seedUser("ada@example.com", strongPassword, userOpts{})
	sess := e.sessionFor(id)
	password := strongPassword
	request := func(to string) string {
		t.Helper()
		expectStatus(t, changeEmail(sess, to, password), http.StatusNoContent, "")
		return e.lastToken(to, subjectEmailChange)
	}
	confirm := func(token string) response {
		t.Helper()
		return e.browser().post("/auth/email/verify", map[string]string{"token": token})
	}

	byReset := request("one@example.com")
	expectStatus(t, forgot(e.browser(), "ada@example.com"), http.StatusOK, "")
	expectStatus(t, e.browser().post("/auth/password/reset", map[string]string{"token": e.lastToken("ada@example.com", subjectReset), "password": newPassword}), http.StatusOK, "")
	expectStatus(t, confirm(byReset), http.StatusBadRequest, CodeInvalidToken)

	b := e.browser()
	b.signIn("ada@example.com", newPassword)
	sess, password = b, newPassword
	byChange := request("two@example.com")
	expectStatus(t, b.post("/auth/password/change", map[string]string{"currentPassword": newPassword, "newPassword": strongPassword}), http.StatusNoContent, "")
	expectStatus(t, confirm(byChange), http.StatusBadRequest, CodeInvalidToken)
	password = strongPassword

	first, second := request("three@example.com"), request("four@example.com")
	expectStatus(t, confirm(second), http.StatusOK, "")
	expectStatus(t, confirm(first), http.StatusBadRequest, CodeInvalidToken)

	late := request("five@example.com")
	e.advance(24*time.Hour + time.Second)
	expectStatus(t, confirm(late), http.StatusBadRequest, CodeInvalidToken)
}

// A reset link mailed to the old address stops working once the address moves, whether the move is confirmed from the new mailbox or applied at once with EMAIL_VERIFICATION_DISABLED: the old mailbox may no longer be the user's.
func TestEmailChangeRevokesPasswordResetLinks(t *testing.T) {
	t.Parallel()
	for _, disabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("verification disabled %v", disabled), func(t *testing.T) {
			t.Parallel()
			e := newEnv(t, func(c *config.Config) { c.EmailVerificationDisabled = disabled })
			id := e.seedUser("ada@example.com", strongPassword, userOpts{})
			expectStatus(t, forgot(e.browser(), "ada@example.com"), http.StatusOK, "")
			reset := e.lastToken("ada@example.com", subjectReset)

			expectStatus(t, changeEmail(e.sessionFor(id), "new@example.com", strongPassword), http.StatusNoContent, "")
			if !disabled {
				expectStatus(t, e.browser().post("/auth/email/verify", map[string]string{"token": e.lastToken("new@example.com", subjectEmailChange)}), http.StatusOK, "")
			}
			if email, _ := e.emailOf(id); email != "new@example.com" {
				t.Fatalf("the address is still %s", email)
			}
			expectStatus(t, e.browser().post("/auth/password/reset", map[string]string{"token": reset, "password": newPassword}), http.StatusBadRequest, CodeInvalidPasswordResetToken)
			e.browser().signIn("new@example.com", strongPassword)
		})
	}
}

// A link for an address someone else took in the meantime fails and stays unused.
func TestEmailChangeToATakenAddress(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	id := e.seedUser("ada@example.com", strongPassword, userOpts{})
	expectStatus(t, changeEmail(e.sessionFor(id), "new@example.com", strongPassword), http.StatusNoContent, "")
	token := e.lastToken("new@example.com", subjectEmailChange)
	e.seedUser("New@example.com", strongPassword, userOpts{})
	expectStatus(t, e.browser().post("/auth/email/verify", map[string]string{"token": token}), http.StatusConflict, apperr.CodeConflict)
	if email, _ := e.emailOf(id); email != "ada@example.com" {
		t.Fatal("the address changed")
	}
}

// action:email allows three changes per user per hour, charged only when the address actually changes; confirming shares auth:verify.
func TestEmailChangeRateLimits(t *testing.T) {
	e := newEnv(t)
	e.enableRateLimits()
	e.inOneWindow(func(run int, ip string) error {
		email := fmt.Sprintf("ada%d@example.com", run)
		sess := e.sessionFor(e.seedUser(email, strongPassword, userOpts{}))
		sess.ip = ip
		if err := repeat(5, func() response { return changeEmail(sess, email, "") }, http.StatusNoContent, ""); err != nil {
			return err
		}
		if err := repeat(3, func() response { return changeEmail(sess, "new@example.com", "Wrong-Password-1") }, http.StatusForbidden, ""); err != nil {
			return err
		}
		if err := statusErr(changeEmail(sess, "new@example.com", strongPassword), http.StatusTooManyRequests, ""); err != nil {
			return err
		}
		b := e.browser()
		b.ip = ip
		confirm := func() response { return b.post("/auth/email/verify", map[string]string{"token": "x"}) }
		if err := repeat(10, confirm, http.StatusBadRequest, CodeInvalidToken); err != nil {
			return err
		}
		return statusErr(confirm(), http.StatusTooManyRequests, "")
	})
}
