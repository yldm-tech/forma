package auth

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/platform/audit"
	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/platform/mailer"
	"github.com/yldm-tech/forma/server/internal/platform/secret"
)

func signupBody(email string) map[string]any {
	return map[string]any{"name": "Ada Lovelace", "email": email, "password": strongPassword}
}

// forma2's signup-duplicate-email and auth-email-password tests: a first sign-up creates the user with a bcrypt credential, the organization a new user gets, and one verification mail; the intent cookie marks this browser.
func TestSignupCreatesTheAccount(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	b := e.browser()
	body := signupBody("Ada@Example.com")
	body["userLocale"] = "ja-JP"
	r := b.post("/auth/signup", body)
	expectStatus(t, r, http.StatusOK, "")
	if r.Body.String() != `{"success":true}` {
		t.Fatalf("body %s", r.Body.String())
	}

	acc, err := e.svc.repo.accountByEmail(t.Context(), "ada@example.com")
	if err != nil || acc == nil {
		t.Fatalf("account: %v, %v", acc, err)
	}
	if acc.Email != "ada@example.com" || acc.Locale != "ja-JP" || acc.EmailVerified || !acc.IsActive || acc.Name != "Ada Lovelace" {
		t.Fatalf("account %+v", acc)
	}
	if !strings.HasPrefix(acc.PasswordHash.String, "$2a$12$") {
		t.Fatalf("password hash %q is not bcrypt cost 12", acc.PasswordHash.String)
	}
	if ok, _ := secret.CheckPassword(acc.PasswordHash.String, strongPassword); !ok {
		t.Fatal("stored hash does not verify")
	}

	var org struct {
		ID   string
		Name string
		Role string
	}
	e.db.Raw(`SELECT o.id, o.name, m.role FROM organizations o JOIN memberships m ON m.organization_id = o.id WHERE m.user_id = ? AND m.accepted`, acc.ID).Scan(&org)
	if org.Name != "Ada Lovelace's Organization" || org.Role != "owner" {
		t.Fatalf("organization %+v", org)
	}
	if n := e.count(`SELECT count(*) FROM workspaces w JOIN languages l ON l.workspace_id = w.id WHERE w.organization_id = ? AND w.name = 'My workspace' AND l.code = 'en-US'`, org.ID); n != 1 {
		t.Fatalf("default workspace with language: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM contact_attribute_keys k JOIN workspaces w ON w.id = k.workspace_id WHERE w.organization_id = ? AND k.type = 'default'`, org.ID); n != 5 {
		t.Fatalf("%d default contact attribute keys, want 5", n)
	}
	var settings string
	e.db.Raw(`SELECT notification_settings::text FROM users WHERE id = ?`, acc.ID).Scan(&settings)
	if settings != `{"alert": {}, "unsubscribedOrganizationIds": ["`+org.ID+`"]}` {
		t.Fatalf("notification settings %s", settings)
	}

	msgs := e.sent("ada@example.com")
	jaSubject, _, _ := e.svc.mailer.Render("ja-JP", mailer.Verification{})
	if len(msgs) != 1 || msgs[0].Subject != jaSubject {
		t.Fatalf("want one verification mail in the chosen locale, got %+v", msgs)
	}
	if !strings.Contains(msgs[0].HTML, webApp+verifyEmailPath+"?token=") {
		t.Fatal("verification mail has no link to the verify page")
	}
	intent := r.cookie(signupIntentCookieName)
	if intent == nil || !intent.HttpOnly || intent.SameSite != http.SameSiteLaxMode || intent.Path != "/" || intent.MaxAge != 3600 || intent.Secure {
		t.Fatalf("intent cookie %+v", intent)
	}
	events := e.auditEvents(audit.ActionCreated)
	if len(events) != 1 || field(events[0], "target", "id") != acc.ID.String() || field(events[0], "organizationId") != org.ID || field(events[0], "status") != "success" {
		t.Fatalf("created events %v", events)
	}
	if strings.Contains(e.logs.String(), "ada@example.com") {
		t.Fatal("the address reached the logs")
	}
}

// forma2's ENG-2091/ENG-2099: a duplicate (in any case) is answered exactly like a new account, a Set-Cookie included, and writes, mails and audits nothing.
func TestSignupDuplicateIsIndistinguishable(t *testing.T) {
	t.Parallel()
	e := newEnv(t, func(c *config.Config) { c.E2ETesting = true })
	e.seedUser("ada@example.com", strongPassword, userOpts{})
	users := e.count(`SELECT count(*) FROM users`)
	orgs := e.count(`SELECT count(*) FROM organizations`)
	before := len(e.auditEvents(audit.ActionCreated))

	r := e.browser().post("/auth/signup", signupBody("ADA@example.com"))
	expectStatus(t, r, http.StatusOK, "")
	if r.Body.String() != `{"success":true}` || r.cookie(signupIntentCookieName) == nil {
		t.Fatalf("duplicate answered %s with cookies %v", r.Body.String(), r.Result().Cookies())
	}
	if e.count(`SELECT count(*) FROM users`) != users || e.count(`SELECT count(*) FROM organizations`) != orgs {
		t.Fatal("a duplicate sign-up wrote rows")
	}
	if len(e.mail.Messages()) != 0 {
		t.Fatal("a duplicate sign-up sent mail")
	}
	if len(e.auditEvents(audit.ActionCreated)) != before {
		t.Fatal("a duplicate sign-up was audited as created")
	}
	// The decoy intent was never stored.
	if e.count(`SELECT count(*) FROM verifications WHERE purpose = 'signup_intent'`) != 0 {
		t.Fatal("a decoy intent was stored")
	}
}

// forma2's signup-closed-instance tests: a self-hosted instance admits its first administrator, then refuses uninvited sign-ups the same way for registered and unregistered addresses.
func TestSignupOnAClosedInstance(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	b := e.browser()
	expectStatus(t, b.post("/auth/signup", signupBody("admin@example.com")), http.StatusOK, "")

	registered := b.post("/auth/signup", signupBody("admin@example.com"))
	unregistered := b.post("/auth/signup", signupBody("newcomer@example.com"))
	expectStatus(t, registered, http.StatusBadRequest, CodeSignupDisabled)
	if registered.Body.String() != unregistered.Body.String() || registered.Code != unregistered.Code {
		t.Fatalf("registered %d %s, unregistered %d %s", registered.Code, registered.Body.String(), unregistered.Code, unregistered.Body.String())
	}
	if e.count(`SELECT count(*) FROM users`) != 1 {
		t.Fatal("a closed instance created a user")
	}
	failures := e.auditEvents(audit.ActionCreated)
	if last := failures[len(failures)-1]; field(last, "status") != "failure" || field(last, "target", "id") != "unknown" {
		t.Fatalf("refused sign-up audited as %v", last)
	}
}

// Two uninvited sign-ups racing on a fresh closed instance: exactly one becomes the first administrator, the other is refused as any later uninvited sign-up is.
func TestFirstAdministratorSignupRace(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	emails := []string{"first@example.com", "second@example.com"}
	responses := make([]response, len(emails))
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i, email := range emails {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b := e.browser()
			<-start
			responses[i] = b.post("/auth/signup", signupBody(email))
		}()
	}
	close(start)
	wg.Wait()

	admitted, refused := 0, 0
	for _, r := range responses {
		switch {
		case r.Code == http.StatusOK:
			admitted++
		case statusErr(r, http.StatusBadRequest, CodeSignupDisabled) == nil:
			refused++
		default:
			t.Fatalf("unexpected answer %d %s", r.Code, r.Body.String())
		}
	}
	if admitted != 1 || refused != 1 || e.count(`SELECT count(*) FROM users`) != 1 || e.count(`SELECT count(*) FROM organizations`) != 1 {
		t.Fatalf("%d admitted, %d refused, %d users, %d organizations", admitted, refused, e.count(`SELECT count(*) FROM users`), e.count(`SELECT count(*) FROM organizations`))
	}
}

