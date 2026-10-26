package auth

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/url"
	"strings"
	"testing"

	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
)

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// forma2's provider-normalization.test.ts.
func TestNormalizeProvider(t *testing.T) {
	cases := map[string]string{
		"google": "google", "GitHub": "github", "azuread": "azuread", "azure-ad": "azuread", "AZURE-AD": "azuread", "openid": "openid", "saml": "saml", "SAML": "saml",
		"email": "", "credential": "", "": "", "toString": "", "__proto__": "",
	}
	for in, want := range cases {
		if got := normalizeProvider(in); got != want {
			t.Errorf("normalizeProvider(%q) = %q, want %q", in, got, want)
		}
	}
}

// forma2's email-verification-policy.test.ts: only an explicit denial is unverified.
func TestEmailVerifiedFromRawClaim(t *testing.T) {
	cases := []struct {
		raw  any
		want bool
	}{
		{nil, true}, {true, true}, {false, false}, {json.Number("0"), false}, {json.Number("0.0"), false}, {json.Number("1"), true},
		{float64(0), false}, {float64(2), true}, {"false", false}, {" FALSE ", false}, {"0", false}, {" 0 ", false},
		{"true", true}, {"", true}, {"no", true}, {map[string]any{}, true},
	}
	for _, tc := range cases {
		if got := emailVerifiedFromRawClaim(tc.raw); got != tc.want {
			t.Errorf("emailVerifiedFromRawClaim(%#v) = %v, want %v", tc.raw, got, tc.want)
		}
	}
}

func TestIdentityFromClaims(t *testing.T) {
	cases := []struct {
		name     string
		provider string
		claims   map[string]any
		want     ssoIdentity
		wantErr  bool
	}{
		{"google takes only an explicit true", providerGoogle, map[string]any{"sub": "g1", "email": "a@x.com", "name": "Ada", "email_verified": "true"}, ssoIdentity{Subject: "g1", Email: "a@x.com", Name: "Ada"}, false},
		{"google verified", providerGoogle, map[string]any{"sub": "g1", "email": "a@x.com", "email_verified": true}, ssoIdentity{Subject: "g1", Email: "a@x.com", EmailVerified: true}, false},
		{"google without the claim is unverified", providerGoogle, map[string]any{"sub": "g1", "email": "a@x.com"}, ssoIdentity{Subject: "g1", Email: "a@x.com"}, false},
		{"oidc without the claim is verified", providerOpenID, map[string]any{"sub": "o1", "email": "a@x.com"}, ssoIdentity{Subject: "o1", Email: "a@x.com", EmailVerified: true}, false},
		{"oidc denial", providerOpenID, map[string]any{"sub": "o1", "email": "a@x.com", "email_verified": "False"}, ssoIdentity{Subject: "o1", Email: "a@x.com"}, false},
		{"oidc name from given and family", providerOpenID, map[string]any{"sub": "o1", "given_name": "Ada", "family_name": "Lovelace", "preferred_username": "ada"}, ssoIdentity{Subject: "o1", Name: "Ada Lovelace", EmailVerified: true}, false},
		{"oidc name from preferred_username", providerOpenID, map[string]any{"sub": "o1", "preferred_username": "ada"}, ssoIdentity{Subject: "o1", Name: "ada", EmailVerified: true}, false},
		{"azure falls back to xms_edov", providerAzureAD, map[string]any{"sub": "a1", "email": "a@x.com", "xms_edov": false}, ssoIdentity{Subject: "a1", Email: "a@x.com"}, false},
		{"azure email_verified wins over xms_edov", providerAzureAD, map[string]any{"sub": "a1", "email_verified": true, "xms_edov": false}, ssoIdentity{Subject: "a1", EmailVerified: true}, false},
		{"azure without either is verified", providerAzureAD, map[string]any{"sub": "a1"}, ssoIdentity{Subject: "a1", EmailVerified: true}, false},
		{"a numeric subject keeps its digits", providerOpenID, map[string]any{"sub": json.Number("12345678901234567890")}, ssoIdentity{Subject: "12345678901234567890", EmailVerified: true}, false},
		{"a missing subject fails", providerOpenID, map[string]any{"email": "a@x.com"}, ssoIdentity{}, true},
		{"an empty subject fails", providerOpenID, map[string]any{"sub": "", "email": "a@x.com"}, ssoIdentity{}, true},
		{"a null subject fails", providerOpenID, map[string]any{"sub": nil}, ssoIdentity{}, true},
		{"a non-string email is missing", providerOpenID, map[string]any{"sub": "o1", "email": 7}, ssoIdentity{Subject: "o1", EmailVerified: true}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := identityFromClaims(tc.provider, tc.claims)
			if (err != nil) != tc.wantErr || got != tc.want {
				t.Fatalf("got %+v, %v; want %+v, error %v", got, err, tc.want, tc.wantErr)
			}
		})
	}
}

