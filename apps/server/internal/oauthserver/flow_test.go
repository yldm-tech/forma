package oauthserver

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/google/uuid"
)

// The whole flow an MCP client drives: registration, authorization with PKCE, the consent page, the code exchange, then the access token's claims as /api/mcp reads them.
func TestAuthorizationCodeFlow(t *testing.T) {
	e := newEnv(t)
	user := e.user("ada@example.com")
	clientID := e.publicClient()
	p := newPKCE()
	q := authorizeQuery(clientID, p, "openid profile email offline_access surveys:read surveys:write")

	consentPage := e.startAuthorization(q, user)
	if consentPage.Path != consentPath || consentPage.Query().Get("client_id") != clientID || consentPage.Query().Get("redirect_uri") != redirectURI ||
		consentPage.Query().Get("scope") != "openid profile email offline_access surveys:read surveys:write" {
		t.Fatalf("consent page %s", consentPage)
	}
	requestID := consentPage.Query().Get("request_id")
	view := e.json(e.do(http.MethodGet, "/api/app/oauth/authorization-requests/"+requestID, nil, appHeaders(user)))
	if view["consentRequired"] != true || view["redirectUri"] != redirectURI || view["resource"] != resource || view["client"].(map[string]any)["name"] != "Claude" {
		t.Fatalf("authorization request %v", view)
	}
	callback, _ := url.Parse(e.consent(requestID, user, true))
	// Only the browser that approved the request collects its code: not one without a session, not another user's.
	for name, headers := range map[string]map[string]string{"no session": nil, "another user": {testUserHeader: e.user("mallory@example.com").String()}} {
		rec := e.do(http.MethodGet, pathOf(callback), nil, headers)
		if rec.Code != http.StatusForbidden || rec.Header().Get("Location") != "" || e.json(rec)["error"] != "access_denied" {
			t.Fatalf("callback with %s: %d %v %s", name, rec.Code, rec.Header(), rec.Body.String())
		}
	}
	back := location(t, e.do(http.MethodGet, pathOf(callback), nil, map[string]string{testUserHeader: user.String()}))
	code := back.Query().Get("code")
	if code == "" || back.Query().Get("state") != "st" {
		t.Fatalf("redirected to %s", back)
	}

	status, out := e.token(url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirectURI}, "client_id": {clientID}, "code_verifier": {p.verifier}, "resource": {resource}})
	if status != http.StatusOK || out["token_type"] != "Bearer" || out["refresh_token"] == nil || out["id_token"] == nil || out["expires_in"].(float64) > 900 {
		t.Fatalf("token: %d %v", status, out)
	}
	access := out["access_token"].(string)
	claims, err := e.verify(access)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Subject != user.String() || claims.Issuer != issuer || claims.ClientID != clientID || claims.AuthorizedParty != clientID ||
		claims.Scope != "openid profile email offline_access surveys:read surveys:write" || claims.Email != "ada@example.com" || claims.Name != "Ada" ||
		!slices.Equal(claims.Audience, []string{resource, issuer + "/oauth2/userinfo"}) || claims.Expiry-claims.IssuedAt != 900 {
		t.Fatalf("claims %+v", claims)
	}
	jws, err := jose.ParseSigned(access, []jose.SignatureAlgorithm{jose.EdDSA})
	if err != nil || jws.Signatures[0].Protected.ExtraHeaders[jose.HeaderType] != "at+jwt" {
		t.Fatalf("access token header: %v %+v", err, jws)
	}
	if !strings.HasPrefix(out["refresh_token"].(string), "fbor_") {
		t.Fatalf("refresh token %v", out["refresh_token"])
	}

	// The code is spent.
	if status, out := e.token(url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirectURI}, "client_id": {clientID}, "code_verifier": {p.verifier}}); status != http.StatusBadRequest || out["error"] != "invalid_grant" {
		t.Fatalf("code reuse: %d %v", status, out)
	}

	// The keys are published.
	jwks := e.json(e.do(http.MethodGet, "/api/auth/jwks", nil, nil))
	keys := jwks["keys"].([]any)
	if len(keys) != 1 || keys[0].(map[string]any)["alg"] != "EdDSA" || keys[0].(map[string]any)["d"] != nil {
		t.Fatalf("jwks %v", jwks)
	}

	// userinfo answers for the token.
	info := e.do(http.MethodGet, "/api/auth/oauth2/userinfo", nil, map[string]string{"Authorization": "Bearer " + access})
	if body := e.json(info); info.Code != http.StatusOK || body["sub"] != user.String() || body["email"] != "ada@example.com" || body["name"] != "Ada" {
		t.Fatalf("userinfo %d %v", info.Code, body)
	}

	// The consent is recorded and listed on the profile page.
	consents := e.do(http.MethodGet, "/api/app/oauth/consents", nil, appHeaders(user))
	if consents.Code != http.StatusOK || !strings.Contains(consents.Body.String(), `"clientId":"`+clientID+`"`) || !strings.Contains(consents.Body.String(), `"surveys:write"`) {
		t.Fatalf("consents %d %s", consents.Code, consents.Body.String())
	}
}

