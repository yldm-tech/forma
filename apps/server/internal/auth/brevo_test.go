package auth

import (
	"net/http"
	"testing"

	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
)

// brevoJobs lists the Brevo syncs queued so far, as "<kind> <email>", or "<kind> <old email> -> <email>" for an address change.
func (e *env) brevoJobs() []string {
	e.t.Helper()
	var jobs []string
	if err := e.db.Raw(`SELECT kind || ' ' || coalesce((args->>'oldEmail') || ' -> ', '') || (args->>'email') FROM river_job WHERE kind LIKE 'brevo.%' ORDER BY id`).Scan(&jobs).Error; err != nil {
		e.t.Fatal(err)
	}
	return jobs
}

func withBrevo(c *config.Config) { c.Brevo.APIKey = "xkeysib-test" }

// forma2's Brevo calls, now jobs queued with the change: a contact on the first verification, its address moved on a confirmed email change, and deleted with the account.
func TestBrevoContactLifecycle(t *testing.T) {
	t.Parallel()
	e := newEnv(t, openSignup, withBrevo)
	b, token := e.signUp("ada@example.com", strongPassword)
	if jobs := e.brevoJobs(); len(jobs) != 0 {
		t.Fatalf("queued before verification: %v", jobs)
	}
	expectStatus(t, b.post("/auth/verification/verify", map[string]string{"token": token}), http.StatusOK, "")
	expectStatus(t, b.post("/auth/verification/verify", map[string]string{"token": token}), http.StatusOK, "")
	acc, err := e.svc.repo.accountByEmail(t.Context(), "ada@example.com")
	if err != nil || acc == nil {
		t.Fatalf("account %v, %v", acc, err)
	}

	sess := e.sessionFor(acc.ID)
	expectStatus(t, changeEmail(sess, "new@example.com", strongPassword), http.StatusNoContent, "")
	expectStatus(t, e.browser().post("/auth/email/verify", map[string]string{"token": e.lastToken("new@example.com", subjectEmailChange)}), http.StatusOK, "")

	expectStatus(t, sess.post("/auth/account/delete", map[string]string{"password": strongPassword}), http.StatusOK, "")
	want := []string{"brevo.create-contact ada@example.com", "brevo.update-contact ada@example.com -> new@example.com", "brevo.delete-contact new@example.com"}
	if got := e.brevoJobs(); len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("queued %v, want %v", got, want)
	}
}

// With verification disabled an email change applies at once and the contact follows it; without BREVO_API_KEY nothing is queued.
func TestBrevoImmediateEmailChange(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		configure func(*config.Config)
		want      []string
	}{
		"configured":     {withBrevo, []string{"brevo.update-contact ada@example.com -> new@example.com"}},
		"not configured": {func(*config.Config) {}, nil},
	} {
		e := newEnv(t, func(c *config.Config) { c.EmailVerificationDisabled = true }, tc.configure)
		id := e.seedUser("ada@example.com", strongPassword, userOpts{})
		expectStatus(t, changeEmail(e.sessionFor(id), "new@example.com", strongPassword), http.StatusNoContent, "")
		if got := e.brevoJobs(); len(got) != len(tc.want) || (len(got) == 1 && got[0] != tc.want[0]) {
			t.Errorf("%s: queued %v, want %v", name, got, tc.want)
		}
	}
}

// forma2's SSO provisioning added the new user to Brevo at once; a later sign-in through the same identity adds nothing.
func TestBrevoContactOnSSOSignUp(t *testing.T) {
	t.Parallel()
	e, fake := ssoEnv(t, providerGoogle, withBrevo)
	fake.set(func(f *fakeIdP) { f.profile = googleProfile("g-1", "Ada@Example.com") })
	expectRedirect(t, e.browser().ssoSignIn(fake, "google", "/"), webApp+"/")
	expectRedirect(t, e.browser().ssoSignIn(fake, "google", "/"), webApp+"/")
	if got := e.brevoJobs(); len(got) != 1 || got[0] != "brevo.create-contact ada@example.com" {
		t.Fatalf("queued %v", got)
	}
}