// forma2's normalizeUserName cases (packages/types/user.test.ts, better-auth-hooks.test.ts).
func TestNormalizeUserName(t *testing.T) {
	cases := map[string]string{
		"Ada Lovelace":        "Ada Lovelace",
		"  J.  Smith  ":       "J. Smith",
		"Smith & Co":          "Smith & Co",
		"Ada 😀 Lovelace":      "Ada Lovelace",
		"😀😀":                  "",
		"O'Brien-Smith (Jr.)": "O'Brien-Smith (Jr.)",
		"José Müller":         "José Müller",
		"山田 太郎":               "山田 太郎",
		"a\tb\nc":             "a b c",
		"<script>":            "script",
		"user@example.com":    "user example.com",
		"١٢٣":                 "",
	}
	for in, want := range cases {
		got := normalizeUserName(in)
		if got != want {
			t.Errorf("normalizeUserName(%q) = %q, want %q", in, got, want)
		}
		if got != "" {
			var errs fieldErrors
			checkName(&errs, got)
			if errs.err() != nil {
				t.Errorf("normalizeUserName(%q) = %q, which ZUserName rejects", in, got)
			}
		}
	}
}

// forma2's url.test.ts (getValidatedCallbackUrl).
func TestValidatedCallbackURL(t *testing.T) {
	app := mustURL(t, "https://app.example.com")
	cases := map[string]string{
		"":                                    "",
		"/surveys?x=1":                        "https://app.example.com/surveys?x=1",
		"https://app.example.com/a":           "https://app.example.com/a",
		"https://APP.example.com:443/a":       "https://APP.example.com:443/a",
		"https://evil.example.com/a":          "",
		"http://app.example.com/a":            "",
		"//evil.example.com/a":                "",
		"https://user:pw@app.example.com/a":   "",
		"https://app.example.com//evil.com":   "",
		"javascript:alert(1)":                 "",
		"surveys":                             "",
		"ftp://app.example.com/a":             "",
		"https://app.example.com.evil.com/a":  "",
		"https://app.example.com/a#fragment":  "https://app.example.com/a#fragment",
		"/auth/signup?token=abc&source=login": "https://app.example.com/auth/signup?token=abc&source=login",
	}
	for in, want := range cases {
		if got := validatedCallbackURL(app, in); got != want {
			t.Errorf("validatedCallbackURL(%q) = %q, want %q", in, got, want)
		}
	}
}

