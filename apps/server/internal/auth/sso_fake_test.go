package auth

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	fakeClientID     = "forma-client"
	fakeClientSecret = "forma-secret"
)

// fakeIdP is a real identity provider on a local httptest server: an OpenID Connect discovery document, an RS256 key set, a token endpoint that checks client authentication, the redirect URI and the PKCE verifier, userinfo endpoints (OIDC, Microsoft Graph's and GitHub's user API), and id_tokens signed with its key. The authorization step, which a person would complete in the browser, is authorize.
type fakeIdP struct {
	t   *testing.T
	srv *httptest.Server
	key *rsa.PrivateKey
	kid string

	mu sync.Mutex
	// issuer is what the discovery document and id_tokens name; the server URL by default.
	issuer string
	// profile is the identity the next authorization grants: id_token claims and the userinfo response alike.
	profile map[string]any
	// idTokenClaims, when set, overrides claims of the next id_token only.
	idTokenClaims map[string]any
	// omitIDToken leaves the id_token out of the token response.
	omitIDToken bool
	// signingKey signs id_tokens instead of the published key, for forged tokens.
	signingKey *rsa.PrivateKey
	// userInfoStatus overrides the status of the userinfo and GitHub user endpoints.
	userInfoStatus int
	// githubEmails is the /user/emails answer; githubEmailsStatus overrides its status.
	githubEmails       []map[string]any
	githubEmailsStatus int

	grants map[string]fakeGrant
	tokens map[string]map[string]any
	// tokenRequests records the redirect_uri each token request carried.
	tokenRequests []string
}

type fakeGrant struct {
	redirectURI string
	challenge   string
	nonce       string
	profile     map[string]any
	idToken     map[string]any
}

func newFakeIdP(t *testing.T) *fakeIdP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeIdP{t: t, key: key, kid: "key-1", grants: map[string]fakeGrant{}, tokens: map[string]map[string]any{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /jwks", f.serveJWKS)
	mux.HandleFunc("POST /token", f.serveToken)
	mux.HandleFunc("GET /userinfo", f.serveUserInfo)
	mux.HandleFunc("GET /oidc/userinfo", f.serveUserInfo)
	mux.HandleFunc("GET /user", f.serveUserInfo)
	mux.HandleFunc("GET /user/emails", f.serveGitHubEmails)
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/.well-known/openid-configuration") {
			http.NotFound(w, r)
			return
		}
		f.serveDiscovery(w)
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	f.issuer = f.srv.URL
	return f
}

func (f *fakeIdP) URL() string { return f.srv.URL }

// set changes the fake's behaviour under its lock.
func (f *fakeIdP) set(fn func(f *fakeIdP)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

func (f *fakeIdP) serveDiscovery(w http.ResponseWriter) {
	f.mu.Lock()
	issuer := f.issuer
	f.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                issuer,
		"authorization_endpoint":                f.srv.URL + "/authorize",
		"token_endpoint":                        f.srv.URL + "/token",
		"jwks_uri":                              f.srv.URL + "/jwks",
		"userinfo_endpoint":                     f.srv.URL + "/userinfo",
		"id_token_signing_alg_values_supported": []string{"RS256"},
	})
}

func (f *fakeIdP) serveJWKS(w http.ResponseWriter, _ *http.Request) {
	pub := f.key.PublicKey
	writeJSON(w, http.StatusOK, map[string]any{"keys": []map[string]any{{
		"kty": "RSA", "use": "sig", "alg": "RS256", "kid": f.kid,
		"n": b64(pub.N.Bytes()), "e": b64(big.NewInt(int64(pub.E)).Bytes()),
	}}})
}

