package auth

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/audit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
)

const newPassword = "Brand-New-Pass-42"

func forgot(b *browser, email string) response {
	b.e.t.Helper()
	return b.post("/auth/password/forgot", map[string]string{"email": email})
}

// forma2's forgotPasswordAction: the same answer for every address; a link only for accounts that can reset, and an audit record only when one was sent.
func TestForgotPassword(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	id := e.seedUser("ada@example.com", strongPassword, userOpts{})
	e.seedUser("sso@example.com", "", userOpts{noPassword: true, sso: true})
	b := e.browser()
	for _, email := range []string{"nobody@example.com", "sso@example.com", "ADA@example.com"} {
		r := forgot(b, email)
		expectStatus(t, r, http.StatusOK, "")
		if r.Body.String() != `{"success":true}` {
			t.Fatalf("%s: %s", email, r.Body.String())
		}
	}
	msgs := e.mail.Messages()
	if len(msgs) != 1 || msgs[0].To != "ada@example.com" {
		t.Fatalf("mails %+v", msgs)
	}
	if !strings.Contains(msgs[0].HTML, webApp+resetPasswordPath+"?token=") || !strings.Contains(msgs[0].HTML, "30") {
		t.Fatal("reset mail lacks the link or its lifetime")
	}
	events := e.auditEvents(audit.ActionPasswordReset)
	if len(events) != 1 || field(events[0], "target", "id") != id.String() || field(events[0], "actor", "id") != "unknown" {
		t.Fatalf("events %v", events)
	}

	// A failed send still answers success and is not recorded as a reset.
	e.mail.Err = errors.New("smtp down")
	expectStatus(t, forgot(b, "ada@example.com"), http.StatusOK, "")
	if len(e.auditEvents(audit.ActionPasswordReset)) != 1 {
		t.Fatal("a failed send was audited as a reset")
	}
}

// canResetPassword: an SSO user who still has a password may reset only while email and password sign-in is enabled; a password user always may.
func TestForgotPasswordForSSOUsersWithAPassword(t *testing.T) {
	t.Parallel()
	for _, disabled := range []bool{false, true} {
		e := newEnv(t, func(c *config.Config) { c.EmailAuthDisabled = disabled })
		e.seedUser("moved@example.com", strongPassword, userOpts{sso: true})
		e.seedUser("ada@example.com", strongPassword, userOpts{})
		b := e.browser()
		expectStatus(t, forgot(b, "moved@example.com"), http.StatusOK, "")
		expectStatus(t, forgot(b, "ada@example.com"), http.StatusOK, "")
		if got := len(e.sent("moved@example.com")); got != map[bool]int{false: 1, true: 0}[disabled] {
			t.Fatalf("EMAIL_AUTH_DISABLED=%v: %d mails to the SSO user", disabled, got)
		}
		if len(e.sent("ada@example.com")) != 1 {
			t.Fatalf("EMAIL_AUTH_DISABLED=%v: the password user got no link", disabled)
		}
	}
}

// PASSWORD_RESET_DISABLED refuses both steps. Requesting charges the rate limit first; redeeming checks the switch first.
func TestPasswordResetDisabled(t *testing.T) {
	e := newEnv(t, func(c *config.Config) { c.PasswordResetDisabled = true })
	e.enableRateLimits()
	e.seedUser("ada@example.com", strongPassword, userOpts{})
	e.inOneWindow(func(_ int, ip string) error {
		b := e.browser()
		b.ip = ip
		send := func() response { return forgot(b, "ada@example.com") }
		r := send()
		if err := statusErr(r, http.StatusForbidden, apperr.CodeOperationNotAllowed); err != nil {
			return err
		}
		if r.json()["message"] != "Password reset is disabled" {
			return fmt.Errorf("message %v", r.json()["message"])
		}
		if err := repeat(4, send, http.StatusForbidden, apperr.CodeOperationNotAllowed); err != nil {
			return err
		}
		if err := statusErr(send(), http.StatusTooManyRequests, ""); err != nil {
			return err
		}
		return repeat(7, func() response {
			return b.post("/auth/password/reset", map[string]string{"token": "t", "password": newPassword})
		}, http.StatusForbidden, apperr.CodeOperationNotAllowed)
	})
	if len(e.mail.Messages()) != 0 {
		t.Fatal("a disabled reset sent mail")
	}
}

