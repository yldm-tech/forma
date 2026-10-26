package mcp

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	jose "github.com/go-jose/go-jose/v4"
	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/ai"
	"github.com/yldm-tech/forma/apps/server/internal/analysis"
	"github.com/yldm-tech/forma/apps/server/internal/apikey"
	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/authz/authztest"
	"github.com/yldm-tech/forma/apps/server/internal/jsonschema"
	"github.com/yldm-tech/forma/apps/server/internal/oauthserver"
	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/audit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpserver"
	"github.com/yldm-tech/forma/apps/server/internal/platform/jobs"
	"github.com/yldm-tech/forma/apps/server/internal/platform/pgtest"
	"github.com/yldm-tech/forma/apps/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/secret"
	"github.com/yldm-tech/forma/apps/server/internal/platform/v3api"
	"github.com/yldm-tech/forma/apps/server/internal/platform/valkey/valkeytest"
	"github.com/yldm-tech/forma/apps/server/internal/survey"
	"github.com/yldm-tech/forma/apps/server/internal/survey/surveydoc"
	"github.com/yldm-tech/forma/apps/server/internal/workflow"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	os.Exit(m.Run())
}

// redirectURI is the loopback callback an MCP client listens on.
const redirectURI = "http://127.0.0.1:33418/callback"

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

// End is never reached: these tests do not sign a browser out through the authorization server's RP-initiated logout.
func (headerSessions) End(*gin.Context, uuid.UUID) error {
	return errors.New("headerSessions cannot end a session")
}

