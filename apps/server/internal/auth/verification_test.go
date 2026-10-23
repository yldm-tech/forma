package auth

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/audit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
)

// openSignup lets a test sign several users up through the API.
func openSignup(c *config.Config) { c.E2ETesting = true }

// signUp registers through the API from a fresh browser and returns it with the emailed verification token.
func (e *env) signUp(email, password string) (*browser, string) {
	e.t.Helper()
	b := e.browser()
	body := signupBody(email)
	body["password"] = password
	expectStatus(e.t, b.post("/auth/signup", body), http.StatusOK, "")
	return b, e.lastToken(email, subjectVerification)
}

func (e *env) emailVerified(email string) bool {
	e.t.Helper()
	acc, err := e.svc.repo.accountByEmail(e.t.Context(), email)
	if err != nil || acc == nil {
		e.t.Fatalf("account %s: %v", email, err)
	}
	return acc.EmailVerified
}

func withheldReasons(e *env) []any {
	var out []any
	for _, ev := range e.auditEvents(audit.ActionUpdated) {
		if field(ev, "changes", "verificationSessionWithheldMarker") == true {
			out = append(out, field(ev, "changes", "reason"))
		}
	}
	return out
}

// forma2's better-auth-verification-autosignin.integration.test.ts: the browser that signed up is signed in by the link; its intent is spent.
func TestVerifySignsInTheSignupBrowser(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	b, token := e.signUp("ada@example.com", strongPassword)
	r := b.post("/auth/verification/verify", map[string]string{"token": token})
	expectStatus(t, r, http.StatusOK, "")
	if r.json()["signedIn"] != true || !b.has(sessionCookieName) || b.has(signupIntentCookieName) {
		t.Fatalf("verify: %s, cookies %v", r.Body.String(), b.cookies)
	}
	if !e.emailVerified("ada@example.com") {
		t.Fatal("email not verified")
	}
	expectStatus(t, b.get("/auth/me"), http.StatusOK, "")
	if len(e.auditEvents(audit.ActionSignedIn)) != 1 {
		t.Fatal("the verification sign-in was not audited")
	}
	if e.count(`SELECT count(*) FROM verifications WHERE purpose = 'signup_intent' AND consumed_at IS NULL`) != 0 {
		t.Fatal("the intent was not consumed")
	}
}

// ENG-2562: any other browser verifies the address but is not signed in, and the reason is recorded.
func TestVerifyWithholdsTheSessionFromOtherBrowsers(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	_, token := e.signUp("ada@example.com", strongPassword)
	other := e.browser()
	r := other.post("/auth/verification/verify", map[string]string{"token": token})
	expectStatus(t, r, http.StatusOK, "")
	if r.json()["signedIn"] != false || other.has(sessionCookieName) {
		t.Fatalf("a stranger was signed in: %s", r.Body.String())
	}
	if !e.emailVerified("ada@example.com") {
		t.Fatal("email not verified")
	}
	if got := withheldReasons(e); len(got) != 1 || got[0] != withheldAbsent {
		t.Fatalf("withheld reasons %v", got)
	}
	if e.count(`SELECT count(*) FROM sessions`) != 0 {
		t.Fatal("a session was created")
	}
}

func TestVerifyClassifiesTheIntent(t *testing.T) {
	t.Parallel()
	e := newEnv(t, openSignup)
	attacker, _ := e.signUp("mallory@example.com", strongPassword)
	_, victimToken := e.signUp("victim@example.com", strongPassword)

	// The attacker's own intent names a different account.
	expectStatus(t, attacker.post("/auth/verification/verify", map[string]string{"token": victimToken}), http.StatusOK, "")
	if attacker.has(sessionCookieName) {
		t.Fatal("an intent for another account signed in")
	}

	garbage := e.browser()
	garbage.cookies[signupIntentCookieName] = &http.Cookie{Name: signupIntentCookieName, Value: "not-a-token"}
	_, otherToken := e.signUp("grace@example.com", strongPassword)
	expectStatus(t, garbage.post("/auth/verification/verify", map[string]string{"token": otherToken}), http.StatusOK, "")
	if garbage.has(sessionCookieName) {
		t.Fatal("a garbage intent signed in")
	}
	if got := withheldReasons(e); len(got) != 2 || got[0] != withheldOtherUser || got[1] != withheldInvalid {
		t.Fatalf("withheld reasons %v", got)
	}
}

// The documented residual: whoever registered an address keeps a working password until the owner resets it.
func TestVerifyLeavesThePreRegisteredPassword(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	_, token := e.signUp("victim@example.com", "Attacker-Chosen-1")
	victim := e.browser()
	expectStatus(t, victim.post("/auth/verification/verify", map[string]string{"token": token}), http.StatusOK, "")
	if victim.has(sessionCookieName) {
		t.Fatal("the victim was signed into the attacker's account")
	}
	e.browser().signIn("victim@example.com", "Attacker-Chosen-1")
}

