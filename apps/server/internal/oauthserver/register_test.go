package oauthserver

import (
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/yldm-tech/forma/apps/server/internal/platform/ratelimit"
)

// Registration follows forma2's rules: every client gets every scope and the MCP resource, whatever it asked for; a loopback redirect without application_type makes a native client; a web client may not use loopback and a native one may not use plain http elsewhere.
func TestDynamicClientRegistration(t *testing.T) {
	e := newEnv(t)

	t.Run("an MCP client that does not say it is native", func(t *testing.T) {
		rec := e.register(map[string]any{
			"client_name": "MCP Inspector", "redirect_uris": []string{redirectURI}, "grant_types": []string{"authorization_code", "refresh_token"},
			"response_types": []string{"code"}, "token_endpoint_auth_method": "none", "scope": "surveys:read",
		})
		out := e.json(rec)
		if rec.Code != http.StatusCreated || out["application_type"] != "native" || out["client_secret"] != nil || out["token_endpoint_auth_method"] != "none" ||
			out["scope"] != strings.Join(Scopes, " ") || rec.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%d %v %v", rec.Code, out, rec.Header())
		}
		var resources []string
		if err := e.db.Raw(`SELECT unnest(resources) FROM oauth_clients WHERE client_id = ?`, out["client_id"]).Scan(&resources).Error; err != nil || !slices.Equal(resources, []string{resource}) {
			t.Fatalf("resources %v %v", resources, err)
		}
	})

	t.Run("defaults", func(t *testing.T) {
		rec := e.register(map[string]any{"redirect_uris": []string{"https://app.example.org/callback"}})
		out := e.json(rec)
		if rec.Code != http.StatusCreated || out["application_type"] != "web" || out["token_endpoint_auth_method"] != "client_secret_basic" ||
			!strings.HasPrefix(out["client_secret"].(string), "fbocs_") || out["client_secret_expires_at"] != float64(0) ||
			!slices.Equal(toStrings(out["grant_types"]), []string{"authorization_code"}) || !slices.Equal(toStrings(out["response_types"]), []string{"code"}) {
			t.Fatalf("%d %v", rec.Code, out)
		}
	})

	cases := []struct {
		name  string
		body  map[string]any
		error string
	}{
		{"no redirect URI", map[string]any{"client_name": "x"}, "invalid_redirect_uri"},
		{"a web client with a loopback redirect", map[string]any{"application_type": "web", "redirect_uris": []string{redirectURI}}, "invalid_redirect_uri"},
		{"a web client with plain http", map[string]any{"redirect_uris": []string{"http://app.example.org/callback"}}, "invalid_redirect_uri"},
		{"a native client with plain http outside loopback", map[string]any{"redirect_uris": []string{redirectURI, "http://evil.example.com/callback"}}, "invalid_redirect_uri"},
		{"a javascript redirect", map[string]any{"application_type": "native", "redirect_uris": []string{"javascript:alert(1)"}}, "invalid_redirect_uri"},
		{"a redirect with a fragment", map[string]any{"redirect_uris": []string{"https://app.example.org/cb#x"}}, "invalid_redirect_uri"},
		{"a relative redirect", map[string]any{"redirect_uris": []string{"/callback"}}, "invalid_redirect_uri"},
		{"an unknown grant type", map[string]any{"redirect_uris": []string{redirectURI}, "grant_types": []string{"client_credentials"}}, "invalid_client_metadata"},
		{"the implicit response type", map[string]any{"redirect_uris": []string{redirectURI}, "response_types": []string{"token"}}, "invalid_client_metadata"},
		{"an unknown auth method", map[string]any{"redirect_uris": []string{redirectURI}, "token_endpoint_auth_method": "private_key_jwt"}, "invalid_client_metadata"},
		{"an unknown application type", map[string]any{"redirect_uris": []string{redirectURI}, "application_type": "service"}, "invalid_client_metadata"},
		{"redirect URIs of the wrong type", map[string]any{"redirect_uris": redirectURI}, "invalid_client_metadata"},
		{"a client URI that is no URL", map[string]any{"redirect_uris": []string{redirectURI}, "client_uri": "javascript:alert(1)"}, "invalid_client_metadata"},
	}
	for _, tc := range cases {
		rec := e.register(tc.body)
		if out := e.json(rec); rec.Code != http.StatusBadRequest || out["error"] != tc.error {
			t.Errorf("%s: %d %v", tc.name, rec.Code, out)
		}
	}
	if rec := e.do(http.MethodPost, "/api/auth/oauth2/register", strings.NewReader(`[1]`), nil); rec.Code != http.StatusBadRequest || e.json(rec)["error"] != "invalid_client_metadata" {
		t.Fatalf("not an object: %d %s", rec.Code, rec.Body.String())
	}
	if n := e.count(`SELECT count(*) FROM oauth_clients`); n != 2 {
		t.Fatalf("%d clients registered", n)
	}
}

func toStrings(v any) []string {
	list, _ := v.([]any)
	out := make([]string, len(list))
	for i, x := range list {
		out[i], _ = x.(string)
	}
	return out
}