// A second authorization within the approved scopes and resource skips the consent page; one asking for more, or with prompt=consent, does not.
func TestConsentIsReused(t *testing.T) {
	e := newEnv(t)
	user := e.user("ada@example.com")
	clientID := e.publicClient()
	e.tokens(clientID, user, "surveys:read offline_access")

	if next := e.startAuthorization(authorizeQuery(clientID, newPKCE(), "surveys:read"), user); next.Path != "/api/auth/oauth2/authorize/callback" {
		t.Fatalf("an approved request went to %s", next)
	}
	if next := e.startAuthorization(authorizeQuery(clientID, newPKCE(), "surveys:read surveys:write"), user); next.Path != consentPath {
		t.Fatalf("a wider request went to %s", next)
	}
	q := authorizeQuery(clientID, newPKCE(), "surveys:read")
	q.Set("prompt", "consent")
	if next := e.startAuthorization(q, user); next.Path != consentPath {
		t.Fatalf("prompt=consent went to %s", next)
	}
	// Without a session the consent page signs the user in first.
	if next := e.startAuthorization(authorizeQuery(clientID, newPKCE(), "surveys:read"), uuid.Nil); next.Path != consentPath {
		t.Fatalf("a signed-out user went to %s", next)
	}
	// prompt=none cannot show anything.
	q = authorizeQuery(clientID, newPKCE(), "surveys:read surveys:write")
	q.Set("prompt", "none")
	if next := e.startAuthorization(q, user); next.Query().Get("error") != "consent_required" || next.Query().Get("state") != "st" {
		t.Fatalf("prompt=none went to %s", next)
	}
}

