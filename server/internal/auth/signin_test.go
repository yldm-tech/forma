package auth

import (
	"crypto/sha256"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/platform/audit"
	"github.com/yldm-tech/forma/server/internal/platform/config"
)

func credentials(email, password string) map[string]string {
	return map[string]string{"email": email, "password": password}
}

// forma2's auth-email-password.integration.test.ts: the right password issues a database session, the wrong one does not.
func TestSignInIssuesASession(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	id := e.seedUser("ada@example.com", strongPassword, userOpts{})
	b := e.browser()

	r := b.post("/auth/sign-in", credentials("ADA@example.com", strongPassword))
	expectStatus(t, r, http.StatusOK, "")
	if r.json()["status"] != "signed_in" {
		t.Fatalf("body %s", r.Body.String())
	}
	c := r.cookie(sessionCookieName)
	if c == nil || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Path != "/" || c.MaxAge != 86400 || c.Secure {
		t.Fatalf("session cookie %+v", c)
	}
	sum := sha256.Sum256([]byte(c.Value))
	if e.count(`SELECT count(*) FROM sessions WHERE user_id = ? AND token_hash = ?`, id, sum[:]) != 1 {
		t.Fatal("the session row does not hold the SHA-256 of the cookie token")
	}
	if e.count(`SELECT count(*) FROM sessions WHERE token_hash = ?`, []byte(c.Value)) != 0 {
		t.Fatal("the raw token is stored")
	}
	var meta struct {
		IPAddress string
		UserAgent string
	}
	e.db.Raw(`SELECT host(ip_address) AS ip_address, user_agent FROM sessions WHERE user_id = ?`, id).Scan(&meta)
	if meta.IPAddress != "203.0.113.10" || meta.UserAgent != "forma-test" {
		t.Fatalf("session metadata %+v", meta)
	}

	me := b.get("/auth/me")
	expectStatus(t, me, http.StatusOK, "")
	if m := me.json(); m["id"] != id.String() || m["email"] != "ada@example.com" || m["identityProvider"] != "email" || m["twoFactorEnabled"] != false || m["lastLoginAt"] == nil {
		t.Fatalf("me %v", m)
	}
	events := e.auditEvents(audit.ActionSignedIn)
	if len(events) != 1 || field(events[0], "actor", "id") != id.String() || field(events[0], "changes", "authMethod") != "password" || field(events[0], "changes", "sessionStrategy") != "database" {
		t.Fatalf("signedIn events %v", events)
	}
}

// Better Auth's INVALID_EMAIL_OR_PASSWORD: a wrong password, an unknown address and an account without a password get the same answer.
func TestSignInRejectsBadCredentialsAlike(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.seedUser("ada@example.com", strongPassword, userOpts{})
	e.seedUser("sso@example.com", "", userOpts{noPassword: true, sso: true})
	b := e.browser()
	var bodies []string
	for _, creds := range []map[string]string{
		credentials("ada@example.com", "Wrong-Password-1"),
		credentials("nobody@example.com", strongPassword),
		credentials("sso@example.com", strongPassword),
	} {
		r := b.post("/auth/sign-in", creds)
		expectStatus(t, r, http.StatusUnauthorized, CodeInvalidEmailOrPassword)
		bodies = append(bodies, r.Body.String())
	}
	if bodies[0] != bodies[1] || bodies[1] != bodies[2] {
		t.Fatalf("answers differ: %v", bodies)
	}
	if e.count(`SELECT count(*) FROM sessions`) != 0 || b.has(sessionCookieName) {
		t.Fatal("a rejected sign-in created a session")
	}
}

// Better Auth's requireEmailVerification with sendOnSignIn: an unverified user with the right password gets a fresh link and 403; with the wrong password, nothing.
func TestSignInRequiresVerification(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.seedUser("ada@example.com", strongPassword, userOpts{unverified: true})
	b := e.browser()
	expectStatus(t, b.post("/auth/sign-in", credentials("ada@example.com", "Wrong-Password-1")), http.StatusUnauthorized, CodeInvalidEmailOrPassword)
	if len(e.mail.Messages()) != 0 {
		t.Fatal("a wrong password triggered a verification mail")
	}
	expectStatus(t, b.post("/auth/sign-in", credentials("ada@example.com", strongPassword)), http.StatusForbidden, CodeEmailNotVerified)
	if len(e.sent("ada@example.com")) != 1 || e.sent("ada@example.com")[0].Subject != subjectVerification {
		t.Fatalf("no fresh verification link: %+v", e.mail.Messages())
	}
	if e.count(`SELECT count(*) FROM sessions`) != 0 {
		t.Fatal("an unverified user got a session")
	}

	open := newEnv(t, func(c *config.Config) { c.EmailVerificationDisabled = true })
	open.seedUser("ada@example.com", strongPassword, userOpts{unverified: true})
	open.browser().signIn("ada@example.com", strongPassword)
}