// forma2's auth-email-verification.integration.test.ts (reset): the password is re-hashed, every session ends, the old password stops working, and the link is single-use.
func TestResetPassword(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	id := e.seedUser("ada@example.com", strongPassword, userOpts{})
	signedIn := e.browser()
	signedIn.signIn("ada@example.com", strongPassword)
	expectStatus(t, forgot(e.browser(), "ada@example.com"), http.StatusOK, "")
	token := e.lastToken("ada@example.com", subjectReset)

	b := e.browser()
	r := b.post("/auth/password/reset", map[string]string{"token": token, "password": newPassword})
	expectStatus(t, r, http.StatusOK, "")
	if e.count(`SELECT count(*) FROM sessions WHERE user_id = ?`, id) != 0 {
		t.Fatal("sessions survived the reset")
	}
	expectStatus(t, signedIn.post("/test/protected", nil), http.StatusUnauthorized, "")
	expectStatus(t, b.post("/auth/sign-in", credentials("ada@example.com", strongPassword)), http.StatusUnauthorized, CodeInvalidEmailOrPassword)
	b.signIn("ada@example.com", newPassword)

	r = e.browser().post("/auth/password/reset", map[string]string{"token": token, "password": "Another-Pass-99"})
	expectStatus(t, r, http.StatusBadRequest, CodeInvalidPasswordResetToken)
	if field(r.json(), "details", "reason") != "invalid_or_expired" {
		t.Fatalf("details %v", r.json())
	}
	if len(e.sent("ada@example.com")) != 2 || e.sent("ada@example.com")[1].Subject != subjectChanged {
		t.Fatal("no password-changed notice")
	}
	var marker bool
	for _, ev := range e.auditEvents(audit.ActionUpdated) {
		if field(ev, "changes", "passwordResetMarker") == true && field(ev, "target", "id") == id.String() {
			marker = true
		}
	}
	if !marker {
		t.Fatal("reset not audited")
	}
}

// forma2's better-auth-hibp.integration.test.ts: a breached password is refused without spending the link, so a retry with a clean password succeeds.
func TestResetPasswordBreachKeepsTheToken(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.seedUser("ada@example.com", strongPassword, userOpts{})
	expectStatus(t, forgot(e.browser(), "ada@example.com"), http.StatusOK, "")
	token := e.lastToken("ada@example.com", subjectReset)
	e.breach("Breached-Pass-1")
	b := e.browser()
	expectStatus(t, b.post("/auth/password/reset", map[string]string{"token": token, "password": "Breached-Pass-1"}), http.StatusBadRequest, CodePasswordCompromised)
	expectStatus(t, b.post("/auth/password/reset", map[string]string{"token": token, "password": newPassword}), http.StatusOK, "")
}

func TestResetPasswordTokenExpiresAndValidates(t *testing.T) {
	t.Parallel()
	e := newEnv(t, func(c *config.Config) { c.PasswordResetTokenLifetimeMinutes = 5 })
	e.seedUser("ada@example.com", strongPassword, userOpts{})
	expectStatus(t, forgot(e.browser(), "ada@example.com"), http.StatusOK, "")
	if !strings.Contains(e.mail.Messages()[0].HTML, "5 minutes") {
		t.Fatal("the mail does not state the configured lifetime")
	}
	token := e.lastToken("ada@example.com", subjectReset)
	b := e.browser()
	expectStatus(t, b.post("/auth/password/reset", map[string]string{"token": token, "password": "weak"}), http.StatusBadRequest, apperr.CodeValidation)
	expectStatus(t, b.post("/auth/password/reset", map[string]string{"token": "", "password": newPassword}), http.StatusBadRequest, apperr.CodeValidation)
	e.advance(5*time.Minute + time.Second)
	expectStatus(t, b.post("/auth/password/reset", map[string]string{"token": token, "password": newPassword}), http.StatusBadRequest, CodeInvalidPasswordResetToken)
}

// auth:forgot and auth:reset-password are separate budgets of five per hour per address.
func TestPasswordResetRateLimits(t *testing.T) {
	e := newEnv(t)
	e.enableRateLimits()
	e.inOneWindow(func(_ int, ip string) error {
		b := e.browser()
		b.ip = ip
		request := func() response { return forgot(b, "nobody@example.com") }
		if err := repeat(5, request, http.StatusOK, ""); err != nil {
			return err
		}
		if err := statusErr(request(), http.StatusTooManyRequests, ""); err != nil {
			return err
		}
		redeem := func() response {
			return b.post("/auth/password/reset", map[string]string{"token": "x", "password": newPassword})
		}
		if err := repeat(5, redeem, http.StatusBadRequest, CodeInvalidPasswordResetToken); err != nil {
			return err
		}
		return statusErr(redeem(), http.StatusTooManyRequests, "")
	})
}