// Denying sends the client access_denied with its state.
func TestConsentDenied(t *testing.T) {
	e := newEnv(t)
	user := e.user("ada@example.com")
	clientID := e.publicClient()
	next := e.startAuthorization(authorizeQuery(clientID, newPKCE(), "surveys:read"), user)
	denied, _ := url.Parse(e.consent(next.Query().Get("request_id"), user, false))
	if !strings.HasPrefix(denied.String(), redirectURI) || denied.Query().Get("error") != "access_denied" || denied.Query().Get("state") != "st" {
		t.Fatalf("denied to %s", denied)
	}
	if e.count(`SELECT count(*) FROM oauth_authorization_requests`) != 0 || e.count(`SELECT count(*) FROM oauth_consents`) != 0 {
		t.Fatal("a denied request left state behind")
	}
	// The request is gone.
	if rec := e.do(http.MethodGet, "/api/app/oauth/authorization-requests/"+next.Query().Get("request_id"), nil, appHeaders(user)); rec.Code != http.StatusNotFound {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
}

// Refresh tokens rotate: each use issues the next one, and presenting a spent one again is a replay that revokes the whole family.
func TestRefreshTokenRotationAndReplay(t *testing.T) {
	e := newEnv(t)
	user := e.user("ada@example.com")
	clientID := e.publicClient()
	first := e.tokens(clientID, user, "offline_access surveys:read surveys:write")

	refresh := func(token string, extra url.Values) (int, map[string]any) {
		form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {token}, "client_id": {clientID}}
		for k, v := range extra {
			form[k] = v
		}
		return e.token(form)
	}
	status, second := refresh(first["refresh_token"].(string), url.Values{"scope": {"surveys:read"}, "resource": {resource}})
	if status != http.StatusOK || second["refresh_token"] == first["refresh_token"] {
		t.Fatalf("refresh: %d %v", status, second)
	}
	// Narrowed to surveys:read, still bound to the resource.
	claims, err := e.verify(second["access_token"].(string))
	if err != nil || claims.Scope != "surveys:read" || !slices.Equal(claims.Audience, []string{resource}) {
		t.Fatalf("refreshed claims %+v %v", claims, err)
	}
	// Widening is refused.
	if status, out := refresh(second["refresh_token"].(string), url.Values{"scope": {"surveys:read workflows:write"}}); status != http.StatusBadRequest || out["error"] != "invalid_scope" {
		t.Fatalf("widened: %d %v", status, out)
	}
	// Another resource is refused.
	if status, out := refresh(second["refresh_token"].(string), url.Values{"resource": {"https://other.example.com/api"}}); status != http.StatusBadRequest || out["error"] != "invalid_target" {
		t.Fatalf("other resource: %d %v", status, out)
	}

	// Replaying the first token revokes the family, the current token included.
	if status, out := refresh(first["refresh_token"].(string), nil); status != http.StatusBadRequest || out["error"] != "invalid_grant" {
		t.Fatalf("replay: %d %v", status, out)
	}
	if status, out := refresh(second["refresh_token"].(string), nil); status != http.StatusBadRequest || out["error"] != "invalid_grant" {
		t.Fatalf("after replay: %d %v", status, out)
	}
	if n := e.count(`SELECT count(*) FROM oauth_refresh_tokens WHERE revoked_at IS NULL`); n != 0 {
		t.Fatalf("%d refresh tokens still live", n)
	}
	if n := e.count(`SELECT count(*) FROM oauth_access_tokens WHERE revoked_at IS NULL`); n != 0 {
		t.Fatalf("%d access tokens still live", n)
	}
}

// Revoking the consent on the profile page revokes the client's refresh tokens; the revocation endpoint revokes one family.
func TestRevocation(t *testing.T) {
	e := newEnv(t)
	user := e.user("ada@example.com")
	clientID := e.publicClient()
	first := e.tokens(clientID, user, "offline_access surveys:read")

	rec := e.do(http.MethodPost, "/api/auth/oauth2/revoke", strings.NewReader(url.Values{"token": {first["refresh_token"].(string)}, "client_id": {clientID}}.Encode()), map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
	if rec.Code != http.StatusOK {
		t.Fatalf("revoke: %d %s", rec.Code, rec.Body.String())
	}
	if status, out := e.token(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {first["refresh_token"].(string)}, "client_id": {clientID}}); status != http.StatusBadRequest || out["error"] != "invalid_grant" {
		t.Fatalf("revoked refresh: %d %v", status, out)
	}

	second := e.tokens(clientID, user, "offline_access surveys:read")
	consents := e.do(http.MethodGet, "/api/app/oauth/consents", nil, appHeaders(user))
	var list []map[string]any
	if err := json.Unmarshal(consents.Body.Bytes(), &list); err != nil || len(list) != 1 {
		t.Fatalf("consents %s", consents.Body.String())
	}
	other := e.user("eve@example.com")
	if rec := e.do(http.MethodDelete, "/api/app/oauth/consents/"+list[0]["id"].(string), nil, appHeaders(other)); rec.Code != http.StatusNotFound {
		t.Fatalf("another user revoked: %d", rec.Code)
	}
	if rec := e.do(http.MethodDelete, "/api/app/oauth/consents/"+list[0]["id"].(string), nil, appHeaders(user)); rec.Code != http.StatusNoContent {
		t.Fatalf("revoke consent: %d %s", rec.Code, rec.Body.String())
	}
	if status, out := e.token(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {second["refresh_token"].(string)}, "client_id": {clientID}}); status != http.StatusBadRequest || out["error"] != "invalid_grant" {
		t.Fatalf("refresh after consent revoked: %d %v", status, out)
	}
	// The next authorization asks again.
	if next := e.startAuthorization(authorizeQuery(clientID, newPKCE(), "surveys:read"), user); next.Path != consentPath {
		t.Fatalf("went to %s", next)
	}
}