// An already verified address short-circuits: no second sign-in, even from the browser that signed up.
func TestVerifyIsUsefulOnce(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	b, token := e.signUp("ada@example.com", strongPassword)
	expectStatus(t, e.browser().post("/auth/verification/verify", map[string]string{"token": token}), http.StatusOK, "")
	r := b.post("/auth/verification/verify", map[string]string{"token": token})
	expectStatus(t, r, http.StatusOK, "")
	if r.json()["signedIn"] != false || b.has(sessionCookieName) {
		t.Fatalf("second use signed in: %s", r.Body.String())
	}

	// A signed-in user re-opening their link is simply told they are signed in.
	b.signIn("ada@example.com", strongPassword)
	if r := b.post("/auth/verification/verify", map[string]string{"token": token}); r.json()["signedIn"] != true {
		t.Fatalf("signed-in re-verify: %s", r.Body.String())
	}
}

func TestVerifyRejectsBadTokens(t *testing.T) {
	t.Parallel()
	e := newEnv(t, openSignup)
	b, token := e.signUp("ada@example.com", strongPassword)
	expectStatus(t, b.post("/auth/verification/verify", map[string]string{"token": "unknown"}), http.StatusBadRequest, CodeInvalidToken)
	expectStatus(t, b.post("/auth/verification/verify", map[string]string{}), http.StatusBadRequest, apperr.CodeValidation)

	e.advance(time.Hour + time.Second)
	expectStatus(t, b.post("/auth/verification/verify", map[string]string{"token": token}), http.StatusBadRequest, CodeTokenExpired)
	if e.emailVerified("ada@example.com") {
		t.Fatal("an expired link verified the address")
	}

	// A link for an address the account no longer has verifies nothing.
	fresh, token2 := e.signUp("grace@example.com", strongPassword)
	e.exec(`UPDATE users SET email = 'grace@elsewhere.example' WHERE email = 'grace@example.com'`)
	expectStatus(t, fresh.post("/auth/verification/verify", map[string]string{"token": token2}), http.StatusBadRequest, CodeInvalidToken)
}

// forma2's resendVerificationEmailAction, without its existence oracle: unknown and verified addresses get the same answer and no mail.
func TestResendVerification(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.seedUser("verified@example.com", strongPassword, userOpts{})
	id := e.seedUser("pending@example.com", strongPassword, userOpts{unverified: true})
	b := e.browser()
	for _, email := range []string{"nobody@example.com", "verified@example.com", "pending@example.com"} {
		r := b.post("/auth/verification/resend", map[string]string{"email": email})
		expectStatus(t, r, http.StatusOK, "")
		if r.Body.String() != `{"success":true}` {
			t.Fatalf("%s: %s", email, r.Body.String())
		}
	}
	if len(e.mail.Messages()) != 1 || e.mail.Messages()[0].To != "pending@example.com" {
		t.Fatalf("mails %+v", e.mail.Messages())
	}
	events := e.auditEvents(audit.ActionVerificationEmailSent)
	if len(events) != 1 || field(events[0], "target", "id") != id.String() {
		t.Fatalf("events %v", events)
	}

	// A failed send is not reported to the caller either.
	e.mail.Err = errors.New("smtp down")
	expectStatus(t, b.post("/auth/verification/resend", map[string]string{"email": "pending@example.com"}), http.StatusOK, "")
}

// The intent is renewed with a resend only for a browser that already holds a valid intent for the same account; it is never created by a resend.
func TestResendRenewsTheIntent(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	b, _ := e.signUp("ada@example.com", strongPassword)
	old := b.cookies[signupIntentCookieName].Value

	r := b.post("/auth/verification/resend", map[string]string{"email": "ada@example.com"})
	renewed := r.cookie(signupIntentCookieName)
	if renewed == nil || renewed.Value == old {
		t.Fatalf("intent not renewed: %+v", renewed)
	}
	// The old intent is spent; the new one signs in with the new link even after the old intent would have expired.
	e.advance(50 * time.Minute)
	expectStatus(t, b.post("/auth/verification/resend", map[string]string{"email": "ada@example.com"}), http.StatusOK, "")
	e.advance(50 * time.Minute)
	token := e.lastToken("ada@example.com", subjectVerification)
	if r := b.post("/auth/verification/verify", map[string]string{"token": token}); r.json()["signedIn"] != true {
		t.Fatalf("renewed intent did not sign in: %s", r.Body.String())
	}

	stranger := e.browser()
	r = stranger.post("/auth/verification/resend", map[string]string{"email": "ada@example.com"})
	if r.cookie(signupIntentCookieName) != nil {
		t.Fatal("a resend armed an intent for a browser that had none")
	}
}

// auth:verify allows ten resends per client address per hour; verifying allows thirty per minute.
func TestVerificationRateLimits(t *testing.T) {
	e := newEnv(t)
	e.enableRateLimits()
	e.inOneWindow(func(_ int, ip string) error {
		b := e.browser()
		b.ip = ip
		send := func() response {
			return b.post("/auth/verification/resend", map[string]string{"email": "nobody@example.com"})
		}
		if err := repeat(10, send, http.StatusOK, ""); err != nil {
			return err
		}
		return statusErr(send(), http.StatusTooManyRequests, apperr.CodeTooManyRequests)
	})
	e.inOneWindow(func(run int, _ string) error {
		b := e.browser()
		b.ip = fmt.Sprintf("192.0.2.%d", 10+run)
		send := func() response { return b.post("/auth/verification/verify", map[string]string{"token": "unknown"}) }
		if err := repeat(30, send, http.StatusBadRequest, CodeInvalidToken); err != nil {
			return err
		}
		return statusErr(send(), http.StatusTooManyRequests, apperr.CodeTooManyRequests)
	})
}