func TestSignupOpenOnFormaCloudAndForE2E(t *testing.T) {
	t.Parallel()
	for name, set := range map[string]func(*config.Config){
		"cloud": func(c *config.Config) { c.IsFormaCloud = true },
		"e2e":   func(c *config.Config) { c.E2ETesting = true },
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t, set)
			e.seedUser("first@example.com", strongPassword, userOpts{})
			expectStatus(t, e.browser().post("/auth/signup", signupBody("second@example.com")), http.StatusOK, "")
			if e.count(`SELECT count(*) FROM users`) != 2 {
				t.Fatal("public sign-up did not create the user")
			}
		})
	}
}

// Forma Cloud refuses personal and disposable mail domains; a self-hosted instance never does.
func TestSignupEmailDomainBlock(t *testing.T) {
	t.Parallel()
	cloud := newEnv(t, func(c *config.Config) { c.IsFormaCloud = true })
	expectStatus(t, cloud.browser().post("/auth/signup", signupBody("ada@Gmail.com")), http.StatusBadRequest, CodeEmailDomainNotAllowed)
	expectStatus(t, cloud.browser().post("/auth/signup", signupBody("alice@mailinator.com")), http.StatusBadRequest, CodeEmailDomainNotAllowed)
	if cloud.count(`SELECT count(*) FROM users`) != 0 {
		t.Fatal("a blocked domain created a user")
	}
	self := newEnv(t)
	expectStatus(t, self.browser().post("/auth/signup", signupBody("ada@gmail.com")), http.StatusOK, "")
}

