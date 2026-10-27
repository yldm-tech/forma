package auth

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/audit"
)

func (b *browser) delete(path string) response {
	b.e.t.Helper()
	return b.do(http.MethodDelete, path, nil)
}

// sessionsOf lists a browser's sessions and fails the test unless it could.
func (b *browser) sessionsOf() []map[string]any {
	b.e.t.Helper()
	r := b.get("/auth/sessions")
	expectStatus(b.e.t, r, http.StatusOK, "")
	if r.Header().Get("Cache-Control") != "no-store" {
		b.e.t.Fatalf("Cache-Control %q", r.Header().Get("Cache-Control"))
	}
	list, ok := r.json()["sessions"].([]any)
	if !ok {
		b.e.t.Fatalf("sessions: %s", r.Body.String())
	}
	out := make([]map[string]any, 0, len(list))
	for _, s := range list {
		out = append(out, s.(map[string]any))
	}
	return out
}

// sessionID is the id of the browser's own session.
func (b *browser) sessionID() string {
	b.e.t.Helper()
	r := b.post("/test/protected", nil)
	expectStatus(b.e.t, r, http.StatusOK, "")
	return r.json()["sessionId"].(string)
}

func TestSessionsRequireASession(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	b := e.browser()
	expectStatus(t, b.get("/auth/sessions"), http.StatusUnauthorized, apperr.CodeNotAuthenticated)
	expectStatus(t, b.post("/auth/sessions/revoke-others", nil), http.StatusUnauthorized, apperr.CodeNotAuthenticated)
	expectStatus(t, b.delete("/auth/sessions/0190f0a0-0000-7000-8000-000000000000"), http.StatusUnauthorized, apperr.CodeNotAuthenticated)
}

// The list holds the user's unexpired sessions, the caller's own first, with where each was opened; another user's sessions never appear.
func TestSessionsList(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ada := e.seedUser("ada@example.com", strongPassword, userOpts{})
	e.seedUser("bob@example.com", strongPassword, userOpts{})
	laptop := e.browser()
	laptop.signIn("ada@example.com", strongPassword)
	e.advance(time.Minute)
	phone := e.browser()
	phone.ip = "198.51.100.7"
	phone.signIn("ada@example.com", strongPassword)
	e.advance(time.Minute)
	stale := e.browser()
	stale.signIn("ada@example.com", strongPassword)
	e.exec(`UPDATE sessions SET expires_at = ? WHERE id = ?`, e.now().Add(-time.Second), stale.sessionID())
	e.browser().signIn("bob@example.com", strongPassword)

	list := laptop.sessionsOf()
	if len(list) != 2 {
		t.Fatalf("want the two live sessions of ada, got %v", list)
	}
	if list[0]["id"] != laptop.sessionID() || list[0]["current"] != true || list[0]["ipAddress"] != "203.0.113.10" || list[0]["userAgent"] != "forma-test" {
		t.Fatalf("current session %v", list[0])
	}
	if list[1]["id"] != phone.sessionID() || list[1]["current"] != false || list[1]["ipAddress"] != "198.51.100.7" {
		t.Fatalf("other session %v", list[1])
	}
	for _, key := range []string{"createdAt", "lastActiveAt", "expiresAt"} {
		if _, err := time.Parse(time.RFC3339Nano, list[1][key].(string)); err != nil {
			t.Fatalf("%s: %v", key, err)
		}
	}
	// The phone sees itself first.
	if mine := phone.sessionsOf(); mine[0]["id"] != phone.sessionID() || mine[0]["current"] != true {
		t.Fatalf("phone's list %v", mine)
	}

	// An IPv6 client is stored as its /64 (httpapi.CanonicalIP) and listed as that network.
	desk := e.browser()
	desk.ip = "2001:db8:1:2:3:4:5:6"
	desk.signIn("ada@example.com", strongPassword)
	if mine := desk.sessionsOf(); mine[0]["ipAddress"] != "2001:db8:1:2::/64" {
		t.Fatalf("IPv6 session %v", mine[0])
	}
	e.exec(`DELETE FROM sessions WHERE id = ?`, desk.sessionID())

	// A session with no recorded address or browser lists them as null.
	e.exec(`UPDATE sessions SET ip_address = NULL, user_agent = NULL WHERE user_id = ? AND id <> ?`, ada, laptop.sessionID())
	other := laptop.sessionsOf()[1]
	if v, ok := other["ipAddress"]; !ok || v != nil {
		t.Fatalf("ipAddress %v", other)
	}
	if v, ok := other["userAgent"]; !ok || v != nil {
		t.Fatalf("userAgent %v", other)
	}
}