// Changing the password checks the current one, applies the password policy and breach check, keeps this session and ends the others.
func TestChangePassword(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	id := e.seedUser("ada@example.com", strongPassword, userOpts{})
	b, other := e.browser(), e.browser()
	b.signIn("ada@example.com", strongPassword)
	other.signIn("ada@example.com", strongPassword)

	change := func(current, next string) response {
		return b.post("/auth/password/change", map[string]string{"currentPassword": current, "newPassword": next})
	}
	expectStatus(t, e.browser().post("/auth/password/change", map[string]string{"currentPassword": strongPassword, "newPassword": newPassword}), http.StatusUnauthorized, apperr.CodeNotAuthenticated)
	expectStatus(t, change("Wrong-Password-1", newPassword), http.StatusBadRequest, CodeInvalidPassword)
	expectStatus(t, change(strongPassword, "nouppercase1"), http.StatusBadRequest, apperr.CodeValidation)
	e.breach("Breached-Pass-1")
	expectStatus(t, change(strongPassword, "Breached-Pass-1"), http.StatusBadRequest, CodePasswordCompromised)

	expectStatus(t, change(strongPassword, newPassword), http.StatusNoContent, "")
	expectStatus(t, b.post("/test/protected", nil), http.StatusOK, "")
	expectStatus(t, other.post("/test/protected", nil), http.StatusUnauthorized, "")
	if e.count(`SELECT count(*) FROM sessions WHERE user_id = ?`, id) != 1 {
		t.Fatal("other sessions survived")
	}
	e.browser().signIn("ada@example.com", newPassword)
	if msgs := e.sent("ada@example.com"); len(msgs) != 1 || msgs[0].Subject != subjectChanged {
		t.Fatalf("mails %+v", msgs)
	}

	sso := e.seedUser("sso@example.com", "", userOpts{noPassword: true, sso: true})
	ssoBrowser := e.browser()
	sess, err := e.sessions.create(t.Context(), e.db, sso, clientMeta{})
	if err != nil {
		t.Fatal(err)
	}
	ssoBrowser.cookies[sessionCookieName] = &http.Cookie{Name: sessionCookieName, Value: sess.Value}
	expectStatus(t, ssoBrowser.post("/auth/password/change", map[string]string{"currentPassword": "x", "newPassword": newPassword}), http.StatusBadRequest, CodePasswordNotSet)
}

// A password change shares the sign-in limit on guessing the current password.
func TestChangePasswordRateLimit(t *testing.T) {
	e := newEnv(t)
	e.seedUser("ada@example.com", strongPassword, userOpts{})
	signedIn := e.browser()
	signedIn.signIn("ada@example.com", strongPassword)
	e.enableRateLimits()
	e.inOneWindow(func(_ int, ip string) error {
		b := e.browser()
		b.ip = ip
		b.cookies = signedIn.cookies
		guess := func() response {
			return b.post("/auth/password/change", map[string]string{"currentPassword": "Guess-Number-1", "newPassword": newPassword})
		}
		if err := repeat(5, guess, http.StatusBadRequest, CodeInvalidPassword); err != nil {
			return err
		}
		return statusErr(b.post("/auth/password/change", map[string]string{"currentPassword": strongPassword, "newPassword": newPassword}), http.StatusTooManyRequests, "")
	})
}

// forma2's in-app reset on the profile page: password users get a link; SSO users and disabled instances are refused.
func TestRequestResetLink(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.seedUser("ada@example.com", strongPassword, userOpts{})
	b := e.browser()
	b.signIn("ada@example.com", strongPassword)
	expectStatus(t, b.post("/auth/password/reset-link", nil), http.StatusOK, "")
	if msgs := e.sent("ada@example.com"); len(msgs) != 1 || msgs[0].Subject != subjectReset {
		t.Fatalf("mails %+v", msgs)
	}
	if len(e.auditEvents(audit.ActionPasswordReset)) != 1 {
		t.Fatal("not audited")
	}
	e.mail.Err = errors.New("smtp down")
	expectStatus(t, b.post("/auth/password/reset-link", nil), http.StatusBadRequest, CodeEmailSendFailed)

	sso := e.seedUser("moved@example.com", strongPassword, userOpts{sso: true})
	ssoBrowser := e.browser()
	sess, _ := e.sessions.create(t.Context(), e.db, sso, clientMeta{})
	ssoBrowser.cookies[sessionCookieName] = &http.Cookie{Name: sessionCookieName, Value: sess.Value}
	r := ssoBrowser.post("/auth/password/reset-link", nil)
	expectStatus(t, r, http.StatusForbidden, apperr.CodeOperationNotAllowed)

	off := newEnv(t, func(c *config.Config) { c.PasswordResetDisabled = true })
	off.seedUser("ada@example.com", strongPassword, userOpts{})
	ob := off.browser()
	ob.signIn("ada@example.com", strongPassword)
	expectStatus(t, ob.post("/auth/password/reset-link", nil), http.StatusForbidden, apperr.CodeOperationNotAllowed)
}