// seedInvite stores an invite to email from creator, with one link, and returns the link's token.
func (e *env) seedInvite(org, creator uuid.UUID, email, role string, inviteTTL, linkTTL time.Duration, teams ...uuid.UUID) string {
	e.t.Helper()
	inviteID := e.insertID(`INSERT INTO invites (organization_id, email, name, creator_id, expires_at, role) VALUES (?, ?, 'Grace', ?, ?, ?) RETURNING id`, org, email, creator, time.Now().Add(inviteTTL), role)
	for _, team := range teams {
		e.exec(`INSERT INTO invite_teams (invite_id, team_id) VALUES (?, ?)`, inviteID, team)
	}
	token, err := secret.NewToken()
	if err != nil {
		e.t.Fatal(err)
	}
	e.exec(`INSERT INTO invite_tokens (invite_id, token_hash, expires_at) VALUES (?, ?, ?)`, inviteID, secret.HashToken(token), time.Now().Add(linkTTL))
	return token
}

// invitingOrganization is a closed instance's existing organization with an owner who invites, and a team.
func (e *env) invitingOrganization() (org, owner, team uuid.UUID) {
	e.t.Helper()
	owner = e.seedUser("owner@example.com", strongPassword, userOpts{name: "Olivia Owner", locale: "ja-JP"})
	org = e.insertID(`INSERT INTO organizations (name) VALUES ('Acme') RETURNING id`)
	e.exec(`INSERT INTO memberships (organization_id, user_id, role, accepted) VALUES (?, ?, 'owner', true)`, org, owner)
	team = e.insertID(`INSERT INTO teams (organization_id, name) VALUES (?, 'Design') RETURNING id`, org)
	return org, owner, team
}