// A session in use records its last activity at most once per sessionActivityInterval, so the list can say when each was last used without a write per request.
func TestSessionLastActivity(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.seedUser("ada@example.com", strongPassword, userOpts{})
	b := e.browser()
	b.signIn("ada@example.com", strongPassword)
	id := b.sessionID()
	updatedAt := func() time.Time {
		var at time.Time
		if err := e.db.Raw(`SELECT updated_at FROM sessions WHERE id = ?`, id).Row().Scan(&at); err != nil {
			t.Fatal(err)
		}
		return at
	}
	signedIn := updatedAt()

	e.advance(sessionActivityInterval - time.Second)
	expectStatus(t, b.post("/test/protected", nil), http.StatusOK, "")
	if !updatedAt().Equal(signedIn) {
		t.Fatal("activity was recorded within the interval")
	}
	e.advance(time.Second)
	expectStatus(t, b.post("/test/protected", nil), http.StatusOK, "")
	// PostgreSQL keeps microseconds, so the stored times are compared to the clock within one.
	if got := updatedAt(); got.Sub(e.now()).Abs() > time.Microsecond {
		t.Fatalf("last activity %v, want %v", got, e.now())
	}
	list := b.sessionsOf()
	lastActive, _ := time.Parse(time.RFC3339Nano, list[0]["lastActiveAt"].(string))
	createdAt, _ := time.Parse(time.RFC3339Nano, list[0]["createdAt"].(string))
	if (lastActive.Sub(createdAt) - sessionActivityInterval).Abs() > time.Microsecond {
		t.Fatalf("lastActiveAt %v, createdAt %v", lastActive, createdAt)
	}
}

// One session can be signed out from another; a session of another user, one already ended or a malformed id is not found. Signing out the current one clears its cookie.
func TestRevokeSession(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ada := e.seedUser("ada@example.com", strongPassword, userOpts{})
	e.seedUser("bob@example.com", strongPassword, userOpts{})
	laptop, phone, bob := e.browser(), e.browser(), e.browser()
	laptop.signIn("ada@example.com", strongPassword)
	phone.signIn("ada@example.com", strongPassword)
	bob.signIn("bob@example.com", strongPassword)
	phoneID, bobID := phone.sessionID(), bob.sessionID()

	expectStatus(t, laptop.delete("/auth/sessions/"+bobID), http.StatusNotFound, apperr.CodeNotFound)
	expectStatus(t, bob.post("/test/protected", nil), http.StatusOK, "")
	expectStatus(t, laptop.delete("/auth/sessions/not-a-session"), http.StatusNotFound, apperr.CodeNotFound)

	r := laptop.delete("/auth/sessions/" + phoneID)
	expectStatus(t, r, http.StatusNoContent, "")
	if r.cookie(sessionCookieName) != nil {
		t.Fatal("ending another session touched this browser's cookie")
	}
	expectStatus(t, phone.post("/test/protected", nil), http.StatusUnauthorized, apperr.CodeNotAuthenticated)
	expectStatus(t, laptop.post("/test/protected", nil), http.StatusOK, "")
	expectStatus(t, laptop.delete("/auth/sessions/"+phoneID), http.StatusNotFound, apperr.CodeNotFound)

	laptopID := laptop.sessionID()
	r = laptop.delete("/auth/sessions/" + laptopID)
	expectStatus(t, r, http.StatusNoContent, "")
	if c := r.cookie(sessionCookieName); c == nil || c.MaxAge >= 0 {
		t.Fatalf("ending the current session left its cookie: %v", c)
	}
	expectStatus(t, laptop.get("/auth/me"), http.StatusUnauthorized, apperr.CodeNotAuthenticated)
	if e.count(`SELECT count(*) FROM sessions WHERE user_id = ?`, ada) != 0 {
		t.Fatal("a session of ada survived")
	}

	var ended []any
	for _, ev := range e.auditEvents(audit.ActionUserSignedOut) {
		if field(ev, "target", "id") == ada.String() && field(ev, "changes", "authMethod") == "revoke_session" {
			ended = append(ended, field(ev, "changes", "sessionId"), field(ev, "changes", "current"))
		}
	}
	if len(ended) != 4 || ended[0] != phoneID || ended[1] != false || ended[2] != laptopID || ended[3] != true {
		t.Fatalf("audit %v", ended)
	}
}

