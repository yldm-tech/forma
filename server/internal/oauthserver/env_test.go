package oauthserver

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/server/internal/platform/httpserver"
	"github.com/yldm-tech/forma/server/internal/platform/pgtest"
	"github.com/yldm-tech/forma/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/server/internal/platform/secret"
	"github.com/yldm-tech/forma/server/internal/platform/valkey/valkeytest"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	os.Exit(m.Run())
}

const (
	webApp      = "http://app.example.test"
	issuer      = webApp + "/api/auth"
	resource    = webApp + "/api/mcp"
	redirectURI = "http://127.0.0.1:33418/callback"
)

// testUserHeader carries the signed-in user; headerSessions stands in for the credential module's sessions, which have their own tests.
const testUserHeader = "X-Test-User"

type headerSessions struct{}

func (headerSessions) Require() gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(c.GetHeader(testUserHeader))
		if err != nil {
			httpapi.Fail(c, apperr.NotAuthenticated("Not authenticated"))
			return
		}
		httpapi.SetActor(c, httpapi.Actor{UserID: id})
		c.Next()
	}
}

func (headerSessions) Optional() gin.HandlerFunc {
	return func(c *gin.Context) {
		if id, err := uuid.Parse(c.GetHeader(testUserHeader)); err == nil {
			httpapi.SetActor(c, httpapi.Actor{UserID: id})
		}
		c.Next()
	}
}

// env is one authorization server on a fresh database, served in-process.
type env struct {
	t        *testing.T
	cfg      config.Config
	db       *gorm.DB
	keys     *Keys
	limiter  *ratelimit.Limiter
	handler  *Handler
	verifier *TokenVerifier
	engine   *gin.Engine
}

// newEnv builds the server; with rateLimits its limits are charged in Valkey, with the clock frozen inside one window.
func newEnv(t *testing.T, rateLimits ...bool) *env {
	t.Helper()
	t.Parallel()
	limited := len(rateLimits) > 0 && rateLimits[0]
	u, _ := url.Parse(webApp)
	cfg := config.Config{WebAppURL: u, RateLimitingDisabled: !limited, EncryptionKey: bytes.Repeat([]byte{7}, 32), EncryptionKeyVersion: 1}
	db := pgtest.GORM(t)
	cipher, err := secret.NewCipher(cfg)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	e := &env{t: t, cfg: cfg, db: db, keys: NewKeys(db, cipher), limiter: ratelimit.New(cfg, nil, log)}
	if limited {
		frozen := time.Now()
		e.limiter = ratelimit.New(cfg, valkeytest.Client(t), log).WithClock(func() time.Time { return frozen })
	}
	if e.handler, err = NewHandler(cfg, db, e.keys, headerSessions{}, e.limiter, log); err != nil {
		t.Fatal(err)
	}
	e.verifier = NewTokenVerifier(cfg, e.keys)
	e.engine = httpserver.NewEngine(log)
	e.handler.Register(e.engine)
	NewAppHandler(e.handler, headerSessions{}).Register(e.engine.Group("/api/app", httpapi.ErrorHandler(log), httpapi.CSRF(cfg)))
	return e
}

func (e *env) do(method, target string, body io.Reader, headers map[string]string) *httptest.ResponseRecorder {
	e.t.Helper()
	req := httptest.NewRequest(method, target, body)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	e.engine.ServeHTTP(rec, req)
	return rec
}

func (e *env) json(rec *httptest.ResponseRecorder) map[string]any {
	e.t.Helper()
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		e.t.Fatalf("%d %s: %v", rec.Code, rec.Body.String(), err)
	}
	return out
}

func (e *env) user(email string) uuid.UUID {
	e.t.Helper()
	var id uuid.UUID
	if err := e.db.Raw(`INSERT INTO users (name, email) VALUES ('Ada', ?) RETURNING id`, email).Row().Scan(&id); err != nil {
		e.t.Fatal(err)
	}
	return id
}