// The authorization server metadata names forma2's endpoints under the issuer, at the issuer and at RFC 8414's path-inserted locations, with forma2's caching.
func TestMetadata(t *testing.T) {
	e := newEnv(t)
	for _, path := range []string{
		"/.well-known/oauth-authorization-server", "/.well-known/oauth-authorization-server/api/auth", "/.well-known/openid-configuration/api/auth",
		"/api/auth/.well-known/oauth-authorization-server", "/api/auth/.well-known/openid-configuration",
	} {
		rec := e.do(http.MethodGet, path, nil, nil)
		out := e.json(rec)
		if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != metadataCacheControl || out["issuer"] != issuer ||
			out["authorization_endpoint"] != issuer+"/oauth2/authorize" || out["token_endpoint"] != issuer+"/oauth2/token" ||
			out["registration_endpoint"] != issuer+"/oauth2/register" || out["revocation_endpoint"] != issuer+"/oauth2/revoke" ||
			out["introspection_endpoint"] != issuer+"/oauth2/introspect" || out["userinfo_endpoint"] != issuer+"/oauth2/userinfo" || out["jwks_uri"] != issuer+"/jwks" ||
			!slices.Equal(toStrings(out["scopes_supported"]), Scopes) || !slices.Equal(toStrings(out["code_challenge_methods_supported"]), []string{"S256"}) ||
			!slices.Equal(toStrings(out["response_types_supported"]), []string{"code"}) || !slices.Equal(toStrings(out["grant_types_supported"]), []string{"authorization_code", "refresh_token"}) {
			t.Errorf("%s: %d %v", path, rec.Code, out)
		}
	}
}

// Every scope the protected resource advertises can be granted, and the resource scopes are among them.
func TestScopeSets(t *testing.T) {
	for _, s := range append(slices.Clone(ProtectedResourceScopes), ResourceScopes...) {
		if !slices.Contains(Scopes, s) {
			t.Errorf("%s is advertised but cannot be granted", s)
		}
	}
	if slices.Contains(ResourceScopes, "offline_access") || !slices.Contains(ProtectedResourceScopes, "offline_access") {
		t.Fatal("offline_access must be advertised and must not count as a resource scope")
	}
}

// The URLs follow forma2's oauth-urls.ts: a WEBAPP_URL subpath is kept, BETTER_AUTH_URL decides the issuer, and /api/auth is not appended twice. The consent page is an SPA page, under the app's base path.
func TestURLs(t *testing.T) {
	cases := []struct {
		webApp, betterAuth           string
		resource, issuer, prm, webUI string
	}{
		{"https://app.example.com/", "", "https://app.example.com/api/mcp", "https://app.example.com/api/auth", "https://app.example.com/.well-known/oauth-protected-resource/api/mcp", "https://app.example.com/app"},
		{"https://example.com/forma/?x=1#y", "", "https://example.com/forma/api/mcp", "https://example.com/forma/api/auth", "https://example.com/forma/.well-known/oauth-protected-resource/api/mcp", "https://example.com/forma/app"},
		{"https://app.example.com", "https://auth.example.com/custom/api/auth/", "https://app.example.com/api/mcp", "https://auth.example.com/custom/api/auth", "https://app.example.com/.well-known/oauth-protected-resource/api/mcp", "https://app.example.com/app"},
		{"https://APP.example.com:443/api/mcp", "", "https://app.example.com/api/mcp", "https://app.example.com/api/mcp/api/auth", "https://app.example.com/api/mcp/.well-known/oauth-protected-resource/api/mcp", "https://app.example.com/api/mcp/app"},
	}
	for _, tc := range cases {
		cfg := configFor(t, tc.webApp, tc.betterAuth)
		u := NewURLs(cfg)
		if u.Resource != tc.resource || u.Issuer != tc.issuer || u.ProtectedResourceMetadata != tc.prm || u.WebApp != tc.webUI || u.UserInfo != tc.issuer+"/oauth2/userinfo" || u.JWKS != tc.issuer+"/jwks" {
			t.Errorf("%s %s: %+v", tc.webApp, tc.betterAuth, u)
		}
	}
}

// Registration, the token endpoint, introspection and revocation are limited per address with forma2's limits; a spent window answers 429 as Better Auth's limiter did.
func TestRateLimits(t *testing.T) {
	e := newEnv(t, true)
	ctx := t.Context()
	for _, tc := range []struct {
		rule ratelimit.Rule
		path string
	}{
		{ratelimit.BAOAuthRegister, "/api/auth/oauth2/register"},
		{ratelimit.BAOAuthToken, "/api/auth/oauth2/token"},
		{ratelimit.BAOAuthIntrospect, "/api/auth/oauth2/introspect"},
		{ratelimit.BAOAuthRevoke, "/api/auth/oauth2/revoke"},
	} {
		if err := e.limiter.ApplyN(ctx, tc.rule, ratelimit.IPIdentifier(ctx), tc.rule.Limit); err != nil {
			t.Fatal(err)
		}
		rec := e.do(http.MethodPost, tc.path, strings.NewReader("{}"), nil)
		if rec.Code != http.StatusTooManyRequests || e.json(rec)["message"] != "Too many requests. Please try again later." || rec.Header().Get("X-Retry-After") == "" {
			t.Errorf("%s: %d %s %v", tc.path, rec.Code, rec.Body.String(), rec.Header())
		}
	}
	if rec := e.register(map[string]any{"redirect_uris": []string{redirectURI}}); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("register after the window was spent: %d", rec.Code)
	}
}

