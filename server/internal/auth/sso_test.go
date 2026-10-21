package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/oauth2"

	"github.com/yldm-tech/forma/server/internal/platform/audit"
	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/platform/httpapi"
)

// withProvider enables one provider with the fake's client credentials. The provider is pointed at the fake once the env exists (see ssoEnv).
func withProvider(id string) func(*config.Config) {
	return func(cfg *config.Config) {
		switch id {
		case providerGoogle:
			cfg.SSO.GoogleClientID, cfg.SSO.GoogleClientSecret = fakeClientID, fakeClientSecret
		case providerGitHub:
			cfg.SSO.GitHubID, cfg.SSO.GitHubSecret = fakeClientID, fakeClientSecret
		case providerAzureAD:
			cfg.SSO.AzureADClientID, cfg.SSO.AzureADClientSecret = fakeClientID, fakeClientSecret
		case providerOpenID:
			cfg.SSO.OIDCClientID, cfg.SSO.OIDCClientSecret, cfg.SSO.OIDCIssuer = fakeClientID, fakeClientSecret, "https://idp.example.com"
		}
	}
}

// ssoEnv is an env with one provider pointed at a fresh fake identity provider, keeping the provider's identity source.
func ssoEnv(t *testing.T, provider string, configure ...func(*config.Config)) (*env, *fakeIdP) {
	t.Helper()
	e := newEnv(t, append([]func(*config.Config){withProvider(provider)}, configure...)...)
	fake := newFakeIdP(t)
	p := e.sso.providers[provider]
	switch p.source {
	case sourceIDToken:
		p.discoveryURL = fake.URL() + "/.well-known/openid-configuration"
	case sourceMicrosoftGraph:
		p.oauth.Endpoint = oauth2.Endpoint{AuthURL: fake.URL() + "/authorize", TokenURL: fake.URL() + "/token"}
		p.userInfoURL = fake.URL() + "/oidc/userinfo"
	case sourceGitHub:
		p.oauth.Endpoint = oauth2.Endpoint{AuthURL: fake.URL() + "/authorize", TokenURL: fake.URL() + "/token"}
		p.userInfoURL = fake.URL()
	}
	return e, fake
}

// ssoStart begins a sign-in and returns the provider's authorization URL.
func (b *browser) ssoStart(provider, callbackURL string) string {
	b.e.t.Helper()
	r := b.post("/auth/sso/sign-in", map[string]string{"provider": provider, "callbackUrl": callbackURL})
	expectStatus(b.e.t, r, http.StatusOK, "")
	authURL, _ := r.json()["url"].(string)
	return authURL
}

// callback delivers the provider's redirect to the given callback path.
func (b *browser) callback(path string, query url.Values) response {
	b.e.t.Helper()
	return b.request(http.MethodGet, path+"?"+query.Encode(), nil, http.Header{"Accept-Language": {"fr-FR, ja-JP;q=0.9"}})
}

// ssoSignIn runs a whole sign-in: start, consent at the fake, and the callback at the provider's redirect URI. It returns the callback's response.
func (b *browser) ssoSignIn(fake *fakeIdP, provider, callbackURL string) response {
	b.e.t.Helper()
	code, q := fake.authorize(b.ssoStart(provider, callbackURL))
	redirect, err := url.Parse(q.Get("redirect_uri"))
	if err != nil {
		b.e.t.Fatal(err)
	}
	return b.callback(redirect.Path, url.Values{"code": {code}, "state": {q.Get("state")}})
}

// expectRedirect fails unless the response redirects to the URL.
func expectRedirect(t *testing.T, r response, want string) {
	t.Helper()
	if r.Code != http.StatusFound || r.Header().Get("Location") != want {
		t.Fatalf("got %d to %q, want a redirect to %q: %s", r.Code, r.Header().Get("Location"), want, r.Body.String())
	}
}

func loginError(code string) string {
	return webApp + "/auth/login?error=" + code
}

func googleProfile(sub, email string) map[string]any {
	return map[string]any{"sub": sub, "email": email, "email_verified": true, "name": "Ada Lovelace"}
}

type storedUser struct {
	ID            uuid.UUID
	Name          string
	Email         string
	EmailVerified bool
	Locale        string
}

func (e *env) userByEmail(email string) *storedUser {
	e.t.Helper()
	var rows []storedUser
	if err := e.db.Raw(`SELECT id, name, email, email_verified, locale FROM users WHERE lower(email) = lower(?)`, email).Scan(&rows).Error; err != nil {
		e.t.Fatal(err)
	}
	if len(rows) == 0 {
		return nil
	}
	return &rows[0]
}

