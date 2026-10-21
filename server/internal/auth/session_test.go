package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/platform/audit"
	"github.com/yldm-tech/forma/server/internal/platform/config"
)

func TestRequireRejectsMissingAndUnknownSessions(t *testing.T) {
	e := newEnv(t)
	b := e.browser()
	r := b.post("/test/protected", nil)
	expectStatus(t, r, http.StatusUnauthorized, apperr.CodeNotAuthenticated)
	if r.json()["message"] != "Not authenticated" {
		t.Fatalf("message %v", r.json()["message"])
	}
	b.cookies[sessionCookieName] = &http.Cookie{Name: sessionCookieName, Value: "forged-token"}
	expectStatus(t, b.post("/test/protected", nil), http.StatusUnauthorized, apperr.CodeNotAuthenticated)
	expectStatus(t, b.get("/auth/me"), http.StatusUnauthorized, apperr.CodeNotAuthenticated)
}

func TestOptionalLetsEveryoneThrough(t *testing.T) {
	e := newEnv(t)
	e.seedUser("ada@example.com", strongPassword, userOpts{})
	b := e.browser()
	if r := b.get("/test/optional"); r.json()["authenticated"] != false {
		t.Fatalf("anonymous: %s", r.Body.String())
	}
	b.signIn("ada@example.com", strongPassword)
	if r := b.get("/test/optional"); r.json()["authenticated"] != true {
		t.Fatalf("signed in: %s", r.Body.String())
	}
}

// A session ends at its expiry and is deleted when seen; with the default lifetime it is not extended before then.
func TestSessionExpires(t *testing.T) {
	e := newEnv(t)
	e.seedUser("ada@example.com", strongPassword, userOpts{})
	b := e.browser()
	b.signIn("ada@example.com", strongPassword)

	e.advance(23 * time.Hour)
	r := b.post("/test/protected", nil)
	expectStatus(t, r, http.StatusOK, "")
	if r.cookie(sessionCookieName) != nil {
		t.Fatal("a default session was extended: forma2's updateAge equals its lifetime")
	}
	e.advance(time.Hour)
	expectStatus(t, b.post("/test/protected", nil), http.StatusUnauthorized, apperr.CodeNotAuthenticated)
	if e.count(`SELECT count(*) FROM sessions`) != 0 {
		t.Fatal("the expired session was not deleted")
	}
}

// With a lifetime longer than a day, a session older than a day is extended to a full lifetime from now and the cookie re-sent (Better Auth's updateAge rule).
func TestSessionSlides(t *testing.T) {
	week := 7 * 24 * time.Hour
	e := newEnv(t, func(c *config.Config) { c.SessionMaxAge = int(week / time.Second) })
	e.seedUser("ada@example.com", strongPassword, userOpts{})
	b := e.browser()
	b.signIn("ada@example.com", strongPassword)

	e.advance(23 * time.Hour)
	if r := b.post("/test/protected", nil); r.cookie(sessionCookieName) != nil {
		t.Fatal("extended before a day had passed")
	}
	e.advance(2 * time.Hour)
	r := b.post("/test/protected", nil)
	expectStatus(t, r, http.StatusOK, "")
	c := r.cookie(sessionCookieName)
	if c == nil || c.MaxAge != int(week/time.Second) {
		t.Fatalf("refresh cookie %+v", c)
	}
	var expires time.Time
	e.db.Raw(`SELECT expires_at FROM sessions`).Scan(&expires)
	if want := e.now().Add(week); expires.Sub(want).Abs() > time.Second {
		t.Fatalf("expires %v, want %v", expires, want)
	}
	e.advance(week - time.Hour)
	expectStatus(t, b.post("/test/protected", nil), http.StatusOK, "")
}

// forma2's getSession re-read User.isActive on every request: deactivating a user ends their sessions at once.
func TestDeactivationEndsSessionsImmediately(t *testing.T) {
	e := newEnv(t)
	id := e.seedUser("ada@example.com", strongPassword, userOpts{})
	b := e.browser()
	b.signIn("ada@example.com", strongPassword)
	e.exec(`UPDATE users SET is_active = false WHERE id = ?`, id)
	expectStatus(t, b.post("/test/protected", nil), http.StatusUnauthorized, apperr.CodeNotAuthenticated)
	e.exec(`UPDATE users SET is_active = true WHERE id = ?`, id)
	expectStatus(t, b.post("/test/protected", nil), http.StatusOK, "")
}