// A confidential client may authenticate to introspection with client_secret_post, which forma2 advertised and accepted, as well as with HTTP Basic.
func TestIntrospectionAcceptsClientSecretPost(t *testing.T) {
	e := newEnv(t)
	user := e.user("ada@example.com")
	rec := e.register(map[string]any{"client_name": "Backend", "redirect_uris": []string{redirectURI}, "token_endpoint_auth_method": "client_secret_post", "grant_types": []string{"authorization_code", "refresh_token"}})
	registered := e.json(rec)
	clientID, secret := registered["client_id"].(string), registered["client_secret"].(string)
	p := newPKCE()
	code := e.code(authorizeQuery(clientID, p, "surveys:read"), user)
	status, out := e.token(url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirectURI}, "client_id": {clientID}, "client_secret": {secret}, "code_verifier": {p.verifier}})
	if status != http.StatusOK {
		t.Fatalf("token: %d %v", status, out)
	}
	form := url.Values{"token": {out["access_token"].(string)}, "client_id": {clientID}, "client_secret": {secret}}
	introspected := e.do(http.MethodPost, "/api/auth/oauth2/introspect", strings.NewReader(form.Encode()), map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
	if body := e.json(introspected); introspected.Code != http.StatusOK || body["active"] != true || body["sub"] != user.String() {
		t.Fatalf("introspect with client_secret_post: %d %v", introspected.Code, body)
	}
	form.Set("client_secret", "fbocs_wrong")
	if wrong := e.do(http.MethodPost, "/api/auth/oauth2/introspect", strings.NewReader(form.Encode()), map[string]string{"Content-Type": "application/x-www-form-urlencoded"}); wrong.Code == http.StatusOK {
		t.Fatalf("a wrong secret introspected: %s", wrong.Body.String())
	}
}

// RP-initiated logout is served where the discovery document says, and answers as forma2's Better Auth did for its dynamically registered clients, none of which could initiate logout.
func TestEndSession(t *testing.T) {
	e := newEnv(t)
	user := e.user("ada@example.com")
	clientID := e.publicClient()
	idToken := e.tokens(clientID, user, "openid surveys:read")["id_token"].(string)
	if got := e.json(e.do(http.MethodGet, "/.well-known/openid-configuration", nil, nil))["end_session_endpoint"]; got != issuer+"/oauth2/end-session" {
		t.Fatalf("end_session_endpoint %v", got)
	}
	jsonAccept := map[string]string{"Accept": "application/json"}
	for _, tc := range []struct {
		name        string
		method      string
		query       url.Values
		status      int
		error, desc string
	}{
		{"client_id", http.MethodGet, url.Values{"client_id": {clientID}}, http.StatusUnauthorized, "invalid_client", "The client is not allowed to initiate logout"},
		{"id_token_hint", http.MethodGet, url.Values{"id_token_hint": {idToken}, "post_logout_redirect_uri": {redirectURI}}, http.StatusUnauthorized, "invalid_client", "The client is not allowed to initiate logout"},
		{"unknown client", http.MethodGet, url.Values{"client_id": {"nope"}}, http.StatusBadRequest, "invalid_client", "The logout client does not exist"},
		{"hint naming no client", http.MethodPost, url.Values{"id_token_hint": {"not-a-jwt"}}, http.StatusBadRequest, "invalid_client", "The logout client does not exist"},
		{"nothing", http.MethodPost, url.Values{}, http.StatusBadRequest, "invalid_request", "No active session is available for logout"},
	} {
		target, body := "/api/auth/oauth2/end-session?"+tc.query.Encode(), io.Reader(nil)
		headers := jsonAccept
		if tc.method == http.MethodPost {
			target, body = "/api/auth/oauth2/end-session", strings.NewReader(tc.query.Encode())
			headers = map[string]string{"Accept": "application/json", "Content-Type": "application/x-www-form-urlencoded"}
		}
		rec := e.do(tc.method, target, body, headers)
		if out := e.json(rec); rec.Code != tc.status || out["error"] != tc.error || out["error_description"] != tc.desc {
			t.Errorf("%s: %d %v", tc.name, rec.Code, out)
		}
	}
	// A browser gets Better Auth's error page.
	page := e.do(http.MethodGet, "/api/auth/oauth2/end-session?client_id="+clientID, nil, map[string]string{"Accept": "text/html", "Sec-Fetch-Mode": "navigate"})
	if page.Code != http.StatusUnauthorized || !strings.Contains(page.Body.String(), "The client is not allowed to initiate logout") || !strings.HasPrefix(page.Header().Get("Content-Type"), "text/html") {
		t.Errorf("browser refusal: %d %s", page.Code, page.Body.String())
	}
}