func TestSSOStart(t *testing.T) {
	t.Parallel()
	e, fake := ssoEnv(t, providerGoogle)
	b := e.browser()

	t.Run("builds an authorization request with state, PKCE and a nonce", func(t *testing.T) {
		r := b.post("/auth/sso/sign-in", map[string]string{"provider": "google", "callbackUrl": "/surveys"})
		expectStatus(t, r, http.StatusOK, "")
		u, _ := url.Parse(r.json()["url"].(string))
		q := u.Query()
		if u.Scheme+"://"+u.Host+u.Path != fake.URL()+"/authorize" || q.Get("client_id") != fakeClientID || q.Get("redirect_uri") != webApp+"/api/auth/callback/google" ||
			q.Get("scope") != "openid email profile" || q.Get("code_challenge_method") != "S256" || len(q.Get("code_challenge")) != 43 ||
			len(q.Get("state")) < 43 || len(q.Get("nonce")) < 43 {
			t.Fatalf("authorization URL %s", u)
		}
		c := r.cookie("forma.oauth_state")
		if c == nil || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Path != "/" || c.MaxAge != 600 || c.Value == q.Get("state") {
			t.Fatalf("state cookie %+v", c)
		}
		// Only hashes are stored; the verifier and nonce never reach the browser.
		if n := e.count(`SELECT count(*) FROM verifications WHERE purpose = 'oauth_state' AND token_hash = sha256(?::bytea)`, c.Value); n != 1 {
			t.Fatalf("%d state rows for the cookie", n)
		}
	})

	t.Run("refuses a provider that is not configured", func(t *testing.T) {
		for _, p := range []string{"github", "saml", "nope"} {
			expectStatus(t, b.post("/auth/sso/sign-in", map[string]string{"provider": p}), http.StatusNotFound, "oauth_provider_not_found")
		}
	})

	t.Run("refuses a callback URL off the app's origin", func(t *testing.T) {
		for _, cb := range []string{"https://evil.example.com/", "//evil.example.com", "javascript:alert(1)", "relative"} {
			expectStatus(t, b.post("/auth/sso/sign-in", map[string]string{"provider": "google", "callbackUrl": cb}), http.StatusBadRequest, "validation_error")
		}
	})

	t.Run("is a CSRF-protected mutation", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/app/auth/sso/sign-in", strings.NewReader(`{"provider":"google"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "https://evil.example.com")
		req.Header.Set(httpapi.CSRFHeader, "XMLHttpRequest")
		rec := httptest.NewRecorder()
		e.engine.ServeHTTP(rec, req)
		expectStatus(t, response{ResponseRecorder: rec, t: t}, http.StatusForbidden, httpapi.CodeCSRF)
	})

	t.Run("options lists the providers", func(t *testing.T) {
		got := b.get("/auth/options").json()["ssoProviders"].([]any)
		if len(got) != 1 || got[0] != "google" {
			t.Fatalf("ssoProviders = %v", got)
		}
	})
}

// forma2's provisioning path: a first sign-in creates the user with the provider's name, the address lowercased, the provider's verdict on it and the browser's locale; no organization unless one is configured.
func TestSSOSignUp(t *testing.T) {
	t.Parallel()
	e, fake := ssoEnv(t, providerGoogle)
	fake.set(func(f *fakeIdP) { f.profile = googleProfile("g-1", "Ada@Example.com") })
	b := e.browser()

	r := b.ssoSignIn(fake, "google", "/surveys?x=1")
	expectRedirect(t, r, webApp+"/surveys?x=1")
	if !b.has("forma.session_token") || b.has("forma.oauth_state") {
		t.Fatalf("cookies after callback: %v", b.cookies)
	}
	u := e.userByEmail("ada@example.com")
	if u == nil || u.Email != "ada@example.com" || u.Name != "Ada Lovelace" || !u.EmailVerified || u.Locale != "ja-JP" {
		t.Fatalf("user %+v", u)
	}
	if n := e.count(`SELECT count(*) FROM user_identities WHERE user_id = ? AND provider = 'google' AND issuer = '' AND subject = 'g-1'`, u.ID); n != 1 {
		t.Fatalf("%d google identities", n)
	}
	if n := e.count(`SELECT count(*) FROM memberships WHERE user_id = ?`, u.ID); n != 0 {
		t.Fatalf("%d memberships, want none", n)
	}
	me := b.get("/auth/me")
	expectStatus(t, me, http.StatusOK, "")
	if me.json()["identityProvider"] != "sso" {
		t.Fatalf("me %s", me.Body.String())
	}
	signedIn := e.auditEvents(audit.ActionSignedIn)
	if len(signedIn) != 1 || field(signedIn[0], "changes", "authMethod") != "sso" {
		t.Fatalf("signedIn events %v", signedIn)
	}
	// The redirect URI is the same on both legs.
	if uris := fake.redirectURIs(); len(uris) != 1 || uris[0] != webApp+"/api/auth/callback/google" {
		t.Fatalf("token requests carried %v", uris)
	}
}

func TestSSOSignUpNames(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, email, want string
	}{
		{"J. Smith 🚀", "x@example.com", "J. Smith"},
		{"😀", "grace.hopper+news@example.com", "grace hopper news"},
		{"", "___@example.com", "User"},
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			e, fake := ssoEnv(t, providerOpenID)
			fake.set(func(f *fakeIdP) { f.profile = map[string]any{"sub": "o-1", "email": tc.email, "name": tc.name} })
			expectRedirect(t, e.browser().ssoSignIn(fake, "openid", ""), webApp+"/")
			if u := e.userByEmail(tc.email); u == nil || u.Name != tc.want {
				t.Fatalf("user %+v, want name %q", u, tc.want)
			}
		})
	}
}

// forma2's better-auth-hooks.test.ts: an account the provider did not vouch for is created unverified, logged and audited, without the address in either.
func TestSSOSignUpUnverified(t *testing.T) {
	t.Parallel()
	e, fake := ssoEnv(t, providerGoogle)
	fake.set(func(f *fakeIdP) {
		f.profile = googleProfile("g-1", "ada@example.com")
		f.profile["email_verified"] = false
	})
	expectRedirect(t, e.browser().ssoSignIn(fake, "google", ""), webApp+"/")
	u := e.userByEmail("ada@example.com")
	if u == nil || u.EmailVerified {
		t.Fatalf("user %+v", u)
	}
	logs := e.logs.String()
	if !strings.Contains(logs, "SSO sign-up created an account the identity provider did not report as verified") || !strings.Contains(logs, `"ssoProvider":"google"`) {
		t.Fatalf("no warning: %s", logs)
	}
	var marker map[string]any
	for _, ev := range e.auditEvents(audit.ActionUpdated) {
		if field(ev, "changes", "ssoUnverifiedSignupMarker") == true {
			marker = ev
		}
	}
	if marker == nil || field(marker, "changes", "provider") != "google" || field(marker, "target", "id") != u.ID.String() {
		t.Fatalf("marker %v", marker)
	}
}

