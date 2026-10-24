package oauthserver

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/secret"
)

const (
	sessionCookie      = "forma.session_token"
	confirmationCookie = "forma.session_token.oauth_logout_confirmation"
)

// signedIn stores a browser session for user and returns its id and cookie token.
func (e *env) signedIn(user uuid.UUID) (uuid.UUID, string) {
	e.t.Helper()
	token := uuid.NewString()
	var id uuid.UUID
	if err := e.db.Raw(`INSERT INTO sessions (user_id, token_hash, expires_at) VALUES (?, ?, ?) RETURNING id`, user, secret.HashToken(token), time.Now().Add(time.Hour)).Row().Scan(&id); err != nil {
		e.t.Fatal(err)
	}
	return id, token
}

// cookieOf is the Set-Cookie the response sent for name.
func cookieOf(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func cookieHeader(cookies map[string]string) string {
	parts := []string{}
	for name, value := range cookies {
		parts = append(parts, name+"="+value)
	}
	return strings.Join(parts, "; ")
}

var (
	browser = map[string]string{"Accept": "text/html", "Sec-Fetch-Mode": "navigate"}
	script  = map[string]string{"Accept": "application/json"}
)

func with(headers map[string]string, extra ...string) map[string]string {
	out := map[string]string{}
	for k, v := range headers {
		out[k] = v
	}
	for i := 0; i+1 < len(extra); i += 2 {
		out[extra[i]] = extra[i+1]
	}
	return out
}

// Better Auth's rpInitiatedLogoutEndpoint for clients that cannot initiate logout: a request naming no client, or a hint naming no known client from a signed-in browser, needs the user's confirmation, which only a browser navigation can give.
func TestEndSessionAsksForConfirmation(t *testing.T) {
	e := newEnv(t)
	user := e.user("ada@example.com")
	clientID := e.publicClient()
	_, token := e.signedIn(user)
	session := map[string]string{sessionCookie: token}
	for _, tc := range []struct {
		name     string
		query    url.Values
		headers  map[string]string
		cookies  map[string]string
		status   int
		contains string
		confirm  bool
	}{
		{"browser without a session", url.Values{}, browser, nil, http.StatusOK, "Confirm logout", true},
		{"browser with a session", url.Values{}, browser, session, http.StatusOK, "Confirm logout", true},
		{"script with a session", url.Values{}, script, session, http.StatusBadRequest, "User confirmation is required to complete logout", false},
		{"script without a session", url.Values{}, script, nil, http.StatusBadRequest, "No active session is available for logout", false},
		{"unknown hint client, browser with a session", url.Values{"id_token_hint": {"not-a-jwt"}}, browser, session, http.StatusOK, "Confirm logout", true},
		{"unknown hint client, browser without a session", url.Values{"id_token_hint": {"not-a-jwt"}}, browser, nil, http.StatusBadRequest, "The logout client does not exist", false},
		{"unknown hint client, script with a session", url.Values{"id_token_hint": {"not-a-jwt"}}, script, session, http.StatusBadRequest, "The logout client does not exist", false},
		{"unknown client_id, browser with a session", url.Values{"client_id": {"nope"}}, browser, session, http.StatusBadRequest, "The logout client does not exist", false},
		{"registered client, browser with a session", url.Values{"client_id": {clientID}}, browser, session, http.StatusUnauthorized, "The client is not allowed to initiate logout", false},
	} {
		headers := with(tc.headers, "Cookie", cookieHeader(tc.cookies))
		rec := e.do(http.MethodGet, "/api/auth/oauth2/end-session?"+tc.query.Encode(), nil, headers)
		if rec.Code != tc.status || !strings.Contains(rec.Body.String(), tc.contains) {
			t.Errorf("%s: %d %s", tc.name, rec.Code, rec.Body.String())
		}
		if got := cookieOf(rec, confirmationCookie) != nil; got != tc.confirm {
			t.Errorf("%s: confirmation state set %v", tc.name, got)
		}
		if tc.confirm && !strings.Contains(rec.Body.String(), `action="`+issuer+`/oauth2/end-session/confirm"`) {
			t.Errorf("%s: form %s", tc.name, rec.Body.String())
		}
	}
	if n := e.count(`SELECT count(*) FROM sessions`); n != 1 {
		t.Fatalf("asking for confirmation ended the session: %d sessions", n)
	}
}

// The confirmation form ends the session the page was shown for and clears its cookie; a forged, missing or foreign state, or a post from another site, ends nothing.
func TestEndSessionConfirm(t *testing.T) {
	e := newEnv(t)
	user := e.user("ada@example.com")
	sessionID, token := e.signedIn(user)
	_, otherToken := e.signedIn(user)
	confirmPage := func(cookies map[string]string) string {
		t.Helper()
		rec := e.do(http.MethodGet, "/api/auth/oauth2/end-session", nil, with(browser, "Cookie", cookieHeader(cookies)))
		c := cookieOf(rec, confirmationCookie)
		if c == nil || !c.HttpOnly || c.Path != "/api/auth/oauth2/end-session/confirm" || c.SameSite != http.SameSiteLaxMode || c.MaxAge != 300 {
			t.Fatalf("confirmation cookie %+v", c)
		}
		return c.Value
	}
	confirm := func(headers map[string]string, cookies map[string]string, form string) *httptest.ResponseRecorder {
		t.Helper()
		h := with(headers, "Content-Type", "application/x-www-form-urlencoded", "Origin", webApp, "Cookie", cookieHeader(cookies))
		return e.do(http.MethodPost, "/api/auth/oauth2/end-session/confirm", strings.NewReader(form), h)
	}
	state := confirmPage(map[string]string{sessionCookie: token})

	refusals := []struct {
		name    string
		headers map[string]string
		cookies map[string]string
		status  int
		desc    string
	}{
		{"no state", browser, map[string]string{sessionCookie: token}, http.StatusBadRequest, "The logout confirmation is invalid or expired"},
		{"forged state", browser, map[string]string{sessionCookie: token, confirmationCookie: state + "x"}, http.StatusBadRequest, "The logout confirmation is invalid or expired"},
		{"another session's state", browser, map[string]string{sessionCookie: otherToken, confirmationCookie: state}, http.StatusBadRequest, "The logout confirmation is invalid or expired"},
		{"cross-site post", with(browser, "Origin", "https://evil.example"), map[string]string{sessionCookie: token, confirmationCookie: state}, http.StatusForbidden, "Invalid origin"},
	}
	for _, tc := range refusals {
		h := with(tc.headers, "Content-Type", "application/x-www-form-urlencoded", "Cookie", cookieHeader(tc.cookies))
		if _, ok := tc.headers["Origin"]; !ok {
			h["Origin"] = webApp
		}
		rec := e.do(http.MethodPost, "/api/auth/oauth2/end-session/confirm", strings.NewReader("action=confirm"), h)
		if rec.Code != tc.status || !strings.Contains(rec.Body.String(), tc.desc) {
			t.Errorf("%s: %d %s", tc.name, rec.Code, rec.Body.String())
		}
	}
	if n := e.count(`SELECT count(*) FROM sessions`); n != 2 {
		t.Fatalf("a refused confirmation ended a session: %d sessions", n)
	}

	rec := confirm(browser, map[string]string{sessionCookie: token, confirmationCookie: state}, "action=confirm")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `data-oidc-logout-state="logged-out">Logged out.`) {
		t.Fatalf("confirm: %d %s", rec.Code, rec.Body.String())
	}
	var ended int
	_ = e.db.Raw(`SELECT count(*) FROM sessions WHERE id = ?`, sessionID).Row().Scan(&ended)
	if ended != 0 || e.count(`SELECT count(*) FROM sessions`) != 1 {
		t.Fatalf("the confirmed session was not the one ended")
	}
	if c := cookieOf(rec, sessionCookie); c == nil || c.Value != "" || c.MaxAge >= 0 {
		t.Errorf("session cookie not cleared: %+v", c)
	}
	if c := cookieOf(rec, confirmationCookie); c == nil || c.Value != "" || c.MaxAge >= 0 {
		t.Errorf("confirmation state not cleared: %+v", c)
	}

	// A script's confirmed logout gets an empty answer; a confirmation for a browser that is already signed out just says so.
	scriptState := confirmPage(map[string]string{sessionCookie: otherToken})
	if rec := confirm(script, map[string]string{sessionCookie: otherToken, confirmationCookie: scriptState}, "action=confirm"); rec.Code != http.StatusOK || rec.Body.Len() != 0 || e.count(`SELECT count(*) FROM sessions`) != 0 {
		t.Errorf("script confirm: %d %s", rec.Code, rec.Body.String())
	}
	signedOutState := confirmPage(nil)
	if rec := confirm(browser, map[string]string{confirmationCookie: signedOutState}, "action=confirm"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Logged out.") {
		t.Errorf("signed-out confirm: %d %s", rec.Code, rec.Body.String())
	}
	if rec := confirm(script, map[string]string{confirmationCookie: signedOutState}, "action=confirm"); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "No active session is available for logout") {
		t.Errorf("signed-out script confirm: %d %s", rec.Code, rec.Body.String())
	}
	if rec := confirm(browser, map[string]string{confirmationCookie: signedOutState}, "action=cancel"); rec.Code != http.StatusBadRequest {
		t.Errorf("another action: %d %s", rec.Code, rec.Body.String())
	}
}