// Signing out everywhere else keeps the current session and leaves other users alone.
func TestRevokeOtherSessions(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ada := e.seedUser("ada@example.com", strongPassword, userOpts{})
	e.seedUser("bob@example.com", strongPassword, userOpts{})
	laptop, phone, tablet, bob := e.browser(), e.browser(), e.browser(), e.browser()
	for _, b := range []*browser{laptop, phone, tablet} {
		b.signIn("ada@example.com", strongPassword)
	}
	bob.signIn("bob@example.com", strongPassword)

	r := laptop.post("/auth/sessions/revoke-others", nil)
	expectStatus(t, r, http.StatusOK, "")
	if r.json()["revoked"] != float64(2) {
		t.Fatalf("revoke-others: %s", r.Body.String())
	}
	expectStatus(t, laptop.post("/test/protected", nil), http.StatusOK, "")
	expectStatus(t, phone.post("/test/protected", nil), http.StatusUnauthorized, apperr.CodeNotAuthenticated)
	expectStatus(t, tablet.post("/test/protected", nil), http.StatusUnauthorized, apperr.CodeNotAuthenticated)
	expectStatus(t, bob.post("/test/protected", nil), http.StatusOK, "")
	if list := laptop.sessionsOf(); len(list) != 1 || list[0]["current"] != true {
		t.Fatalf("after revoke-others %v", list)
	}

	// With nothing left to end it still succeeds.
	r = laptop.post("/auth/sessions/revoke-others", nil)
	expectStatus(t, r, http.StatusOK, "")
	if r.json()["revoked"] != float64(0) {
		t.Fatalf("second revoke-others: %s", r.Body.String())
	}

	var revoked []any
	for _, ev := range e.auditEvents(audit.ActionUserSignedOut) {
		if field(ev, "target", "id") == ada.String() && field(ev, "changes", "authMethod") == "revoke_other_sessions" {
			revoked = append(revoked, field(ev, "changes", "revoked"))
		}
	}
	if len(revoked) != 2 || revoked[0] != float64(2) || revoked[1] != float64(0) {
		t.Fatalf("audit %v", revoked)
	}
}

// Every change that signs the user's other sessions out revokes their OAuth grants in the same transaction: an MCP client's refresh token is a session that rotates for thirty days, so one an attacker holds must not survive the owner's recovery. Another user's grants are not touched, and a refused change revokes nothing.
func TestEndingOtherAccessRevokesOAuthGrants(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		twoFactor bool
		// refused runs a request that must change nothing; end runs the change.
		refused func(e *env, id uuid.UUID) response
		end     func(e *env, id uuid.UUID) response
	}{
		{
			name: "password reset",
			refused: func(e *env, _ uuid.UUID) response {
				return e.browser().post("/auth/password/reset", map[string]string{"token": "not-a-token", "password": newPassword})
			},
			end: func(e *env, _ uuid.UUID) response {
				expectStatus(e.t, forgot(e.browser(), "ada@example.com"), http.StatusOK, "")
				return e.browser().post("/auth/password/reset", map[string]string{"token": e.lastToken("ada@example.com", subjectReset), "password": newPassword})
			},
		},
		{
			name: "password change",
			refused: func(e *env, id uuid.UUID) response {
				return e.sessionFor(id).post("/auth/password/change", map[string]string{"currentPassword": "Wrong-Password-1", "newPassword": newPassword})
			},
			end: func(e *env, id uuid.UUID) response {
				return e.sessionFor(id).post("/auth/password/change", map[string]string{"currentPassword": strongPassword, "newPassword": newPassword})
			},
		},
		{
			name: "sign out other sessions",
			end: func(e *env, id uuid.UUID) response {
				return e.sessionFor(id).post("/auth/sessions/revoke-others", nil)
			},
		},
		{
			name: "enable two-factor authentication",
			refused: func(e *env, id uuid.UUID) response {
				e.enrolTOTP(id, false)
				return e.sessionFor(id).post("/auth/two-factor/enable", map[string]string{"code": e.totpCode(id, -3)})
			},
			end: func(e *env, id uuid.UUID) response {
				return e.sessionFor(id).post("/auth/two-factor/enable", map[string]string{"code": e.totpCode(id, 0)})
			},
		},
		{
			name:      "disable two-factor authentication",
			twoFactor: true,
			refused: func(e *env, id uuid.UUID) response {
				return e.sessionFor(id).post("/auth/two-factor/disable", map[string]string{"password": strongPassword, "backupCode": "aaaaa-bbbbb"})
			},
			end: func(e *env, id uuid.UUID) response {
				return e.sessionFor(id).post("/auth/two-factor/disable", map[string]string{"password": strongPassword, "backupCode": e.totp[id].BackupCodes[0]})
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t)
			ada := e.seedUser("ada@example.com", strongPassword, userOpts{twoFactor: tc.twoFactor})
			bob := e.seedUser("bob@example.com", strongPassword, userOpts{})
			e.seedOAuthGrant(ada)
			e.seedOAuthGrant(ada)
			e.seedOAuthGrant(bob)
			if tc.refused != nil {
				if r := tc.refused(e, ada); r.Code < 400 {
					t.Fatalf("the refused request succeeded: %d %s", r.Code, r.Body.String())
				}
				if n := e.liveOAuthTokens(ada); n != 4 {
					t.Fatalf("a refused request revoked OAuth tokens: %d live, want 4", n)
				}
			}
			if r := tc.end(e, ada); r.Code >= 300 {
				t.Fatalf("%d %s", r.Code, r.Body.String())
			}
			if n := e.liveOAuthTokens(ada); n != 0 {
				t.Fatalf("%d of the user's OAuth tokens survived", n)
			}
			if n := e.liveOAuthTokens(bob); n != 2 {
				t.Fatalf("another user's OAuth tokens were revoked: %d live, want 2", n)
			}
		})
	}
}