// A returning user is found by provider and subject. The provider's name is synchronized; its address never replaces the account's; an unverified account it vouches for becomes verified.
func TestSSOSignInLinked(t *testing.T) {
	t.Parallel()
	e, fake := ssoEnv(t, providerGoogle)
	fake.set(func(f *fakeIdP) { f.profile = googleProfile("g-1", "ada@example.com") })
	expectRedirect(t, e.browser().ssoSignIn(fake, "google", ""), webApp+"/")
	u := e.userByEmail("ada@example.com")
	e.exec(`UPDATE users SET email_verified = false WHERE id = ?`, u.ID)

	fake.set(func(f *fakeIdP) {
		f.profile = googleProfile("g-1", "ADA@example.com")
		f.profile["name"] = "Ada King (Countess)"
	})
	b := e.browser()
	expectRedirect(t, b.ssoSignIn(fake, "google", ""), webApp+"/")
	got := e.userByEmail("ada@example.com")
	if got.ID != u.ID || got.Name != "Ada King (Countess)" || got.Email != "ada@example.com" || !got.EmailVerified {
		t.Fatalf("user %+v", got)
	}

	t.Run("a different address from the provider leaves the account's", func(t *testing.T) {
		fake.set(func(f *fakeIdP) { f.profile = googleProfile("g-1", "ada@new.example.com") })
		expectRedirect(t, e.browser().ssoSignIn(fake, "google", ""), webApp+"/")
		if e.userByEmail("ada@new.example.com") != nil || e.userByEmail("ada@example.com") == nil {
			t.Fatal("the address changed")
		}
	})

	t.Run("a name that normalizes away keeps the stored one", func(t *testing.T) {
		before := e.userByEmail("ada@example.com").Name
		fake.set(func(f *fakeIdP) {
			f.profile = googleProfile("g-1", "ada@example.com")
			f.profile["name"] = "🦄"
		})
		expectRedirect(t, e.browser().ssoSignIn(fake, "google", ""), webApp+"/")
		if got := e.userByEmail("ada@example.com"); got.Name != before {
			t.Fatalf("name %q, want %q", got.Name, before)
		}
	})

	t.Run("a deactivated user is refused", func(t *testing.T) {
		e.exec(`UPDATE users SET is_active = false WHERE id = ?`, u.ID)
		b := e.browser()
		expectRedirect(t, b.ssoSignIn(fake, "google", ""), loginError("account_inactive"))
		if b.has("forma.session_token") {
			t.Fatal("a session was set")
		}
	})
}

// The state cookie binds the callback to the browser and the request that started it, once.
func TestSSOCallbackState(t *testing.T) {
	t.Parallel()
	e, fake := ssoEnv(t, providerGoogle)
	fake.set(func(f *fakeIdP) { f.profile = googleProfile("g-1", "ada@example.com") })

	t.Run("without the state cookie", func(t *testing.T) {
		b := e.browser()
		code, q := fake.authorize(b.ssoStart("google", ""))
		other := e.browser()
		expectRedirect(t, other.callback("/api/auth/callback/google", url.Values{"code": {code}, "state": {q.Get("state")}}), loginError("please_restart_the_process"))
	})

	t.Run("with another request's state", func(t *testing.T) {
		b := e.browser()
		code, _ := fake.authorize(b.ssoStart("google", ""))
		expectRedirect(t, b.callback("/api/auth/callback/google", url.Values{"code": {code}, "state": {"forged"}}), loginError("state_mismatch"))
	})

	t.Run("replayed", func(t *testing.T) {
		b := e.browser()
		code, q := fake.authorize(b.ssoStart("google", ""))
		jar := map[string]*http.Cookie{}
		for k, v := range b.cookies {
			jar[k] = v
		}
		expectRedirect(t, b.callback("/api/auth/callback/google", url.Values{"code": {code}, "state": {q.Get("state")}}), webApp+"/")
		b.cookies = jar
		expectRedirect(t, b.callback("/api/auth/callback/google", url.Values{"code": {code}, "state": {q.Get("state")}}), loginError("please_restart_the_process"))
	})

	t.Run("expired", func(t *testing.T) {
		b := e.browser()
		code, q := fake.authorize(b.ssoStart("google", ""))
		e.advance(oauthStateTTL + 1)
		defer e.advance(-oauthStateTTL - 1)
		expectRedirect(t, b.callback("/api/auth/callback/google", url.Values{"code": {code}, "state": {q.Get("state")}}), loginError("please_restart_the_process"))
	})

	t.Run("the provider's error is passed on", func(t *testing.T) {
		b := e.browser()
		_, q := fake.authorize(b.ssoStart("google", ""))
		expectRedirect(t, b.callback("/api/auth/callback/google", url.Values{"error": {"access_denied"}, "state": {q.Get("state")}}), loginError("access_denied"))
		b = e.browser()
		_, q = fake.authorize(b.ssoStart("google", ""))
		expectRedirect(t, b.callback("/api/auth/callback/google", url.Values{"error": {"<script>"}, "state": {q.Get("state")}}), loginError("oauth_provider_error"))
	})

	t.Run("without a code", func(t *testing.T) {
		b := e.browser()
		_, q := fake.authorize(b.ssoStart("google", ""))
		expectRedirect(t, b.callback("/api/auth/callback/google", url.Values{"state": {q.Get("state")}}), loginError("no_code"))
	})

	t.Run("a wrong code", func(t *testing.T) {
		b := e.browser()
		_, q := fake.authorize(b.ssoStart("google", ""))
		expectRedirect(t, b.callback("/api/auth/callback/google", url.Values{"code": {"stolen"}, "state": {q.Get("state")}}), loginError("invalid_code"))
	})

	t.Run("RFC 9207 issuer on the response", func(t *testing.T) {
		b := e.browser()
		code, q := fake.authorize(b.ssoStart("google", ""))
		expectRedirect(t, b.callback("/api/auth/callback/google", url.Values{"code": {code}, "state": {q.Get("state")}, "iss": {"https://attacker.example.com"}}), loginError("issuer_mismatch"))
		b = e.browser()
		code, q = fake.authorize(b.ssoStart("google", ""))
		expectRedirect(t, b.callback("/api/auth/callback/google", url.Values{"code": {code}, "state": {q.Get("state")}, "iss": {fake.URL()}}), webApp+"/")
	})

	t.Run("a callback for a provider that is not configured", func(t *testing.T) {
		b := e.browser()
		code, q := fake.authorize(b.ssoStart("google", ""))
		expectRedirect(t, b.callback("/api/auth/callback/github", url.Values{"code": {code}, "state": {q.Get("state")}}), loginError("oauth_provider_not_found"))
	})

	t.Run("the pinned path serves only Azure AD and OIDC", func(t *testing.T) {
		r := e.browser().callback("/api/auth/oauth2/callback/google", url.Values{})
		if r.Code != http.StatusNotFound {
			t.Fatalf("status %d", r.Code)
		}
	})
}