// forma2's signup-invite.integration.test.ts, "new invitee accepted": a valid invite admits its holder on a closed instance, into the inviting organization with the invite's role and teams instead of one of their own, and the inviter hears about it.
func TestSignupWithInvite(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	org, _, team := e.invitingOrganization()
	foreignTeam := e.insertID(`INSERT INTO teams (organization_id, name) VALUES (?, 'Elsewhere') RETURNING id`, e.insertID(`INSERT INTO organizations (name) VALUES ('Other') RETURNING id`))
	token := e.seedInvite(org, e.insertID(`SELECT id FROM users WHERE email = 'owner@example.com'`), "grace@example.com", "manager", 7*24*time.Hour, 7*24*time.Hour, team, foreignTeam)
	orgs := e.count(`SELECT count(*) FROM organizations`)

	body := signupBody("Grace@Example.com")
	body["name"] = "Grace Hopper"
	body["inviteToken"] = "  " + token + " "
	r := e.browser().post("/auth/signup", body)
	expectStatus(t, r, http.StatusOK, "")
	if r.Body.String() != `{"success":true}` || r.cookie(signupIntentCookieName) == nil {
		t.Fatalf("sign-up answered %s", r.Body.String())
	}

	acc, err := e.svc.repo.accountByEmail(t.Context(), "grace@example.com")
	if err != nil || acc == nil {
		t.Fatalf("account: %v, %v", acc, err)
	}
	var role string
	var accepted bool
	if err := e.db.Raw(`SELECT role, accepted FROM memberships WHERE organization_id = ? AND user_id = ?`, org, acc.ID).Row().Scan(&role, &accepted); err != nil {
		t.Fatal(err)
	}
	if role != "manager" || !accepted || e.count(`SELECT count(*) FROM memberships WHERE user_id = ?`, acc.ID) != 1 {
		t.Fatalf("membership %q accepted %v; want only the invited one", role, accepted)
	}
	if e.count(`SELECT count(*) FROM organizations`) != orgs {
		t.Fatal("an invitee got an organization of their own")
	}
	// A manager joins the invite's teams as an admin; a team of another organization is never joined.
	if e.count(`SELECT count(*) FROM team_members WHERE user_id = ? AND team_id = ? AND role = 'admin'`, acc.ID, team) != 1 || e.count(`SELECT count(*) FROM team_members WHERE user_id = ?`, acc.ID) != 1 {
		t.Fatal("team memberships do not match the invite")
	}
	var settings string
	e.db.Raw(`SELECT notification_settings::text FROM users WHERE id = ?`, acc.ID).Scan(&settings)
	if settings != `{"alert": {}, "unsubscribedOrganizationIds": ["`+org.String()+`"]}` {
		t.Fatalf("notification settings %s", settings)
	}
	if e.count(`SELECT count(*) FROM invites`)+e.count(`SELECT count(*) FROM invite_tokens`) != 0 {
		t.Fatal("the invite outlived its acceptance")
	}

	accepted2 := e.sent("owner@example.com")
	jaSubject, _, _ := e.svc.mailer.Render("ja-JP", mailer.InviteAccepted{})
	if len(accepted2) != 1 || accepted2[0].Subject != jaSubject || !strings.Contains(accepted2[0].HTML, "Grace Hopper") {
		t.Fatalf("want the accepted mail to the inviter in their language, got %+v", accepted2)
	}
	if len(e.sent("grace@example.com")) != 1 {
		t.Fatal("the invitee was not sent the verification mail")
	}
	events := e.auditEvents(audit.ActionCreated)
	if last := events[len(events)-1]; field(last, "organizationId") != org.String() {
		t.Fatalf("created/user is attributed to %v, want the invited organization", field(last, "organizationId"))
	}

	// The link was single-use.
	again := signupBody("grace2@example.com")
	again["inviteToken"] = token
	expectStatus(t, e.browser().post("/auth/signup", again), http.StatusBadRequest, CodeInviteTokenInvalid)
}