// forma2's better-auth-active-user-gate: a deactivated user with the right password gets the inactive message and no session row.
func TestSignInRejectsDeactivatedUsers(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.seedUser("ada@example.com", strongPassword, userOpts{inactive: true})
	r := e.browser().post("/auth/sign-in", credentials("ada@example.com", strongPassword))
	expectStatus(t, r, http.StatusForbidden, CodeAccountInactive)
	if r.json()["message"] != "Your account is currently inactive. Please contact the organization admin." {
		t.Fatalf("message %v", r.json()["message"])
	}
	if e.count(`SELECT count(*) FROM sessions`) != 0 {
		t.Fatal("a deactivated user got a session")
	}
}

func TestSignInWithEmailAuthDisabled(t *testing.T) {
	t.Parallel()
	e := newEnv(t, func(c *config.Config) { c.EmailAuthDisabled = true })
	e.seedUser("ada@example.com", strongPassword, userOpts{})
	expectStatus(t, e.browser().post("/auth/sign-in", credentials("ada@example.com", strongPassword)), http.StatusBadRequest, CodeEmailPasswordDisabled)
	if e.count(`SELECT count(*) FROM sessions`) != 0 {
		t.Fatal("EMAIL_AUTH_DISABLED let a password sign in")
	}
}

func TestSignInValidation(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	r := e.browser().post("/auth/sign-in", map[string]string{"email": "nope", "password": ""})
	expectStatus(t, r, http.StatusBadRequest, apperr.CodeValidation)
}

// HTTPS deployments get the __Secure- cookie names and the Secure attribute.
func TestSecureCookies(t *testing.T) {
	t.Parallel()
	e := newEnv(t, func(c *config.Config) { c.WebAppURL.Scheme = "https" })
	e.seedUser("ada@example.com", strongPassword, userOpts{})
	r := e.browser().post("/auth/sign-in", credentials("ada@example.com", strongPassword))
	expectStatus(t, r, http.StatusOK, "")
	c := r.cookie("__Secure-" + sessionCookieName)
	if c == nil || !c.Secure || !c.HttpOnly {
		t.Fatalf("cookies %v", r.Result().Cookies())
	}
}

// forma2 audited a rejected sign-in against a hashed identity, never the address, and throttled the records under brute force.
func TestSignInFailuresAreAuditedWithoutTheAddress(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.seedUser("ada@example.com", strongPassword, userOpts{inactive: true})
	b := e.browser()
	expectStatus(t, b.post("/auth/sign-in", credentials("Ada@example.com", "Wrong-Password-1")), http.StatusUnauthorized, "")
	expectStatus(t, b.post("/auth/sign-in", credentials("ada@example.com", strongPassword)), http.StatusForbidden, "")

	events := e.auditEvents(audit.ActionAuthenticationAttempted)
	if len(events) != 2 {
		t.Fatalf("%d failure events, want 2", len(events))
	}
	want := auditIdentifier("ada@example.com", "email")
	reasons := []string{CodeInvalidEmailOrPassword, CodeAccountInactive}
	for i, ev := range events {
		if field(ev, "actor", "id") != want || field(ev, "target", "id") != want || field(ev, "status") != "failure" ||
			field(ev, "changes", "failureReason") != reasons[i] || field(ev, "changes", "provider") != "credentials" || field(ev, "organizationId") != "unknown" {
			t.Fatalf("event %d: %v", i, ev)
		}
	}
	if strings.Contains(strings.ToLower(e.logs.String()), "ada@example.com") {
		t.Fatal("the address reached the logs")
	}
}

// The first four failures in a five-minute bucket are recorded, then every tenth, or one after a quiet minute; without Valkey none is.
func TestFailureThrottle(t *testing.T) {
	e := newEnv(t)
	f := e.svc.failures
	start := time.Date(2026, 10, 21, 12, 0, 1, 0, time.UTC)
	e.clock = start
	var recorded []int
	for i := 0; i < 12; i++ {
		if f.shouldRecord(t.Context(), "ada@example.com") {
			recorded = append(recorded, i)
		}
		e.advance(time.Second)
	}
	// Attempts are numbered from 0 here, the count of earlier failures forma2 compared.
	if got := recorded; len(got) != 5 || got[0] != 0 || got[3] != 3 || got[4] != 10 {
		t.Fatalf("recorded attempts %v, want [0 1 2 3 10]", got)
	}
	e.advance(61 * time.Second)
	if !f.shouldRecord(t.Context(), "ada@example.com") {
		t.Fatal("a failure after a quiet minute was not recorded")
	}
	if !f.shouldRecord(t.Context(), "other@example.com") {
		t.Fatal("another identity shares the throttle")
	}
	e.advance(5 * time.Minute)
	if !f.shouldRecord(t.Context(), "ada@example.com") {
		t.Fatal("a new bucket did not start fresh")
	}

	_ = e.rdb.Close()
	if f.shouldRecord(t.Context(), "fresh@example.com") {
		t.Fatal("recorded without Valkey; the throttle must fail closed")
	}
}