// The id_token is verified (signature, issuer, audience, expiry, nonce); anything it does not establish fails closed and creates nothing.
func TestSSOIDTokenVerification(t *testing.T) {
	t.Parallel()
	forged, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name  string
		setup func(f *fakeIdP)
		want  string
	}{
		{"signed with another key", func(f *fakeIdP) { f.signingKey = forged }, "unable_to_get_user_info"},
		{"from another issuer", func(f *fakeIdP) { f.idTokenClaims = map[string]any{"iss": "https://attacker.example.com"} }, "unable_to_get_user_info"},
		{"for another client", func(f *fakeIdP) { f.idTokenClaims = map[string]any{"aud": "someone-else"} }, "unable_to_get_user_info"},
		{"expired", func(f *fakeIdP) { f.idTokenClaims = map[string]any{"exp": 1} }, "unable_to_get_user_info"},
		{"with another nonce", func(f *fakeIdP) { f.idTokenClaims = map[string]any{"nonce": "replayed"} }, "unable_to_get_user_info"},
		{"without a nonce", func(f *fakeIdP) { f.idTokenClaims = map[string]any{"nonce": nil} }, "unable_to_get_user_info"},
		{"without a subject", func(f *fakeIdP) { f.profile = map[string]any{"email": "ada@example.com", "email_verified": true} }, "unable_to_get_user_info"},
		{"without an address", func(f *fakeIdP) { f.profile = map[string]any{"sub": "g-1", "email_verified": true} }, "email_not_found"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e, fake := ssoEnv(t, providerGoogle)
			fake.set(func(f *fakeIdP) {
				f.profile = googleProfile("g-1", "ada@example.com")
				tc.setup(f)
			})
			// The fake's authorize copies the id_token overrides into the grant, so they are set before it runs.
			b := e.browser()
			expectRedirect(t, b.ssoSignIn(fake, "google", ""), loginError(tc.want))
			if b.has("forma.session_token") || e.count(`SELECT count(*) FROM users`) != 0 {
				t.Fatal("a sign-in went through")
			}
		})
	}
}

// forma2's better-auth-oidc-email-verified.test.ts: the generic provider takes the raw email_verified claim, and falls back to userinfo when the id_token lacks the address.
func TestSSOGenericOIDC(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name         string
		profile      map[string]any
		idToken      map[string]any
		wantVerified bool
		wantName     string
	}{
		{"absent claim is verified", map[string]any{"sub": "o-1", "email": "ada@example.com", "name": "Ada"}, nil, true, "Ada"},
		{"string denial", map[string]any{"sub": "o-1", "email": "ada@example.com", "email_verified": "false"}, nil, false, "ada"},
		{"zero is a denial", map[string]any{"sub": "o-1", "email": "ada@example.com", "email_verified": 0}, nil, false, "ada"},
		{"userinfo when the id_token has no address", map[string]any{"sub": "o-1", "email": "ada@example.com", "given_name": "Ada", "family_name": "Lovelace"}, map[string]any{"email": nil}, true, "Ada Lovelace"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e, fake := ssoEnv(t, providerOpenID)
			fake.set(func(f *fakeIdP) {
				f.profile = tc.profile
				f.idTokenClaims = tc.idToken
			})
			expectRedirect(t, e.browser().ssoSignIn(fake, "openid", ""), webApp+"/")
			u := e.userByEmail("ada@example.com")
			if u == nil || u.EmailVerified != tc.wantVerified || u.Name != tc.wantName {
				t.Fatalf("user %+v", u)
			}
		})
	}

	t.Run("userinfo must name the id_token's subject", func(t *testing.T) {
		t.Parallel()
		e, fake := ssoEnv(t, providerOpenID)
		fake.set(func(f *fakeIdP) {
			f.profile = map[string]any{"sub": "o-1", "email": "ada@example.com"}
			f.idTokenClaims = map[string]any{"email": nil, "sub": "o-2"}
		})
		expectRedirect(t, e.browser().ssoSignIn(fake, "openid", ""), loginError("unable_to_get_user_info"))
	})

	t.Run("without an id_token identity comes from userinfo", func(t *testing.T) {
		t.Parallel()
		e, fake := ssoEnv(t, providerOpenID)
		fake.set(func(f *fakeIdP) {
			f.profile = map[string]any{"sub": "o-1", "email": "ada@example.com"}
			f.omitIDToken = true
		})
		expectRedirect(t, e.browser().ssoSignIn(fake, "openid", ""), webApp+"/")
	})

	t.Run("discovery failure", func(t *testing.T) {
		t.Parallel()
		e, _ := ssoEnv(t, providerOpenID)
		e.sso.providers[providerOpenID].discoveryURL = "http://127.0.0.1:1/.well-known/openid-configuration"
		r := e.browser().post("/auth/sso/sign-in", map[string]string{"provider": "openid"})
		expectStatus(t, r, http.StatusInternalServerError, "internal_server_error")
	})
}