// The authorization request is refused for an unregistered redirect URI (without redirecting to it), and sent back to the client for missing or plain PKCE, an unknown scope and a foreign resource.
func TestAuthorizationRequestIsChecked(t *testing.T) {
	e := newEnv(t)
	clientID := e.publicClient()

	wrong := authorizeQuery(clientID, newPKCE(), "surveys:read")
	wrong.Set("redirect_uri", "http://127.0.0.1:9999/callback")
	if rec := e.do(http.MethodGet, "/api/auth/oauth2/authorize?"+wrong.Encode(), nil, nil); rec.Code != http.StatusBadRequest || rec.Header().Get("Location") != "" {
		t.Fatalf("another loopback port: %d %v", rec.Code, rec.Header())
	}
	wrong.Set("redirect_uri", "https://evil.example.com/callback")
	if rec := e.do(http.MethodGet, "/api/auth/oauth2/authorize?"+wrong.Encode(), nil, nil); rec.Code != http.StatusBadRequest || rec.Header().Get("Location") != "" {
		t.Fatalf("foreign redirect: %d %v", rec.Code, rec.Header())
	}
	unknown := authorizeQuery("no-such-client", newPKCE(), "surveys:read")
	if rec := e.do(http.MethodGet, "/api/auth/oauth2/authorize?"+unknown.Encode(), nil, nil); rec.Code != http.StatusBadRequest || rec.Header().Get("Location") != "" {
		t.Fatalf("unknown client: %d %v", rec.Code, rec.Header())
	}

	cases := []struct {
		name   string
		change func(url.Values)
		error  string
	}{
		{"no PKCE", func(q url.Values) { q.Del("code_challenge"); q.Del("code_challenge_method") }, "invalid_request"},
		{"plain PKCE", func(q url.Values) { q.Set("code_challenge_method", "plain") }, "invalid_request"},
		{"a scope outside the set", func(q url.Values) { q.Set("scope", "surveys:read billing:admin") }, "invalid_scope"},
		{"another resource", func(q url.Values) { q.Set("resource", "https://other.example.com/api/mcp") }, "invalid_target"},
		{"a second resource", func(q url.Values) { q.Add("resource", "https://other.example.com/api/mcp") }, "invalid_target"},
	}
	for _, tc := range cases {
		q := authorizeQuery(clientID, newPKCE(), "surveys:read")
		tc.change(q)
		back := location(t, e.do(http.MethodGet, "/api/auth/oauth2/authorize?"+q.Encode(), nil, nil))
		if !strings.HasPrefix(back.String(), redirectURI) || back.Query().Get("error") != tc.error || back.Query().Get("state") != "st" {
			t.Errorf("%s: redirected to %s", tc.name, back)
		}
	}
	// Without a resource indicator the MCP resource is meant.
	q := authorizeQuery(clientID, newPKCE(), "surveys:read")
	q.Del("resource")
	if next := e.startAuthorization(q, e.user("ada@example.com")); next.Path != consentPath {
		t.Fatalf("went to %s", next)
	}
}