// Better Auth's /sign-in/email limit: five attempts per client address per minute.
func TestSignInRateLimit(t *testing.T) {
	e := newEnv(t)
	e.enableRateLimits()
	e.inOneWindow(time.Minute, func(_ int, ip string) error {
		b := e.browser()
		b.ip = ip
		send := func() response { return b.post("/auth/sign-in", credentials("ada@example.com", "Wrong-Password-1")) }
		if err := repeat(5, send, http.StatusUnauthorized, CodeInvalidEmailOrPassword); err != nil {
			return err
		}
		return statusErr(send(), http.StatusTooManyRequests, apperr.CodeTooManyRequests)
	})
}

// forma2's auth-two-factor.integration.test.ts: with a second factor the password yields a challenge, not a session; completing it issues the session.
func TestSignInWithTwoFactor(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	id := e.seedUser("ada@example.com", strongPassword, userOpts{twoFactor: true})
	b := e.browser()

	r := b.post("/auth/sign-in", credentials("ada@example.com", strongPassword))
	expectStatus(t, r, http.StatusOK, "")
	if r.json()["status"] != "two_factor_required" || r.cookie(sessionCookieName) != nil {
		t.Fatalf("sign-in with 2FA: %s, cookies %v", r.Body.String(), r.Result().Cookies())
	}
	pending := r.cookie(twoFactorCookieName)
	if pending == nil || pending.MaxAge != 600 || !pending.HttpOnly {
		t.Fatalf("two-factor cookie %+v", pending)
	}
	if e.count(`SELECT count(*) FROM sessions`) != 0 || len(e.auditEvents(audit.ActionSignedIn)) != 0 {
		t.Fatal("the password step alone signed the user in")
	}
	expectStatus(t, b.post("/test/protected", nil), http.StatusUnauthorized, apperr.CodeNotAuthenticated)

	p := b.get("/test/two-factor/pending")
	expectStatus(t, p, http.StatusOK, "")
	if p.json()["userId"] != id.String() {
		t.Fatalf("pending user %v", p.json())
	}
	stolen := pending.Value
	c := b.post("/test/two-factor/complete", nil)
	expectStatus(t, c, http.StatusNoContent, "")
	if !b.has(sessionCookieName) || b.has(twoFactorCookieName) {
		t.Fatalf("after completion cookies %v", b.cookies)
	}
	expectStatus(t, b.post("/test/protected", nil), http.StatusOK, "")
	if len(e.auditEvents(audit.ActionSignedIn)) != 1 {
		t.Fatal("the completed sign-in was not audited")
	}

	// The pending sign-in is single-use.
	replay := e.browser()
	replay.cookies[twoFactorCookieName] = &http.Cookie{Name: twoFactorCookieName, Value: stolen}
	expectStatus(t, replay.post("/test/two-factor/complete", nil), http.StatusUnauthorized, CodeInvalidTwoFactorCookie)
	expectStatus(t, replay.get("/test/two-factor/pending"), http.StatusUnauthorized, CodeInvalidTwoFactorCookie)
	expectStatus(t, e.browser().get("/test/two-factor/pending"), http.StatusUnauthorized, CodeInvalidTwoFactorCookie)
}

func TestTwoFactorPendingExpiresAndRespectsDeactivation(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	id := e.seedUser("ada@example.com", strongPassword, userOpts{twoFactor: true})
	b := e.browser()
	expectStatus(t, b.post("/auth/sign-in", credentials("ada@example.com", strongPassword)), http.StatusOK, "")
	e.advance(10*time.Minute + time.Second)
	expectStatus(t, b.get("/test/two-factor/pending"), http.StatusUnauthorized, CodeInvalidTwoFactorCookie)
	expectStatus(t, b.post("/test/two-factor/complete", nil), http.StatusUnauthorized, CodeInvalidTwoFactorCookie)

	expectStatus(t, b.post("/auth/sign-in", credentials("ada@example.com", strongPassword)), http.StatusOK, "")
	e.exec(`UPDATE users SET is_active = false WHERE id = ?`, id)
	expectStatus(t, b.post("/test/two-factor/complete", nil), http.StatusForbidden, CodeAccountInactive)
	if e.count(`SELECT count(*) FROM sessions`) != 0 {
		t.Fatal("a deactivated user completed a sign-in")
	}
}

// A password reset ends sign-ins that are waiting for their second factor: they were authorized by the old password.
func TestPasswordResetCancelsPendingSecondFactor(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.seedUser("ada@example.com", strongPassword, userOpts{twoFactor: true})
	b := e.browser()
	expectStatus(t, b.post("/auth/sign-in", credentials("ada@example.com", strongPassword)), http.StatusOK, "")
	expectStatus(t, e.browser().post("/auth/password/forgot", map[string]string{"email": "ada@example.com"}), http.StatusOK, "")
	token := e.lastToken("ada@example.com", subjectReset)
	expectStatus(t, e.browser().post("/auth/password/reset", map[string]string{"token": token, "password": "New-Password-77"}), http.StatusOK, "")
	expectStatus(t, b.post("/test/two-factor/complete", nil), http.StatusUnauthorized, CodeInvalidTwoFactorCookie)
}