func (headerSessions) Optional() gin.HandlerFunc {
	return func(c *gin.Context) {
		if id, err := uuid.Parse(c.GetHeader(testUserHeader)); err == nil {
			httpapi.SetActor(c, httpapi.Actor{UserID: id})
		}
		c.Next()
	}
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// records returns the JSON log records with the given message.
func (b *syncBuffer) records(t *testing.T, msg string) []map[string]any {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []map[string]any
	for _, line := range bytes.Split(b.buf.Bytes(), []byte("\n")) {
		var rec map[string]any
		if json.Unmarshal(line, &rec) == nil && rec["msg"] == msg {
			out = append(out, rec)
		}
	}
	return out
}

// env is the server as `forma serve` mounts MCP: the survey and workflow modules, the authorization server and /api/mcp over a fresh database and real authorization, served on a loopback port as MCP clients reach it.
type env struct {
	t       *testing.T
	cfg     config.Config
	db      *gorm.DB
	authz   *authztest.Env
	limiter *ratelimit.Limiter
	logs    *syncBuffer
	server  *httptest.Server
	// browser follows no redirects, so each step of the authorization can be checked.
	browser *http.Client
}

type envOptions struct {
	// rateLimits charges real limits in Valkey, with the clock frozen inside one window.
	rateLimits bool
}

func newEnv(t *testing.T, opts ...envOptions) *env {
	t.Helper()
	t.Parallel()
	var o envOptions
	if len(opts) > 0 {
		o = opts[0]
	}
	server := httptest.NewUnstartedServer(nil)
	webApp, _ := url.Parse("http://" + server.Listener.Addr().String())
	cfg := config.Config{
		WebAppURL: webApp, RateLimitingDisabled: !o.rateLimits, AuditLogEnabled: true, UserManagementMinimumRole: "manager",
		EncryptionKey: bytes.Repeat([]byte{7}, 32), EncryptionKeyText: config.Secret(strings.Repeat("07", 32)), EncryptionKeyVersion: 1, SurveySchedulingTimeZone: "Europe/Berlin",
	}
	db := pgtest.GORM(t)
	e := &env{t: t, cfg: cfg, db: db, authz: authztest.New(t, cfg, db), logs: &syncBuffer{}, server: server}
	log := slog.New(slog.NewJSONHandler(e.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	e.limiter = ratelimit.New(cfg, nil, log)
	if o.rateLimits {
		frozen := time.Now()
		e.limiter = ratelimit.New(cfg, valkeytest.Client(t), log).WithClock(func() time.Time { return frozen })
	}
	cipher, err := secret.NewCipher(cfg)
	if err != nil {
		t.Fatal(err)
	}
	schemas, err := jsonschema.New()
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := survey.LoadCatalog()
	if err != nil {
		t.Fatal(err)
	}
	keys := apikey.NewAuthenticator(db, log)
	emitter := audit.New(cfg, log)
	guard := v3api.NewGuard(cfg, headerSessions{}, keys, e.limiter, log)
	aiService := ai.NewService(ai.NewClient(cfg, log), ai.NewRepository(db), e.authz.Authorizer, log)
	surveys := survey.NewHandler(survey.NewService(cfg, survey.NewRepository(db), e.authz.Authorizer, e.limiter, emitter, surveydoc.NewValidator(schemas), cipher, catalog, aiService, log), headerSessions{}, guard, e.limiter, log)
	workflows := workflow.NewHandler(cfg, workflow.NewService(workflow.NewRepository(db), db, e.authz.Authorizer, nil, nil, log), guard, headerSessions{}, emitter, log)
	signingKeys := oauthserver.NewKeys(db, cipher)
	oauth, err := oauthserver.NewHandler(cfg, db, signingKeys, headerSessions{}, e.limiter, log)
	if err != nil {
		t.Fatal(err)
	}
	enqueuer, err := jobs.NewEnqueuer(log)
	if err != nil {
		t.Fatal(err)
	}
	analyses := analysis.NewService(cfg, db, e.authz.Authorizer, enqueuer, emitter, log)
	mcp, err := NewHandler(cfg, db, e.authz.Authorizer, keys, oauthserver.NewTokenVerifier(cfg, signingKeys), e.limiter, surveys, workflows, analyses, log)
	if err != nil {
		t.Fatal(err)
	}
	engine := httpserver.NewEngine(cfg, log)
	oauthserver.NewAppHandler(oauth, headerSessions{}).Register(engine.Group("/api/app", httpapi.ErrorHandler(log), httpapi.CSRF(cfg), authz.DecisionMemo()))
	mcp.Register(engine)
	oauth.Register(engine)
	surveys.RegisterV3(engine)
	server.Config.Handler = engine
	server.Start()
	t.Cleanup(server.Close)
	e.browser = &http.Client{Transport: server.Client().Transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return e
}

func (e *env) url(path string) string { return e.server.URL + path }

func (e *env) exec(query string, args ...any) {
	e.t.Helper()
	if err := e.db.Exec(query, args...).Error; err != nil {
		e.t.Fatalf("exec %q: %v", query, err)
	}
}

func (e *env) insert(query string, args ...any) uuid.UUID {
	e.t.Helper()
	var id uuid.UUID
	if err := e.db.Raw(query, args...).Row().Scan(&id); err != nil {
		e.t.Fatalf("insert %q: %v", query, err)
	}
	return id
}

func (e *env) count(query string, args ...any) int {
	e.t.Helper()
	var n int
	if err := e.db.Raw(query, args...).Row().Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

// fixture is an organization owned by owner with two workspaces, a survey in the first, and an outsider who belongs to another organization.
type fixture struct {
	org, ws, ws2, survey, owner, outsider uuid.UUID
}

const blocks = `[{"id":"b1","name":"Block","elements":[{"id":"q1","type":"openText","headline":{"default":"Your email?"},"required":false,"inputType":"email"}]}]`

func (e *env) fixture() fixture {
	e.t.Helper()
	org := e.insert(`INSERT INTO organizations (name) VALUES ('Acme') RETURNING id`)
	ws := e.insert(`INSERT INTO workspaces (organization_id, name) VALUES (?, 'Main') RETURNING id`, org)
	ws2 := e.insert(`INSERT INTO workspaces (organization_id, name) VALUES (?, 'Beta') RETURNING id`, org)
	sv := e.insert(`INSERT INTO surveys (workspace_id, name, type, status, blocks) VALUES (?, 'Feedback', 'link', 'inProgress', ?::jsonb) RETURNING id`, ws, blocks)
	owner := e.insert(`INSERT INTO users (name, email) VALUES ('Ada', 'ada@example.com') RETURNING id`)
	e.exec(`INSERT INTO memberships (user_id, organization_id, role, accepted) VALUES (?, ?, 'owner', true)`, owner, org)
	otherOrg := e.insert(`INSERT INTO organizations (name) VALUES ('Other') RETURNING id`)
	e.insert(`INSERT INTO workspaces (organization_id, name) VALUES (?, 'Elsewhere') RETURNING id`, otherOrg)
	outsider := e.insert(`INSERT INTO users (name, email) VALUES ('Eve', 'eve@example.com') RETURNING id`)
	e.exec(`INSERT INTO memberships (user_id, organization_id, role, accepted) VALUES (?, ?, 'owner', true)`, outsider, otherOrg)
	e.authz.Deliver(e.t)
	return fixture{org: org, ws: ws, ws2: ws2, survey: sv, owner: owner, outsider: outsider}
}

// legacyKey mints a key in forma2's first format, the SHA-256 of the whole key in both columns, which only x-api-key carries. It verifies without bcrypt, so tests that only need some key use it.
func (e *env) legacyKey(org, ws uuid.UUID, permission string) string {
	e.t.Helper()
	key := "test-key-" + uuid.NewString()
	sum := sha256.Sum256([]byte(key))
	digest := hex.EncodeToString(sum[:])
	id := e.insert(`INSERT INTO api_keys (organization_id, label, hashed_key, lookup_hash) VALUES (?, 'test', ?, ?) RETURNING id`, org, digest, digest)
	e.exec(`INSERT INTO api_key_workspaces (api_key_id, workspace_id, permission) VALUES (?, ?, ?)`, id, ws, permission)
	e.authz.Deliver(e.t)
	return key
}

// apiKey mints a current-format key of org with a permission on ws.
func (e *env) apiKey(org, ws uuid.UUID, permission string) string {
	e.t.Helper()
	s, err := secret.NewToken()
	if err != nil {
		e.t.Fatal(err)
	}
	hashed, err := secret.HashPassword(s)
	if err != nil {
		e.t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(s))
	id := e.insert(`INSERT INTO api_keys (organization_id, label, hashed_key, lookup_hash) VALUES (?, 'test', ?, ?) RETURNING id`, org, hashed, hex.EncodeToString(sum[:]))
	e.exec(`INSERT INTO api_key_workspaces (api_key_id, workspace_id, permission) VALUES (?, ?, ?)`, id, ws, permission)
	e.authz.Deliver(e.t)
	return apikey.Prefix + s
}

// reply is a response whose body was read and closed.
type reply struct {
	*http.Response
	body []byte
}

func (e *env) get(client *http.Client, target string, headers map[string]string) reply {
	e.t.Helper()
	return e.send(client, http.MethodGet, target, "", headers)
}

func (e *env) send(client *http.Client, method, target, body string, headers map[string]string) reply {
	e.t.Helper()
	req, err := http.NewRequest(method, target, strings.NewReader(body))
	if err != nil {
		e.t.Fatal(err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return e.do(client, req)
}

func (e *env) do(client *http.Client, req *http.Request) reply {
	e.t.Helper()
	res, err := client.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		e.t.Fatal(err)
	}
	return reply{Response: res, body: raw}
}

func readJSON(t *testing.T, res reply) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(res.body, &out); err != nil {
		t.Fatalf("%d %s: %v", res.StatusCode, res.body, err)
	}
	return out
}

// signIn is the browser of user through an authorization: op's redirect to the continue step, the consent page and its API, op's callback, and the redirect back to the client. It returns the code, as the client's loopback listener would receive it.
func (e *env) signIn(user uuid.UUID) auth.AuthorizationCodeFetcher {
	return func(_ context.Context, args *auth.AuthorizationArgs) (*auth.AuthorizationResult, error) {
		next, err := e.redirect(args.URL, nil)
		if err != nil {
			return nil, err
		}
		if next, err = e.redirect(next, map[string]string{testUserHeader: user.String()}); err != nil {
			return nil, err
		}
		page, _ := url.Parse(next)
		if page.Path == "/app/account/authorize" {
			requestID := page.Query().Get("request_id")
			view := e.get(e.browser, e.url("/api/app/oauth/authorization-requests/"+requestID), map[string]string{testUserHeader: user.String()})
			if view.StatusCode != http.StatusOK {
				return nil, fmt.Errorf("authorization request: %d", view.StatusCode)
			}
			res := e.send(e.browser, http.MethodPost, e.url("/api/app/oauth/authorization-requests/"+requestID+"/consent"), `{"accept":true}`, map[string]string{
				testUserHeader: user.String(), "Origin": e.server.URL, "X-Requested-With": "XMLHttpRequest", "Content-Type": "application/json",
			})
			out := readJSON(e.t, res)
			if res.StatusCode != http.StatusOK || out["redirect"] != true {
				return nil, fmt.Errorf("consent: %d %v", res.StatusCode, out)
			}
			next = out["url"].(string)
		}
		// The callback hands the code only to the browser session that approved the request.
		back, err := e.redirect(next, map[string]string{testUserHeader: user.String()})
		if err != nil {
			return nil, err
		}
		u, _ := url.Parse(back)
		if !strings.HasPrefix(back, redirectURI) || u.Query().Get("code") == "" {
			return nil, fmt.Errorf("authorization ended at %s", back)
		}
		return &auth.AuthorizationResult{Code: u.Query().Get("code"), State: u.Query().Get("state")}, nil
	}
}

// redirect requests target and returns where it redirects.
func (e *env) redirect(target string, headers map[string]string) (string, error) {
	res := e.get(e.browser, target, headers)
	if res.StatusCode != http.StatusFound {
		return "", fmt.Errorf("%s answered %d %s", target, res.StatusCode, res.body)
	}
	loc, err := res.Location()
	if err != nil {
		return "", err
	}
	return loc.String(), nil
}

// accessToken registers a client and runs the authorization for user, returning the access token the client receives.
func (e *env) accessToken(user uuid.UUID, scope string) string {
	e.t.Helper()
	reg := readJSON(e.t, e.send(e.browser, http.MethodPost, e.url("/api/auth/oauth2/register"), `{"client_name":"Test client","redirect_uris":["`+redirectURI+`"],"token_endpoint_auth_method":"none","grant_types":["authorization_code","refresh_token"]}`, map[string]string{"Content-Type": "application/json"}))
	clientID, _ := reg["client_id"].(string)
	verifier := uuid.NewString() + uuid.NewString()
	sum := sha256.Sum256([]byte(verifier))
	q := url.Values{
		"client_id": {clientID}, "response_type": {"code"}, "redirect_uri": {redirectURI}, "scope": {scope}, "state": {"st"},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"}, "resource": {e.url("/api/mcp")},
	}
	res, err := e.signIn(user)(context.Background(), &auth.AuthorizationArgs{URL: e.url("/api/auth/oauth2/authorize?" + q.Encode())})
	if err != nil {
		e.t.Fatal(err)
	}
	form := url.Values{"grant_type": {"authorization_code"}, "code": {res.Code}, "redirect_uri": {redirectURI}, "client_id": {clientID}, "code_verifier": {verifier}}
	out := readJSON(e.t, e.send(e.browser, http.MethodPost, e.url("/api/auth/oauth2/token"), form.Encode(), map[string]string{"Content-Type": "application/x-www-form-urlencoded"}))
	token, ok := out["access_token"].(string)
	if !ok {
		e.t.Fatalf("token: %v", out)
	}
	return token
}

// sign signs claims as the authorization server would, with its own key, for tokens it would never issue.
func (e *env) sign(claims map[string]any) string {
	e.t.Helper()
	var row struct {
		ID         uuid.UUID
		PrivateKey []byte
	}
	if err := e.db.Raw(`SELECT id, private_key FROM oauth_signing_keys ORDER BY created_at DESC LIMIT 1`).Scan(&row).Error; err != nil || row.ID == uuid.Nil {
		e.t.Fatalf("signing key: %v", err)
	}
	cipher, err := secret.NewCipher(e.cfg)
	if err != nil {
		e.t.Fatal(err)
	}
	seed, err := cipher.Open(row.PrivateKey, []byte("oauth_signing_keys.private_key"))
	if err != nil {
		e.t.Fatal(err)
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.EdDSA, Key: jose.JSONWebKey{Key: ed25519.NewKeyFromSeed(seed), KeyID: row.ID.String()}}, (&jose.SignerOptions{}).WithType("at+jwt"))
	if err != nil {
		e.t.Fatal(err)
	}
	payload, _ := json.Marshal(claims)
	jws, err := signer.Sign(payload)
	if err != nil {
		e.t.Fatal(err)
	}
	out, _ := jws.CompactSerialize()
	return out
}