// forma2's oauth-urls.test.ts and better-auth-providers.test.ts: the redirect URIs registered at the providers.
func TestRedirectURIs(t *testing.T) {
	cases := []struct {
		name         string
		webApp, auth string
		base         string
	}{
		{"WEBAPP_URL", "https://app.example.com", "", "https://app.example.com/api/auth"},
		{"trailing slash", "https://app.example.com/", "", "https://app.example.com/api/auth"},
		{"AUTH_URL first", "https://app.example.com", "https://auth.example.com", "https://auth.example.com/api/auth"},
		{"already ends in /api/auth", "https://app.example.com", "https://app.example.com/api/auth/", "https://app.example.com/api/auth"},
		{"subpath", "https://example.com/forma", "", "https://example.com/forma/api/auth"},
		{"origin normalized", "https://APP.example.com:443?x=1#y", "", "https://app.example.com/api/auth"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.Config{WebAppURL: mustURL(t, tc.webApp)}
			if tc.auth != "" {
				cfg.SSO.AuthURL = mustURL(t, tc.auth)
			}
			cfg.SSO = enableAllSSO(cfg.SSO)
			providers := newSSOProviders(cfg, slog.New(slog.DiscardHandler))
			want := map[string]string{
				providerGoogle:  tc.base + "/callback/google",
				providerGitHub:  tc.base + "/callback/github",
				providerAzureAD: tc.base + "/oauth2/callback/azuread",
				providerOpenID:  tc.base + "/oauth2/callback/openid",
			}
			for id, uri := range want {
				if got := providers[id].oauth.RedirectURL; got != uri {
					t.Errorf("%s redirect URI = %q, want %q", id, got, uri)
				}
			}
		})
	}
}

func enableAllSSO(s config.SSOConfig) config.SSOConfig {
	s.GoogleClientID, s.GoogleClientSecret = fakeClientID, fakeClientSecret
	s.GitHubID, s.GitHubSecret = fakeClientID, fakeClientSecret
	s.AzureADClientID, s.AzureADClientSecret = fakeClientID, fakeClientSecret
	s.OIDCClientID, s.OIDCClientSecret, s.OIDCIssuer = fakeClientID, fakeClientSecret, "https://idp.example.com"
	return s
}

// A provider is offered exactly when its own credentials are set (forma2's *_OAUTH_ENABLED).
func TestProvidersNeedTheirCredentials(t *testing.T) {
	cases := []struct {
		name string
		sso  config.SSOConfig
		want []string
	}{
		{"none", config.SSOConfig{}, []string{}},
		{"all", enableAllSSO(config.SSOConfig{}), []string{"google", "github", "azuread", "openid"}},
		{"google without a secret", config.SSOConfig{GoogleClientID: "id"}, []string{}},
		{"github without an id", config.SSOConfig{GitHubSecret: "s"}, []string{}},
		{"azure without a tenant", config.SSOConfig{AzureADClientID: "id", AzureADClientSecret: "s"}, []string{"azuread"}},
		{"oidc without an issuer", config.SSOConfig{OIDCClientID: "id", OIDCClientSecret: "s"}, []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := enabledSSOProviders(tc.sso)
			providers := newSSOProviders(config.Config{WebAppURL: mustURL(t, webApp), SSO: tc.sso}, slog.New(slog.DiscardHandler))
			if strings.Join(got, ",") != strings.Join(tc.want, ",") || len(providers) != len(tc.want) {
				t.Fatalf("enabled = %v (%d built), want %v", got, len(providers), tc.want)
			}
		})
	}
}