func (e *env) count(query string) int {
	e.t.Helper()
	var n int
	if err := e.db.Raw(query).Row().Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

// register registers a client and returns the response.
func (e *env) register(body any) *httptest.ResponseRecorder {
	e.t.Helper()
	raw, _ := json.Marshal(body)
	return e.do(http.MethodPost, "/api/auth/oauth2/register", bytes.NewReader(raw), map[string]string{"Content-Type": "application/json"})
}

// publicClient registers a native public client with the loopback redirect URI, as MCP clients do, and returns its id.
func (e *env) publicClient() string {
	e.t.Helper()
	rec := e.register(map[string]any{
		"client_name": "Claude", "redirect_uris": []string{redirectURI}, "grant_types": []string{"authorization_code", "refresh_token"},
		"response_types": []string{"code"}, "token_endpoint_auth_method": "none",
	})
	if rec.Code != http.StatusCreated {
		e.t.Fatalf("register: %d %s", rec.Code, rec.Body.String())
	}
	return e.json(rec)["client_id"].(string)
}

// pkce is a code verifier and its S256 challenge.
type pkce struct{ verifier, challenge string }

func newPKCE() pkce {
	v := base64.RawURLEncoding.EncodeToString([]byte(uuid.NewString() + uuid.NewString()))
	sum := sha256.Sum256([]byte(v))
	return pkce{verifier: v, challenge: base64.RawURLEncoding.EncodeToString(sum[:])}
}

// authorizeQuery is an authorization request for clientID with PKCE.
func authorizeQuery(clientID string, p pkce, scope string) url.Values {
	return url.Values{
		"client_id": {clientID}, "response_type": {"code"}, "redirect_uri": {redirectURI}, "scope": {scope}, "state": {"st"},
		"code_challenge": {p.challenge}, "code_challenge_method": {"S256"}, "resource": {resource},
	}
}

func location(t *testing.T, rec *httptest.ResponseRecorder) *url.URL {
	t.Helper()
	if rec.Code != http.StatusFound {
		t.Fatalf("expected a redirect, got %d %s", rec.Code, rec.Body.String())
	}
	u, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// pathOf is a URL's path and query on this server.
func pathOf(u *url.URL) string {
	return u.RequestURI()
}

// startAuthorization sends the authorization request and follows op to the continue step; it returns where continuing as user leads.
func (e *env) startAuthorization(q url.Values, user uuid.UUID) *url.URL {
	e.t.Helper()
	cont := location(e.t, e.do(http.MethodGet, "/api/auth/oauth2/authorize?"+q.Encode(), nil, nil))
	if cont.Path != "/api/auth/oauth2/continue" {
		e.t.Fatalf("authorize led to %s", cont)
	}
	headers := map[string]string{}
	if user != uuid.Nil {
		headers[testUserHeader] = user.String()
	}
	return location(e.t, e.do(http.MethodGet, pathOf(cont), nil, headers))
}

// appHeaders are a signed-in SPA request's.
func appHeaders(user uuid.UUID) map[string]string {
	return map[string]string{testUserHeader: user.String(), "Origin": webApp, "X-Requested-With": "XMLHttpRequest", "Content-Type": "application/json"}
}

// consent answers the consent page's request and returns where it sends the browser.
func (e *env) consent(requestID string, user uuid.UUID, accept bool) string {
	e.t.Helper()
	body := `{"accept":false}`
	if accept {
		body = `{"accept":true}`
	}
	rec := e.do(http.MethodPost, "/api/app/oauth/authorization-requests/"+requestID+"/consent", strings.NewReader(body), appHeaders(user))
	if rec.Code != http.StatusOK {
		e.t.Fatalf("consent: %d %s", rec.Code, rec.Body.String())
	}
	out := e.json(rec)
	if out["redirect"] != true {
		e.t.Fatalf("consent: %v", out)
	}
	return out["url"].(string)
}

// code runs the authorization through consent and returns the code the client receives.
func (e *env) code(q url.Values, user uuid.UUID) string {
	e.t.Helper()
	next := e.startAuthorization(q, user)
	if next.Path == consentPath {
		cb, _ := url.Parse(e.consent(next.Query().Get("request_id"), user, true))
		next = cb
	}
	back := location(e.t, e.do(http.MethodGet, pathOf(next), nil, nil))
	if back.String()[:len(redirectURI)] != redirectURI || back.Query().Get("state") != "st" || back.Query().Get("code") == "" {
		e.t.Fatalf("callback redirected to %s", back)
	}
	return back.Query().Get("code")
}

func (e *env) token(form url.Values) (int, map[string]any) {
	e.t.Helper()
	rec := e.do(http.MethodPost, "/api/auth/oauth2/token", strings.NewReader(form.Encode()), map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
	return rec.Code, e.json(rec)
}

// tokens runs the whole authorization for user and redeems the code.
func (e *env) tokens(clientID string, user uuid.UUID, scope string) map[string]any {
	e.t.Helper()
	p := newPKCE()
	code := e.code(authorizeQuery(clientID, p, scope), user)
	status, out := e.token(url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirectURI}, "client_id": {clientID}, "code_verifier": {p.verifier}, "resource": {resource}})
	if status != http.StatusOK {
		e.t.Fatalf("token: %d %v", status, out)
	}
	return out
}

func (e *env) verify(token string) (*AccessTokenClaims, error) {
	return e.verifier.Verify(context.Background(), token)
}

func configFor(t *testing.T, webApp, betterAuth string) config.Config {
	t.Helper()
	u, err := url.Parse(webApp)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{WebAppURL: u}
	if betterAuth != "" {
		if cfg.SSO.BetterAuthURL, err = url.Parse(betterAuth); err != nil {
			t.Fatal(err)
		}
	}
	return cfg
}