// forma2's ENG-2343/ENG-2750 cases: a concrete tenant verifies the id_token against the issuer its discovery document names, which may differ from the URL it was read from; the placeholder authorities take identity from Graph only.
func TestSSOAzureAD(t *testing.T) {
	t.Parallel()
	t.Run("a concrete tenant verifies the id_token", func(t *testing.T) {
		t.Parallel()
		e, fake := ssoEnv(t, providerAzureAD, func(cfg *config.Config) { cfg.SSO.AzureADTenantID = "contoso.onmicrosoft.com" })
		e.sso.providers[providerAzureAD].discoveryURL = fake.URL() + "/contoso.onmicrosoft.com/v2.0/.well-known/openid-configuration"
		fake.set(func(f *fakeIdP) {
			f.issuer = fake.URL() + "/9188040d-tenant/v2.0"
			f.profile = map[string]any{"sub": "a-1", "email": "ada@contoso.com", "name": "Ada", "xms_edov": false}
		})
		b := e.browser()
		code, q := fake.authorize(b.ssoStart("azuread", ""))
		if q.Get("redirect_uri") != webApp+"/api/auth/oauth2/callback/azuread" {
			t.Fatalf("redirect_uri %q", q.Get("redirect_uri"))
		}
		expectRedirect(t, b.callback("/api/auth/oauth2/callback/azuread", url.Values{"code": {code}, "state": {q.Get("state")}}), webApp+"/")
		if u := e.userByEmail("ada@contoso.com"); u == nil || u.EmailVerified {
			t.Fatalf("user %+v, want unverified from xms_edov", u)
		}
	})

	t.Run("the multi-tenant authority reads Graph and never the id_token", func(t *testing.T) {
		forged, _ := rsa.GenerateKey(rand.Reader, 2048)
		e, fake := ssoEnv(t, providerAzureAD)
		fake.set(func(f *fakeIdP) {
			f.profile = map[string]any{"sub": "graph-sub", "email": "ada@contoso.com", "given_name": "Ada", "family_name": "Lovelace"}
			// An unverifiable id_token naming someone else: it must not matter.
			f.signingKey = forged
			f.idTokenClaims = map[string]any{"sub": "token-sub", "email": "mallory@contoso.com"}
		})
		b := e.browser()
		expectRedirect(t, b.ssoSignIn(fake, "azuread", ""), webApp+"/")
		u := e.userByEmail("ada@contoso.com")
		if u == nil || !u.EmailVerified || u.Name != "Ada Lovelace" || e.userByEmail("mallory@contoso.com") != nil {
			t.Fatalf("user %+v", u)
		}
		if n := e.count(`SELECT count(*) FROM user_identities WHERE provider = 'azuread' AND subject = 'graph-sub'`); n != 1 {
			t.Fatalf("%d graph identities", n)
		}
	})

	for _, tc := range []struct {
		name  string
		setup func(f *fakeIdP)
	}{
		{"Graph refuses", func(f *fakeIdP) { f.userInfoStatus = http.StatusServiceUnavailable }},
		{"Graph names no subject", func(f *fakeIdP) { f.profile = map[string]any{"email": "ada@contoso.com"} }},
	} {
		t.Run(tc.name+" fails closed", func(t *testing.T) {
			t.Parallel()
			e, fake := ssoEnv(t, providerAzureAD)
			fake.set(func(f *fakeIdP) {
				f.profile = map[string]any{"sub": "graph-sub", "email": "ada@contoso.com"}
				tc.setup(f)
			})
			expectRedirect(t, e.browser().ssoSignIn(fake, "azuread", ""), loginError("unable_to_get_user_info"))
			if e.count(`SELECT count(*) FROM users`) != 0 {
				t.Fatal("a user was created")
			}
		})
	}

	t.Run("the current callback path serves Azure AD too", func(t *testing.T) {
		t.Parallel()
		e, fake := ssoEnv(t, providerAzureAD)
		fake.set(func(f *fakeIdP) { f.profile = map[string]any{"sub": "graph-sub", "email": "ada@contoso.com"} })
		b := e.browser()
		code, q := fake.authorize(b.ssoStart("azure-ad", ""))
		expectRedirect(t, b.callback("/api/auth/callback/azuread", url.Values{"code": {code}, "state": {q.Get("state")}}), webApp+"/")
	})
}

// Better Auth's GitHub provider: the numeric id is the subject, the login stands in for a missing name, and /user/emails supplies a missing address and whether it is verified.
func TestSSOGitHub(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name         string
		profile      map[string]any
		emails       []map[string]any
		emailsStatus int
		wantEmail    string
		wantName     string
		wantVerified bool
	}{
		{"public address listed as verified", map[string]any{"id": 583231, "login": "octocat", "name": "The Octocat", "email": "octo@example.com"},
			[]map[string]any{{"email": "octo@example.com", "primary": true, "verified": true}}, 0, "octo@example.com", "The Octocat", true},
		{"no public address takes the primary", map[string]any{"id": 583231, "login": "octocat", "email": nil},
			[]map[string]any{{"email": "old@example.com", "primary": false, "verified": true}, {"email": "octo@example.com", "primary": true, "verified": false}}, 0, "octo@example.com", "octocat", false},
		{"a failed lookup leaves the address unverified", map[string]any{"id": 583231, "login": "octocat", "email": "octo@example.com"},
			nil, http.StatusForbidden, "octo@example.com", "octocat", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e, fake := ssoEnv(t, providerGitHub)
			fake.set(func(f *fakeIdP) {
				f.profile = tc.profile
				f.githubEmails = tc.emails
				f.githubEmailsStatus = tc.emailsStatus
				f.omitIDToken = true
			})
			b := e.browser()
			code, q := fake.authorize(b.ssoStart("github", ""))
			if q.Get("scope") != "read:user user:email" || q.Has("nonce") || q.Get("redirect_uri") != webApp+"/api/auth/callback/github" {
				t.Fatalf("authorization request %v", q)
			}
			expectRedirect(t, b.callback("/api/auth/callback/github", url.Values{"code": {code}, "state": {q.Get("state")}}), webApp+"/")
			u := e.userByEmail(tc.wantEmail)
			if u == nil || u.Name != tc.wantName || u.EmailVerified != tc.wantVerified {
				t.Fatalf("user %+v", u)
			}
			if n := e.count(`SELECT count(*) FROM user_identities WHERE provider = 'github' AND subject = '583231'`); n != 1 {
				t.Fatalf("%d identities with the numeric id", n)
			}
		})
	}

	t.Run("no address anywhere", func(t *testing.T) {
		t.Parallel()
		e, fake := ssoEnv(t, providerGitHub)
		fake.set(func(f *fakeIdP) { f.profile = map[string]any{"id": 1, "login": "ghost"} })
		expectRedirect(t, e.browser().ssoSignIn(fake, "github", ""), loginError("email_not_found"))
	})
}