// forma2's better-auth-providers.test.ts: the tenant decides between discovery with full id_token verification and Microsoft's explicit endpoints with Graph, and only an explicitly configured placeholder tenant is warned about.
func TestAzureADTenant(t *testing.T) {
	cases := []struct {
		tenant    string
		graph     bool
		authority string
		discovery string
		warn      bool
	}{
		{"", true, "common", "", false},
		{"  ", true, "common", "", false},
		{"common", true, "common", "", true},
		{"Common", true, "common", "", true},
		{"ORGANIZATIONS", true, "organizations", "", true},
		{"consumers", false, "", "https://login.microsoftonline.com/consumers/v2.0/.well-known/openid-configuration", false},
		{"9188040d-6c67-4c5b-b112-36a304b66dad", false, "", "https://login.microsoftonline.com/9188040d-6c67-4c5b-b112-36a304b66dad/v2.0/.well-known/openid-configuration", false},
		{" Contoso.onmicrosoft.com ", false, "", "https://login.microsoftonline.com/Contoso.onmicrosoft.com/v2.0/.well-known/openid-configuration", false},
	}
	for _, tc := range cases {
		t.Run(tc.tenant, func(t *testing.T) {
			var logs bytes.Buffer
			cfg := config.Config{WebAppURL: mustURL(t, webApp), SSO: config.SSOConfig{AzureADClientID: "id", AzureADClientSecret: "s", AzureADTenantID: tc.tenant}}
			p := newSSOProviders(cfg, slog.New(slog.NewTextHandler(&logs, nil)))[providerAzureAD]
			if tc.graph {
				if p.source != sourceMicrosoftGraph || p.discoveryURL != "" || p.userInfoURL != "https://graph.microsoft.com/oidc/userinfo" ||
					p.oauth.Endpoint.AuthURL != "https://login.microsoftonline.com/"+tc.authority+"/oauth2/v2.0/authorize" ||
					p.oauth.Endpoint.TokenURL != "https://login.microsoftonline.com/"+tc.authority+"/oauth2/v2.0/token" {
					t.Fatalf("provider %+v", p)
				}
			} else if p.source != sourceIDToken || p.discoveryURL != tc.discovery {
				t.Fatalf("provider %+v", p)
			}
			if warned := strings.Contains(logs.String(), "AZUREAD_TENANT_ID="); warned != tc.warn {
				t.Fatalf("warned = %v, want %v: %s", warned, tc.warn, logs.String())
			}
		})
	}
}

// The generic provider takes the Graph path only for Microsoft's placeholder authorities, matched on the parsed host.
func TestOIDCIssuer(t *testing.T) {
	cases := []struct {
		issuer    string
		authority string
		discovery string
	}{
		{"https://login.microsoftonline.com/common/v2.0", "common", ""},
		{"https://LOGIN.microsoftonline.com/Organizations/v2.0", "organizations", ""},
		{"https://login.microsoftonline.com/consumers/v2.0", "", "https://login.microsoftonline.com/consumers/v2.0/.well-known/openid-configuration"},
		{"https://login.microsoftonline.com/0000-tenant/v2.0", "", "https://login.microsoftonline.com/0000-tenant/v2.0/.well-known/openid-configuration"},
		{"https://login.microsoftonline.com.evil.example/common/v2.0", "", "https://login.microsoftonline.com.evil.example/common/v2.0/.well-known/openid-configuration"},
		{"https://evil.example/login.microsoftonline.com/common", "", "https://evil.example/login.microsoftonline.com/common/.well-known/openid-configuration"},
		{"https://idp.example.com/realms/forma/", "", "https://idp.example.com/realms/forma//.well-known/openid-configuration"},
	}
	for _, tc := range cases {
		t.Run(tc.issuer, func(t *testing.T) {
			var logs bytes.Buffer
			cfg := config.Config{WebAppURL: mustURL(t, webApp), SSO: config.SSOConfig{OIDCClientID: "id", OIDCClientSecret: "s", OIDCIssuer: tc.issuer}}
			p := newSSOProviders(cfg, slog.New(slog.NewTextHandler(&logs, nil)))[providerOpenID]
			if tc.authority != "" {
				if p.source != sourceMicrosoftGraph || p.oauth.Endpoint.AuthURL != "https://login.microsoftonline.com/"+tc.authority+"/oauth2/v2.0/authorize" || !strings.Contains(logs.String(), "OIDC_ISSUER points at Microsoft") {
					t.Fatalf("provider %+v, logs %s", p, logs.String())
				}
				return
			}
			if p.source != sourceIDToken || p.discoveryURL != tc.discovery || logs.Len() != 0 {
				t.Fatalf("provider %+v, logs %s", p, logs.String())
			}
		})
	}
}
