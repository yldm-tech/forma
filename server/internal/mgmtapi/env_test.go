package mgmtapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/server/internal/actionclass"
	"github.com/yldm-tech/forma/server/internal/apikey"
	"github.com/yldm-tech/forma/server/internal/authz/authztest"
	"github.com/yldm-tech/forma/server/internal/contact"
	"github.com/yldm-tech/forma/server/internal/jsonschema"
	"github.com/yldm-tech/forma/server/internal/mcp"
	"github.com/yldm-tech/forma/server/internal/mgmtapi/mgmtv1"
	"github.com/yldm-tech/forma/server/internal/oauthserver"
	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/platform/audit"
	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/server/internal/platform/httpserver"
	"github.com/yldm-tech/forma/server/internal/platform/jobs"
	"github.com/yldm-tech/forma/server/internal/platform/pgtest"
	"github.com/yldm-tech/forma/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/server/internal/platform/secret"
	"github.com/yldm-tech/forma/server/internal/platform/ssrf"
	"github.com/yldm-tech/forma/server/internal/platform/v3api"
	"github.com/yldm-tech/forma/server/internal/platform/valkey/valkeytest"
	"github.com/yldm-tech/forma/server/internal/response"
	"github.com/yldm-tech/forma/server/internal/storage"
	"github.com/yldm-tech/forma/server/internal/survey"
	"github.com/yldm-tech/forma/server/internal/survey/surveydoc"
	"github.com/yldm-tech/forma/server/internal/webhook"
	"github.com/yldm-tech/forma/server/internal/webui"
	"github.com/yldm-tech/forma/server/internal/workflow"
	"github.com/yldm-tech/forma/server/internal/workspace"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	os.Exit(m.Run())
}

// webAppURL is the WEBAPP_URL the recording ran with.
const webAppURL = "http://localhost:3999"

// testUserHeader carries the signed-in user of the routes that also accept a session; headerSessions stands in for the credential module's sessions, which have their own tests.
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

// publicResolver answers every host name with a public address, so webhook URLs validate without the network's DNS.
type publicResolver struct{}

func (publicResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
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

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// clock is the time the rate limiter and the handlers read; tests move it.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// nextWindow moves the clock to the start of the next rate-limit minute.
func (c *clock) nextWindow() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Truncate(time.Minute).Add(time.Minute)
}

// env is the management APIs on a fresh database, Valkey and SpiceDB, mounted as the server mounts them, with rate limits on and a clock the test moves.
type env struct {
	t      *testing.T
	cfg    config.Config
	db     *gorm.DB
	cache  *redis.Client
	authz  *authztest.Env
	clock  *clock
	engine *gin.Engine
	logs   *syncBuffer
}