// authorize plays the user consenting at the provider: it checks the authorization request the server built and returns the code the provider would send back.
func (f *fakeIdP) authorize(authURL string) (code string, query url.Values) {
	f.t.Helper()
	u, err := url.Parse(authURL)
	if err != nil {
		f.t.Fatal(err)
	}
	q := u.Query()
	if q.Get("client_id") != fakeClientID || q.Get("response_type") != "code" || q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" || q.Get("state") == "" {
		f.t.Fatalf("authorization request %s", authURL)
	}
	code = "code-" + q.Get("state")[:12]
	f.mu.Lock()
	defer f.mu.Unlock()
	f.grants[code] = fakeGrant{redirectURI: q.Get("redirect_uri"), challenge: q.Get("code_challenge"), nonce: q.Get("nonce"), profile: f.profile, idToken: f.idTokenClaims}
	f.idTokenClaims = nil
	return code, q
}

func (f *fakeIdP) serveToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		return
	}
	id, secret, ok := r.BasicAuth()
	if !ok {
		id, secret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
	}
	if id != fakeClientID || secret != fakeClientSecret {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_client"})
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tokenRequests = append(f.tokenRequests, r.PostForm.Get("redirect_uri"))
	code := r.PostForm.Get("code")
	grant, ok := f.grants[code]
	delete(f.grants, code)
	sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
	if !ok || r.PostForm.Get("grant_type") != "authorization_code" || r.PostForm.Get("redirect_uri") != grant.redirectURI || b64(sum[:]) != grant.challenge {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_grant"})
		return
	}
	access := "access-" + code
	f.tokens[access] = grant.profile
	res := map[string]any{"access_token": access, "token_type": "Bearer", "expires_in": 3600}
	if !f.omitIDToken {
		claims := map[string]any{"iss": f.issuer, "aud": fakeClientID, "iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix()}
		if grant.nonce != "" {
			claims["nonce"] = grant.nonce
		}
		for k, v := range grant.profile {
			claims[k] = v
		}
		for k, v := range grant.idToken {
			if v == nil {
				delete(claims, k)
				continue
			}
			claims[k] = v
		}
		key := f.key
		if f.signingKey != nil {
			key = f.signingKey
		}
		res["id_token"] = f.sign(key, claims)
	}
	writeJSON(w, http.StatusOK, res)
}

func (f *fakeIdP) sign(key *rsa.PrivateKey, claims map[string]any) string {
	header, _ := json.Marshal(map[string]string{"alg": "RS256", "kid": f.kid, "typ": "JWT"})
	payload, err := json.Marshal(claims)
	if err != nil {
		f.t.Fatal(err)
	}
	input := b64(header) + "." + b64(payload)
	digest := sha256.Sum256([]byte(input))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		f.t.Fatal(err)
	}
	return input + "." + b64(sig)
}

// bearerProfile returns the profile an access token was issued for.
func (f *fakeIdP) bearerProfile(r *http.Request) (map[string]any, bool) {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		return nil, false
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.tokens[token]
	return p, ok
}

func (f *fakeIdP) serveUserInfo(w http.ResponseWriter, r *http.Request) {
	profile, ok := f.bearerProfile(r)
	f.mu.Lock()
	status := f.userInfoStatus
	f.mu.Unlock()
	switch {
	case !ok:
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_token"})
	case status != 0:
		writeJSON(w, status, map[string]string{"error": "unavailable"})
	default:
		writeJSON(w, http.StatusOK, profile)
	}
}

func (f *fakeIdP) serveGitHubEmails(w http.ResponseWriter, r *http.Request) {
	if _, ok := f.bearerProfile(r); !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "Bad credentials"})
		return
	}
	f.mu.Lock()
	status, emails := f.githubEmailsStatus, f.githubEmails
	f.mu.Unlock()
	if status != 0 {
		writeJSON(w, status, map[string]string{"message": "unavailable"})
		return
	}
	if emails == nil {
		emails = []map[string]any{}
	}
	writeJSON(w, http.StatusOK, emails)
}

// redirectURIs are the redirect_uri values token requests carried, in order.
func (f *fakeIdP) redirectURIs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.tokenRequests...)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