// forma2's signup-invite.integration.test.ts: an existing account is answered like a new one and left untouched, and the invite survives; a mismatched address, an expired invite or link, or anything that is not a link is refused before a user exists; a blank token means no invite.
func TestSignupInviteRefusals(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	org, owner, _ := e.invitingOrganization()

	existing := e.seedUser("grace@example.com", strongPassword, userOpts{})
	token := e.seedInvite(org, owner, "grace@example.com", "member", time.Hour, time.Hour)
	body := signupBody("grace@example.com")
	body["inviteToken"] = token
	r := e.browser().post("/auth/signup", body)
	expectStatus(t, r, http.StatusOK, "")
	if r.Body.String() != `{"success":true}` || e.count(`SELECT count(*) FROM memberships WHERE user_id = ?`, existing) != 0 || e.count(`SELECT count(*) FROM invites`) != 1 {
		t.Fatal("a sign-up for an existing account acted on it")
	}

	users := e.count(`SELECT count(*) FROM users`)
	cases := map[string]string{
		"a different address": e.seedInvite(org, owner, "someone@example.com", "owner", time.Hour, time.Hour),
		"an expired invite":   e.seedInvite(org, owner, "late@example.com", "member", -time.Minute, time.Hour),
		"an expired link":     e.seedInvite(org, owner, "stale@example.com", "member", time.Hour, -time.Minute),
		"not a link at all":   "eyJhbGciOiJIUzI1NiJ9.e30.x",
	}
	for name, token := range cases {
		t.Run(name, func(t *testing.T) {
			address := "stale@example.com"
			if name == "an expired invite" {
				address = "late@example.com"
			}
			if name == "a different address" {
				address = "ada@example.com"
			}
			body := signupBody(address)
			body["inviteToken"] = token
			expectStatus(t, e.browser().post("/auth/signup", body), http.StatusBadRequest, CodeInviteTokenInvalid)
		})
	}
	if e.count(`SELECT count(*) FROM users`) != users {
		t.Fatal("a refused invite left a user behind")
	}

	// A blank token is no invite, and a closed instance refuses it as an uninvited sign-up.
	blank := signupBody("newcomer@example.com")
	blank["inviteToken"] = "   "
	expectStatus(t, e.browser().post("/auth/signup", blank), http.StatusBadRequest, CodeSignupDisabled)
}

// forma2's cloud domain rule: invitees may use a personal address unless SIGNUP_DOMAIN_CHECK_ON_INVITES=1.
func TestSignupInviteDomainCheck(t *testing.T) {
	t.Parallel()
	for _, check := range []bool{false, true} {
		e := newEnv(t, func(c *config.Config) { c.IsFormaCloud = true; c.SignupDomainCheckOnInvites = check })
		org, owner, _ := e.invitingOrganization()
		body := signupBody("grace@gmail.com")
		body["inviteToken"] = e.seedInvite(org, owner, "grace@gmail.com", "member", time.Hour, time.Hour)
		r := e.browser().post("/auth/signup", body)
		if check {
			expectStatus(t, r, http.StatusBadRequest, CodeEmailDomainNotAllowed)
		} else {
			expectStatus(t, r, http.StatusOK, "")
		}
	}
}

// forma2's better-auth-hibp.integration.test.ts: a breached password creates no user; an unreachable breach service lets the sign-up through.
func TestSignupBreachCheck(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.breach(strongPassword)
	expectStatus(t, e.browser().post("/auth/signup", signupBody("ada@example.com")), http.StatusBadRequest, CodePasswordCompromised)
	if e.count(`SELECT count(*) FROM users`) != 0 {
		t.Fatal("a breached password created a user")
	}

	e.setHIBPStatus(http.StatusServiceUnavailable)
	expectStatus(t, e.browser().post("/auth/signup", signupBody("ada@example.com")), http.StatusOK, "")
	if e.count(`SELECT count(*) FROM users`) != 1 {
		t.Fatal("the check did not fail open")
	}

	off := newEnv(t, func(c *config.Config) { c.PasswordHIBPCheckDisabled = true })
	off.breach(strongPassword)
	expectStatus(t, off.browser().post("/auth/signup", signupBody("ada@example.com")), http.StatusOK, "")
	if off.hibpCallCount() != 0 {
		t.Fatal("PASSWORD_HIBP_CHECK_DISABLED still called the service")
	}
}

func TestSignupValidation(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	r := e.browser().post("/auth/signup", map[string]any{"name": " ", "email": "not-an-email", "password": "short", "userLocale": "xx"})
	expectStatus(t, r, http.StatusBadRequest, apperr.CodeValidation)
	fields := map[string]string{}
	for _, f := range field(r.json(), "details", "fields").([]any) {
		m := f.(map[string]any)
		fields[m["field"].(string)] = m["rule"].(string)
	}
	want := map[string]string{"name": "min", "email": "email", "password": "min", "userLocale": "oneof"}
	for k, v := range want {
		if fields[k] != v {
			t.Errorf("field %s rule %q, want %q (all: %v)", k, fields[k], v, fields)
		}
	}
	if e.count(`SELECT count(*) FROM users`) != 0 {
		t.Fatal("an invalid request created a user")
	}
}