// forma2's auth-signout.integration.test.ts and logSignOutAction: the session row is deleted, the reason recorded, and the organization kept only when the user belongs to it.
func TestSignOut(t *testing.T) {
	e := newEnv(t)
	id := e.seedUser("ada@example.com", strongPassword, userOpts{})
	member := e.insertID(`INSERT INTO organizations (name) VALUES ('Mine') RETURNING id`)
	stranger := e.insertID(`INSERT INTO organizations (name) VALUES ('Theirs') RETURNING id`)
	e.exec(`INSERT INTO memberships (user_id, organization_id, role, accepted) VALUES (?, ?, 'member', true)`, id, member)

	b := e.browser()
	b.signIn("ada@example.com", strongPassword)
	other := e.browser()
	other.signIn("ada@example.com", strongPassword)

	r := b.post("/auth/sign-out", map[string]any{"reason": "session_timeout", "organizationId": member})
	expectStatus(t, r, http.StatusNoContent, "")
	if c := r.cookie(sessionCookieName); c == nil || c.MaxAge >= 0 {
		t.Fatalf("session cookie not cleared: %+v", c)
	}
	expectStatus(t, b.post("/test/protected", nil), http.StatusUnauthorized, "")
	expectStatus(t, other.post("/test/protected", nil), http.StatusOK, "")
	if e.count(`SELECT count(*) FROM sessions WHERE user_id = ?`, id) != 1 {
		t.Fatal("sign-out did not delete exactly its own session")
	}

	expectStatus(t, other.post("/auth/sign-out", map[string]any{"organizationId": stranger}), http.StatusNoContent, "")
	events := e.auditEvents(audit.ActionUserSignedOut)
	if len(events) != 2 {
		t.Fatalf("%d sign-out events", len(events))
	}
	if field(events[0], "changes", "reason") != "session_timeout" || field(events[0], "changes", "organizationId") != member.String() || field(events[0], "actor", "id") != id.String() {
		t.Fatalf("first sign-out %v", events[0])
	}
	if field(events[1], "changes", "reason") != "user_initiated" || field(events[1], "changes", "organizationId") != nil {
		t.Fatalf("second sign-out %v", events[1])
	}

	// Without a session, sign-out still clears the cookie and succeeds.
	anon := e.browser()
	r = anon.post("/auth/sign-out", nil)
	expectStatus(t, r, http.StatusNoContent, "")
	if r.cookie(sessionCookieName) == nil {
		t.Fatal("anonymous sign-out did not clear the cookie")
	}
	expectStatus(t, anon.post("/auth/sign-out", map[string]any{"reason": "because"}), http.StatusBadRequest, apperr.CodeValidation)
}

func TestRevokeAllKeepsOneSession(t *testing.T) {
	e := newEnv(t)
	id := e.seedUser("ada@example.com", strongPassword, userOpts{})
	b1, b2, b3 := e.browser(), e.browser(), e.browser()
	b1.signIn("ada@example.com", strongPassword)
	b2.signIn("ada@example.com", strongPassword)
	b3.signIn("ada@example.com", strongPassword)
	r := b1.post("/test/protected", nil)
	keep := uuid.MustParse(r.json()["sessionId"].(string))

	n, err := e.sessions.RevokeAll(t.Context(), nil, id, keep)
	if err != nil || n != 2 {
		t.Fatalf("RevokeAll = %d, %v; want 2", n, err)
	}
	expectStatus(t, b1.post("/test/protected", nil), http.StatusOK, "")
	expectStatus(t, b2.post("/test/protected", nil), http.StatusUnauthorized, "")
	if n, _ := e.sessions.RevokeAll(t.Context(), nil, id, uuid.Nil); n != 1 {
		t.Fatalf("RevokeAll without a kept session ended %d", n)
	}
}

// Every mutation under /api/app passes the platform CSRF check, sign-in included.
func TestAuthMutationsRequireCSRFHeaders(t *testing.T) {
	e := newEnv(t)
	e.seedUser("ada@example.com", strongPassword, userOpts{})
	for _, path := range []string{"/auth/sign-in", "/auth/signup", "/auth/password/forgot", "/auth/sign-out"} {
		req, _ := http.NewRequest(http.MethodPost, "/api/app"+path, nil)
		req.Header.Set("Origin", "https://evil.example")
		req.Header.Set("X-Requested-With", "XMLHttpRequest")
		rec := response{ResponseRecorder: httptest.NewRecorder(), t: t}
		e.engine.ServeHTTP(rec, req)
		expectStatus(t, rec, http.StatusForbidden, "csrf_rejected")
	}
}

func TestOptions(t *testing.T) {
	e := newEnv(t)
	r := e.browser().get("/auth/options")
	expectStatus(t, r, http.StatusOK, "")
	if m := r.json(); m["signupEnabled"] != true || m["emailAuthEnabled"] != true || m["passwordResetEnabled"] != true || m["emailVerificationRequired"] != true || m["smtpConfigured"] != true || m["turnstileSiteKey"] != nil {
		t.Fatalf("fresh instance options %v", m)
	}
	e.seedUser("ada@example.com", strongPassword, userOpts{})
	if m := e.browser().get("/auth/options").json(); m["signupEnabled"] != false {
		t.Fatalf("after the first user %v", m)
	}

	cloud := newEnv(t, func(c *config.Config) {
		c.IsFormaCloud = true
		c.TurnstileSiteKey, c.TurnstileSecretKey = "site-key", "secret"
		c.PasswordResetDisabled, c.EmailVerificationDisabled = true, true
	})
	cloud.seedUser("ada@example.com", strongPassword, userOpts{})
	if m := cloud.browser().get("/auth/options").json(); m["signupEnabled"] != true || m["turnstileSiteKey"] != "site-key" || m["passwordResetEnabled"] != false || m["emailVerificationRequired"] != false {
		t.Fatalf("cloud options %v", m)
	}
	ssoOnly := newEnv(t, func(c *config.Config) { c.EmailAuthDisabled = true })
	if m := ssoOnly.browser().get("/auth/options").json(); m["emailAuthEnabled"] != false || m["signupEnabled"] != false {
		t.Fatalf("SSO-only options %v", m)
	}
}