// forma2's sso-provisioning.test.ts: Forma Cloud refuses personal mail domains before creating anything.
func TestSSOCloudDomainBlock(t *testing.T) {
	t.Parallel()
	e, fake := ssoEnv(t, providerGoogle, func(cfg *config.Config) { cfg.IsFormaCloud = true })
	fake.set(func(f *fakeIdP) { f.profile = googleProfile("g-1", "ada@Gmail.com.") })
	b := e.browser()
	expectRedirect(t, b.ssoSignIn(fake, "google", ""), webApp+"/auth/signup?error=email_domain_not_allowed")
	if b.has("forma.session_token") || e.count(`SELECT count(*) FROM users`) != 0 {
		t.Fatal("the sign-up went through")
	}
	fake.set(func(f *fakeIdP) { f.profile = googleProfile("g-2", "ada@company.example") })
	expectRedirect(t, e.browser().ssoSignIn(fake, "google", ""), webApp+"/")
}

// forma2's default-organization.test.ts: the sign-up that creates AUTH_SSO_DEFAULT_ORGANIZATION_ID's organization owns it; later ones get the configured role, manager by default.
func TestSSODefaultOrganization(t *testing.T) {
	t.Parallel()
	orgID := uuid.New()
	cases := []struct {
		name       string
		role       string
		preexists  bool
		wantFirst  string
		wantSecond string
	}{
		{"created by the first sign-up", "", false, "owner", "manager"},
		{"created, with a configured role", "member", false, "owner", "member"},
		{"existing", "billing", true, "billing", "billing"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e, fake := ssoEnv(t, providerGoogle, func(cfg *config.Config) {
				cfg.SSO.DefaultOrganizationID = orgID.String()
				cfg.SSO.DefaultOrganizationRole = tc.role
			})
			if tc.preexists {
				e.exec(`INSERT INTO organizations (id, name) VALUES (?, 'Acme')`, orgID)
			}
			for i, want := range []string{tc.wantFirst, tc.wantSecond} {
				email := []string{"ada@acme.example", "grace@acme.example"}[i]
				fake.set(func(f *fakeIdP) { f.profile = googleProfile("g-"+email, email) })
				expectRedirect(t, e.browser().ssoSignIn(fake, "google", ""), webApp+"/")
				u := e.userByEmail(email)
				var role string
				if err := e.db.Raw(`SELECT role FROM memberships WHERE user_id = ? AND organization_id = ? AND accepted`, u.ID, orgID).Scan(&role).Error; err != nil || role != want {
					t.Fatalf("%s: role %q, %v; want %q", email, role, err, want)
				}
				if n := e.count(`SELECT count(*) FROM users WHERE id = ? AND notification_settings -> 'unsubscribedOrganizationIds' = jsonb_build_array(?::text)`, u.ID, orgID.String()); n != 1 {
					t.Fatalf("%s: alerts not switched off once", email)
				}
			}
			var name string
			_ = e.db.Raw(`SELECT name FROM organizations WHERE id = ?`, orgID).Scan(&name).Error
			if !tc.preexists && name != "Ada Lovelace's Organization" {
				t.Fatalf("organization name %q", name)
			}
			if !tc.preexists && e.count(`SELECT count(*) FROM workspaces WHERE organization_id = ?`, orgID) != 1 {
				t.Fatal("the created organization has no workspace")
			}
		})
	}
}

// recoveryLink extracts the SSO recovery token mailed to an address.
func (e *env) recoveryToken(to string) string {
	e.t.Helper()
	msgs := e.sent(to)
	if len(msgs) == 0 {
		e.t.Fatalf("no mail to %s", to)
	}
	html := msgs[len(msgs)-1].HTML
	if !strings.Contains(html, webApp+ssoRecoveryPath+"?token=") {
		e.t.Fatalf("no recovery link in %q", msgs[len(msgs)-1].Subject)
	}
	return e.lastToken(to, subjectVerification)
}