func newEnv(t *testing.T) *env {
	t.Helper()
	t.Parallel()
	u, _ := url.Parse(webAppURL)
	s3, _ := url.Parse("http://localhost:9000")
	cfg := config.Config{
		WebAppURL: u, TrustedProxyHopCount: 1, AuditLogEnabled: true, UserManagementMinimumRole: "manager", SurveySchedulingTimeZone: "UTC",
		EncryptionKey: bytes.Repeat([]byte{7}, 32), EncryptionKeyText: config.Secret(strings.Repeat("07", 32)), EncryptionKeyVersion: 1,
		S3: config.S3Config{AccessKey: "contractfixtureaccess", SecretKey: "contractfixturesecret", Region: "us-east-1", EndpointURL: s3, ForcePathStyle: true, BucketName: "forma-uploads"},
	}
	db := pgtest.GORM(t)
	e := &env{t: t, cfg: cfg, db: db, cache: valkeytest.Client(t), authz: authztest.New(t, cfg, db), logs: &syncBuffer{}, clock: &clock{now: time.Now().Truncate(time.Minute)}}
	log := slog.New(slog.NewJSONHandler(e.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
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
	enqueuer, err := jobs.NewEnqueuer(log)
	if err != nil {
		t.Fatal(err)
	}
	bucket, err := storage.NewBucket(cfg, log)
	if err != nil {
		t.Fatal(err)
	}
	authorizer := e.authz.Authorizer
	limiter := ratelimit.New(cfg, e.cache, log).WithClock(e.clock.Now)
	emitter := audit.New(cfg, log)
	workspaces := workspace.NewRepository(db, log)
	keys := apikey.NewAuthenticator(db, log)
	surveys := survey.NewService(cfg, survey.NewRepository(db), authorizer, limiter, emitter, surveydoc.NewValidator(schemas), cipher, catalog, nil, log)
	responses := response.NewService(cfg, db, enqueuer, cipher, authorizer, limiter, emitter, log)
	contacts := contact.NewService(contact.NewRepository(db), authorizer, limiter, emitter, schemas, log)
	webhooks := webhook.NewService(webhook.NewRepository(db), authorizer, limiter, emitter, ssrf.New(cfg).WithResolver(publicResolver{}), log)
	v1 := mgmtv1.New(cfg, keys, limiter, emitter, authorizer, workspaces, log)
	h := NewHandler(cfg, db, e.cache, v1, keys, authorizer, limiter, emitter, workspaces, headerSessions{}, surveys, responses,
		actionclass.NewService(actionclass.NewRepository(db), authorizer, limiter, emitter), contacts, webhooks, storage.NewService(bucket), log)
	h.now = e.clock.Now
	web, err := webui.NewFromFS(fstest.MapFS{"index.html": {Data: []byte("<!doctype html>")}})
	if err != nil {
		t.Fatal(err)
	}
	// The MCP endpoint and the OAuth authorization server MCP clients discover were recorded with the management APIs; their fixtures are replayed here too.
	guard := v3api.NewGuard(cfg, headerSessions{}, keys, limiter, log)
	surveyHandler := survey.NewHandler(surveys, headerSessions{}, guard, limiter, log)
	workflows := workflow.NewHandler(cfg, workflow.NewService(workflow.NewRepository(db), db, authorizer, log), guard, headerSessions{}, emitter, log)
	signingKeys := oauthserver.NewKeys(db, cipher)
	oauth, err := oauthserver.NewHandler(cfg, db, signingKeys, headerSessions{}, limiter, log)
	if err != nil {
		t.Fatal(err)
	}
	mcpHandler, err := mcp.NewHandler(cfg, db, authorizer, keys, oauthserver.NewTokenVerifier(cfg, signingKeys), limiter, surveyHandler, workflows, log)
	if err != nil {
		t.Fatal(err)
	}
	e.engine = httpserver.NewEngine(log)
	e.engine.Use(httpapi.NewClientIPResolver(cfg, log).Middleware())
	webhook.NewV1Handler(webhooks, v1, log).Register(e.engine)
	h.Register(e.engine)
	mcpHandler.Register(e.engine)
	oauth.Register(e.engine)
	web.Register(e.engine)
	return e
}

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

// deliver projects the authorization rows the test or the API wrote into SpiceDB, as the worker would.
func (e *env) deliver() {
	e.t.Helper()
	e.authz.Deliver(e.t)
}

// user creates a user.
func (e *env) user(name, email string) uuid.UUID {
	return e.insert(`INSERT INTO users (name, email, email_verified, last_login_at) VALUES (?, ?, true, now()) RETURNING id`, name, email)
}

// workspace creates a workspace of org seeded as the workspace module seeds one: forma2's default attribute keys, in order, and an en-US language.
func (e *env) workspace(org uuid.UUID, name string) uuid.UUID {
	e.t.Helper()
	ws := e.insert(`INSERT INTO workspaces (organization_id, name) VALUES (?, ?) RETURNING id`, org, name)
	for i, k := range [][3]string{
		{"userId", "User Id", "The user id of a contact"}, {"email", "Email", "The email of a contact"},
		{"firstName", "First Name", "Your contact's first name"}, {"lastName", "Last Name", "Your contact's last name"},
		{"language", "Language", "The language preference of a contact"},
	} {
		e.exec(`INSERT INTO contact_attribute_keys (workspace_id, key, name, description, type, is_unique, created_at, updated_at) VALUES (?, ?, ?, ?, 'default', ?, now() + make_interval(secs => ?), now())`,
			ws, k[0], k[1], k[2], k[0] == "userId" || k[0] == "email", float64(i)/1000)
	}
	e.exec(`INSERT INTO languages (workspace_id, code) VALUES (?, 'en-US')`, ws)
	return ws
}

// apiKey creates an organization API key created by creator with the organization access and workspace grants given, and returns the key. Its hash has bcrypt's lowest cost, so the many requests of a test do not each spend a production-cost comparison.
func (e *env) apiKey(org, creator uuid.UUID, label string, read, write bool, grants ...any) (string, uuid.UUID) {
	e.t.Helper()
	s := strings.ReplaceAll(uuid.NewString(), "-", "") + strings.ReplaceAll(uuid.NewString(), "-", "")
	sum := sha256.Sum256([]byte(s))
	hashed, err := bcrypt.GenerateFromPassword([]byte(s), bcrypt.MinCost)
	if err != nil {
		e.t.Fatal(err)
	}
	access, _ := json.Marshal(map[string]any{"accessControl": map[string]bool{"read": read, "write": write}})
	id := e.insert(`INSERT INTO api_keys (organization_id, created_by, label, hashed_key, lookup_hash, organization_access) VALUES (?, ?, ?, ?, ?, ?::jsonb) RETURNING id`,
		org, creator, label, string(hashed), hex.EncodeToString(sum[:]), string(access))
	for i := 0; i+1 < len(grants); i += 2 {
		e.exec(`INSERT INTO api_key_workspaces (api_key_id, workspace_id, permission) VALUES (?, ?, ?)`, id, grants[i], grants[i+1])
	}
	return apikey.Prefix + s, id
}

// result is an answered request.
type result struct {
	Status int
	Header http.Header
	Body   []byte
}

func (r result) json(t *testing.T) map[string]any {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal(r.Body, &v); err != nil {
		t.Fatalf("body %s: %v", r.Body, err)
	}
	return v
}

// do sends a request; body is JSON-encoded unless it is a string, which is sent as it is.
func (e *env) do(method, path string, body any, headers ...string) result {
	e.t.Helper()
	var r io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		r = strings.NewReader(b)
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			e.t.Fatal(err)
		}
		r = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, r)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	rec := httptest.NewRecorder()
	e.engine.ServeHTTP(rec, req)
	return result{Status: rec.Code, Header: rec.Header(), Body: rec.Body.Bytes()}
}