func TestSignupTurnstile(t *testing.T) {
	t.Parallel()
	e := newEnv(t, func(c *config.Config) { c.TurnstileSiteKey, c.TurnstileSecretKey = "site", "secret" })
	r := e.browser().post("/auth/signup", signupBody("ada@example.com"))
	expectStatus(t, r, http.StatusBadRequest, apperr.CodeValidation)
	if !strings.Contains(r.Body.String(), `"field":"turnstileToken"`) {
		t.Fatalf("missing token not reported: %s", r.Body.String())
	}

	body := signupBody("ada@example.com")
	body["turnstileToken"] = "token"
	e.setTurnstile(false)
	expectStatus(t, e.browser().post("/auth/signup", body), http.StatusBadRequest, CodeCaptchaFailed)
	e.setTurnstile(true)
	expectStatus(t, e.browser().post("/auth/signup", body), http.StatusOK, "")
}

// forma2's signup-verification-send.integration.test.ts: whatever happens to the verification mail, the account exists and the response is byte-identical.
func TestSignupResponseIgnoresMailFailures(t *testing.T) {
	t.Parallel()
	states := map[string]func(*config.Config){
		"working":        nil,
		"failing":        nil,
		"not configured": func(c *config.Config) { c.SMTPHost = "" },
	}
	var bodies []string
	for name, set := range states {
		t.Run(name, func(t *testing.T) {
			var configure []func(*config.Config)
			if set != nil {
				configure = append(configure, set)
			}
			e := newEnv(t, configure...)
			if name == "failing" {
				e.mail.Err = errors.New("smtp: 451 try later")
			}
			r := e.browser().post("/auth/signup", signupBody("ada@example.com"))
			expectStatus(t, r, http.StatusOK, "")
			bodies = append(bodies, r.Body.String())
			if e.count(`SELECT count(*) FROM users`) != 1 {
				t.Fatal("no account")
			}
			if name != "working" && (!strings.Contains(e.logs.String(), "Failed to send verification email") || !strings.Contains(e.logs.String(), `"emailDomain":"example.com"`)) {
				t.Fatalf("send failure not logged with the domain: %s", e.logs.String())
			}
		})
	}
	for _, b := range bodies[1:] {
		if b != bodies[0] {
			t.Fatalf("responses differ: %q vs %q", b, bodies[0])
		}
	}
}

func TestSignupWithEmailAuthDisabled(t *testing.T) {
	t.Parallel()
	e := newEnv(t, func(c *config.Config) { c.EmailAuthDisabled = true })
	expectStatus(t, e.browser().post("/auth/signup", signupBody("ada@example.com")), http.StatusBadRequest, CodeEmailPasswordDisabled)
}

// auth:signup allows 30 attempts per client address per hour, counted before any other check.
func TestSignupRateLimit(t *testing.T) {
	e := newEnv(t)
	e.seedUser("admin@example.com", strongPassword, userOpts{})
	e.enableRateLimits()
	e.inOneWindow(time.Hour, func(run int, ip string) error {
		b := e.browser()
		b.ip = ip
		send := func() response { return b.post("/auth/signup", signupBody("x@example.com")) }
		if err := repeat(30, send, http.StatusBadRequest, CodeSignupDisabled); err != nil {
			return err
		}
		r := send()
		if err := statusErr(r, http.StatusTooManyRequests, apperr.CodeTooManyRequests); err != nil {
			return err
		}
		if r.Header().Get("Retry-After") == "" {
			return errors.New("no Retry-After")
		}
		other := e.browser()
		other.ip = fmt.Sprintf("203.0.113.%d", 50+run)
		return statusErr(other.post("/auth/signup", signupBody("x@example.com")), http.StatusBadRequest, CodeSignupDisabled)
	})
}