// forma2's sso-recovery.test.ts and its security notes: an identity provider asserting a registered address links nothing until the mailbox owner uses the emailed link, which the starting browser never sees.
func TestSSORecovery(t *testing.T) {
	t.Parallel()
	e, fake := ssoEnv(t, providerGoogle)
	id := e.seedUser("ada@example.com", strongPassword, userOpts{})
	fake.set(func(f *fakeIdP) { f.profile = googleProfile("g-1", "ADA@example.com") })

	starter := e.browser()
	r := starter.ssoSignIn(fake, "google", "/surveys")
	expectRedirect(t, r, webApp+"/auth/verification-requested?purpose=sso_recovery")
	if starter.has("forma.session_token") || e.count(`SELECT count(*) FROM user_identities WHERE provider = 'google'`) != 0 {
		t.Fatal("the identity was linked or a session granted before the mailbox was proven")
	}
	token := e.recoveryToken("ada@example.com")
	if strings.Contains(r.Header().Get("Location"), token) {
		t.Fatal("the recovery token reached the browser that started the flow")
	}
	if ev := e.auditEvents(audit.ActionSSORecoveryStarted); len(ev) != 1 || field(ev[0], "target", "id") != id.String() || field(ev[0], "changes", "provider") != "google" {
		t.Fatalf("started events %v", ev)
	}

	owner := e.browser()
	owner.signIn("ada@example.com", strongPassword)
	done := owner.post("/auth/sso/recovery/complete", map[string]string{"token": token})
	expectStatus(t, done, http.StatusOK, "")
	if done.json()["redirectUrl"] != webApp+"/surveys" || done.cookie("forma.session_token") == nil {
		t.Fatalf("completion %s", done.Body.String())
	}
	if n := e.count(`SELECT count(*) FROM user_identities WHERE user_id = ? AND provider = 'google' AND subject = 'g-1'`, id); n != 1 {
		t.Fatal("the identity was not linked")
	}
	// A verified account keeps its password and its other sessions.
	expectStatus(t, owner.post("/test/protected", nil), http.StatusOK, "")
	e.browser().signIn("ada@example.com", strongPassword)
	if len(e.sent("ada@example.com")) != 1 {
		t.Fatal("a factors-removed mail was sent for a verified account")
	}
	completed := e.auditEvents(audit.ActionSSORecoveryCompleted)
	if len(completed) != 1 || field(completed[0], "changes", "credentialPasswordsCleared") != nil {
		t.Fatalf("completed events %v", completed)
	}

	t.Run("the link works once", func(t *testing.T) {
		expectStatus(t, e.browser().post("/auth/sso/recovery/complete", map[string]string{"token": token}), http.StatusBadRequest, "invalid_token")
		failed := e.auditEvents(audit.ActionSSORecoveryFailed)
		if len(failed) == 0 || field(failed[len(failed)-1], "changes", "failureReason") != "invalid_or_expired_intent" {
			t.Fatalf("failed events %v", failed)
		}
	})

	t.Run("the provider now signs the account in directly", func(t *testing.T) {
		b := e.browser()
		expectRedirect(t, b.ssoSignIn(fake, "google", ""), webApp+"/")
		me := b.get("/auth/me")
		if me.json()["id"] != id.String() {
			t.Fatalf("me %s", me.Body.String())
		}
	})
}

// forma2's ENG-554/ENG-2557: linking an account whose address was never proven strips what its registrant set (the password, two-factor authentication) and signs everyone else out, and the owner is told.
func TestSSORecoveryReclaimsUnprovenAccount(t *testing.T) {
	t.Parallel()
	e, fake := ssoEnv(t, providerGoogle, func(cfg *config.Config) { cfg.EmailVerificationDisabled = true })
	id := e.seedUser("ada@example.com", strongPassword, userOpts{unverified: true, twoFactor: true})
	// Whoever registered the address holds a session.
	held, err := e.sessions.create(context.Background(), e.db, id, clientMeta{})
	if err != nil {
		t.Fatal(err)
	}
	squatter := e.browser()
	squatter.cookies["forma.session_token"] = &http.Cookie{Name: "forma.session_token", Value: held.Value}
	expectStatus(t, squatter.post("/test/protected", nil), http.StatusOK, "")
	e.insertID(`INSERT INTO verifications (purpose, user_id, email, token_hash, expires_at) VALUES ('email_change', ?, 'squatter@evil.example', '\x01', now() + interval '1 day') RETURNING id`, id)
	e.exec(`INSERT INTO sessions (user_id, token_hash, expires_at) VALUES (?, '\x02', now() + interval '1 day')`, id)

	fake.set(func(f *fakeIdP) { f.profile = googleProfile("g-1", "ada@example.com") })
	expectRedirect(t, e.browser().ssoSignIn(fake, "google", ""), webApp+"/auth/verification-requested?purpose=sso_recovery")
	owner := e.browser()
	done := owner.post("/auth/sso/recovery/complete", map[string]string{"token": e.recoveryToken("ada@example.com")})
	expectStatus(t, done, http.StatusOK, "")
	if done.json()["redirectUrl"] != webApp+"/" {
		t.Fatalf("redirect %v", done.json()["redirectUrl"])
	}

	u := e.userByEmail("ada@example.com")
	if !u.EmailVerified {
		t.Fatal("the address was not marked verified")
	}
	if n := e.count(`SELECT count(*) FROM user_identities WHERE user_id = ? AND provider = 'credential' AND password_hash IS NULL`, id); n != 1 {
		t.Fatal("the credential identity was not kept without its password")
	}
	if e.count(`SELECT count(*) FROM user_totp WHERE user_id = ?`, id) != 0 || e.count(`SELECT count(*) FROM verifications WHERE user_id = ? AND purpose = 'email_change'`, id) != 0 {
		t.Fatal("two-factor authentication or an email change link survived")
	}
	if e.count(`SELECT count(*) FROM sessions WHERE user_id = ?`, id) != 1 {
		t.Fatal("other sessions survived")
	}
	expectStatus(t, squatter.post("/test/protected", nil), http.StatusUnauthorized, "not_authenticated")
	expectStatus(t, owner.post("/test/protected", nil), http.StatusOK, "")
	expectStatus(t, e.browser().post("/auth/sign-in", map[string]string{"email": "ada@example.com", "password": strongPassword}), http.StatusUnauthorized, "invalid_email_or_password")

	mails := e.sent("ada@example.com")
	notice := mails[len(mails)-1]
	if notice.Subject != "Sign-in factors were removed from your Forma account" || !strings.Contains(notice.HTML, "Your password was removed.") || !strings.Contains(notice.HTML, "Two-factor authentication was removed.") {
		t.Fatalf("notice %q", notice.Subject)
	}
	completed := e.auditEvents(audit.ActionSSORecoveryCompleted)
	if len(completed) != 1 || field(completed[0], "changes", "credentialPasswordsCleared") != float64(1) ||
		field(completed[0], "changes", "twoFactorRowsRemoved") != float64(1) || field(completed[0], "changes", "sessionsRevoked") != float64(2) {
		t.Fatalf("completed %v", completed)
	}

	t.Run("the owner can set a password again through a reset", func(t *testing.T) {
		expectStatus(t, e.browser().post("/auth/password/forgot", map[string]string{"email": "ada@example.com"}), http.StatusOK, "")
		token := e.lastToken("ada@example.com", subjectReset)
		expectStatus(t, e.browser().post("/auth/password/reset", map[string]string{"token": token, "password": "New-Password-1"}), http.StatusOK, "")
		e.browser().signIn("ada@example.com", "New-Password-1")
	})
}