// The code exchange needs the verifier, the same redirect URI and the same client, and a resource the client may use.
func TestCodeExchangeIsChecked(t *testing.T) {
	e := newEnv(t)
	user := e.user("ada@example.com")
	clientID := e.publicClient()
	otherClient := e.publicClient()

	cases := []struct {
		name   string
		change func(url.Values)
		error  string
	}{
		{"no verifier", func(f url.Values) { f.Del("code_verifier") }, "invalid_request"},
		{"a wrong verifier", func(f url.Values) { f.Set("code_verifier", newPKCE().verifier) }, "invalid_grant"},
		{"another redirect URI", func(f url.Values) { f.Set("redirect_uri", "http://127.0.0.1:1/callback") }, "invalid_grant"},
		{"another client", func(f url.Values) { f.Set("client_id", otherClient) }, "invalid_grant"},
		{"another resource", func(f url.Values) { f.Set("resource", "https://other.example.com/api/mcp") }, "invalid_target"},
	}
	for _, tc := range cases {
		p := newPKCE()
		code := e.code(authorizeQuery(clientID, p, "surveys:read"), user)
		form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirectURI}, "client_id": {clientID}, "code_verifier": {p.verifier}}
		tc.change(form)
		if status, out := e.token(form); status != http.StatusBadRequest || out["error"] != tc.error {
			t.Errorf("%s: %d %v", tc.name, status, out)
		}
	}
}

// An inactive user is issued nothing.
func TestInactiveUserGetsNoToken(t *testing.T) {
	e := newEnv(t)
	user := e.user("ada@example.com")
	clientID := e.publicClient()
	p := newPKCE()
	code := e.code(authorizeQuery(clientID, p, "surveys:read"), user)
	if err := e.db.Exec(`UPDATE users SET is_active = false WHERE id = ?`, user).Error; err != nil {
		t.Fatal(err)
	}
	if status, out := e.token(url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirectURI}, "client_id": {clientID}, "code_verifier": {p.verifier}}); status != http.StatusBadRequest || out["error"] != "invalid_grant" {
		t.Fatalf("%d %v", status, out)
	}
}

// The verifier accepts only at+jwt tokens of this issuer for the MCP resource that have not expired.
func TestVerifier(t *testing.T) {
	e := newEnv(t)
	ctx := t.Context()
	now := time.Now()
	sign := func(claims AccessTokenClaims, typ string) string {
		key, err := e.keys.signing(ctx)
		if err != nil {
			t.Fatal(err)
		}
		signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.EdDSA, Key: jose.JSONWebKey{Key: key.private, KeyID: key.ID()}}, (&jose.SignerOptions{}).WithType(jose.ContentType(typ)))
		if err != nil {
			t.Fatal(err)
		}
		payload, _ := json.Marshal(claims)
		jws, err := signer.Sign(payload)
		if err != nil {
			t.Fatal(err)
		}
		out, _ := jws.CompactSerialize()
		return out
	}
	valid := AccessTokenClaims{Issuer: issuer, Subject: uuid.NewString(), Audience: Audience{resource}, Expiry: now.Add(time.Minute).Unix(), IssuedAt: now.Unix(), Scope: "surveys:read"}
	if _, err := e.verify(sign(valid, "at+jwt")); err != nil {
		t.Fatalf("valid token refused: %v", err)
	}
	cases := map[string]string{}
	cases["typ JWT"] = sign(valid, "JWT")
	expired := valid
	expired.Expiry = now.Add(-time.Second).Unix()
	cases["expired"] = sign(expired, "at+jwt")
	foreignIssuer := valid
	foreignIssuer.Issuer = "https://evil.example.com/api/auth"
	cases["another issuer"] = sign(foreignIssuer, "at+jwt")
	foreignAudience := valid
	foreignAudience.Audience = Audience{"https://other.example.com/api/mcp"}
	cases["another resource"] = sign(foreignAudience, "at+jwt")
	for name, token := range cases {
		if _, err := e.verify(token); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	// A token signed with a key the server does not have.
	_, foreignKey, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := jose.NewSigner(jose.SigningKey{Algorithm: jose.EdDSA, Key: jose.JSONWebKey{Key: foreignKey, KeyID: uuid.NewString()}}, (&jose.SignerOptions{}).WithType("at+jwt"))
	payload, _ := json.Marshal(valid)
	jws, _ := signer.Sign(payload)
	forged, _ := jws.CompactSerialize()
	if _, err := e.verify(forged); err == nil {
		t.Error("a token signed with a foreign key was accepted")
	}
}