func TestSSORecoveryRefusals(t *testing.T) {
	t.Parallel()
	start := func(t *testing.T) (*env, uuid.UUID, string) {
		e, fake := ssoEnv(t, providerGoogle)
		id := e.seedUser("ada@example.com", strongPassword, userOpts{})
		fake.set(func(f *fakeIdP) { f.profile = googleProfile("g-1", "ada@example.com") })
		expectRedirect(t, e.browser().ssoSignIn(fake, "google", ""), webApp+"/auth/verification-requested?purpose=sso_recovery")
		return e, id, e.recoveryToken("ada@example.com")
	}
	lastFailure := func(e *env) any {
		failed := e.auditEvents(audit.ActionSSORecoveryFailed)
		if len(failed) == 0 {
			return nil
		}
		return field(failed[len(failed)-1], "changes", "failureReason")
	}

	t.Run("expired", func(t *testing.T) {
		t.Parallel()
		e, _, token := start(t)
		e.advance(ssoRecoveryTTL + 1)
		expectStatus(t, e.browser().post("/auth/sso/recovery/complete", map[string]string{"token": token}), http.StatusBadRequest, "token_expired")
	})

	t.Run("the account moved to another address", func(t *testing.T) {
		t.Parallel()
		e, id, token := start(t)
		e.exec(`UPDATE users SET email = 'ada@new.example' WHERE id = ?`, id)
		expectStatus(t, e.browser().post("/auth/sso/recovery/complete", map[string]string{"token": token}), http.StatusBadRequest, "account_not_linked")
		if lastFailure(e) != "user_mismatch" {
			t.Fatalf("failure reason %v", lastFailure(e))
		}
	})

	t.Run("the identity was linked to someone else meanwhile", func(t *testing.T) {
		t.Parallel()
		e, _, token := start(t)
		other := e.seedUser("grace@example.com", strongPassword, userOpts{})
		e.exec(`INSERT INTO user_identities (user_id, provider, subject) VALUES (?, 'google', 'g-1')`, other)
		b := e.browser()
		expectStatus(t, b.post("/auth/sso/recovery/complete", map[string]string{"token": token}), http.StatusBadRequest, "account_not_linked")
		if b.has("forma.session_token") || lastFailure(e) != "identity_linked_to_another_user" {
			t.Fatalf("failure reason %v", lastFailure(e))
		}
	})

	t.Run("deactivated", func(t *testing.T) {
		t.Parallel()
		e, id, token := start(t)
		e.exec(`UPDATE users SET is_active = false WHERE id = ?`, id)
		expectStatus(t, e.browser().post("/auth/sso/recovery/complete", map[string]string{"token": token}), http.StatusForbidden, "account_inactive")
	})

	t.Run("an unknown token", func(t *testing.T) {
		t.Parallel()
		e, _, _ := start(t)
		expectStatus(t, e.browser().post("/auth/sso/recovery/complete", map[string]string{"token": "nope"}), http.StatusBadRequest, "invalid_token")
	})

	t.Run("without SMTP nothing pretends a mail went out", func(t *testing.T) {
		t.Parallel()
		e, fake := ssoEnv(t, providerGoogle, func(cfg *config.Config) { cfg.SMTPHost = "" })
		e.seedUser("ada@example.com", strongPassword, userOpts{})
		fake.set(func(f *fakeIdP) { f.profile = googleProfile("g-1", "ada@example.com") })
		expectRedirect(t, e.browser().ssoSignIn(fake, "google", ""), loginError("account_not_linked"))
		if lastFailure(e) != "email_not_sent" {
			t.Fatalf("failure reason %v", lastFailure(e))
		}
	})

	t.Run("rate limited", func(t *testing.T) {
		t.Parallel()
		e, _, _ := start(t)
		e.enableRateLimits()
		e.inOneWindow(time.Minute, func(run int, ip string) error {
			b := e.browser()
			b.ip = ip
			send := func() response {
				return b.post("/auth/sso/recovery/complete", map[string]string{"token": "nope"})
			}
			if err := repeat(5, send, http.StatusBadRequest, "invalid_token"); err != nil {
				return err
			}
			return statusErr(send(), http.StatusTooManyRequests, "too_many_requests")
		})
	})
}

// The state payload never carries anything the browser could use: it is stored server-side and named by the cookie's hash.
func TestSSOStatePayload(t *testing.T) {
	t.Parallel()
	e, _ := ssoEnv(t, providerGoogle)
	b := e.browser()
	b.ssoStart("google", "/x")
	var raw []byte
	if err := e.db.Raw(`SELECT payload FROM verifications WHERE purpose = 'oauth_state'`).Row().Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var st oauthState
	if err := json.Unmarshal(raw, &st); err != nil || st.Provider != "google" || st.CallbackURL != webApp+"/x" || st.CodeVerifier == "" || st.Nonce == "" {
		t.Fatalf("payload %s: %v", raw, err)
	}
}
