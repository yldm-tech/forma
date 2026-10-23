package apikey

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/server/internal/auth"
	"github.com/yldm-tech/forma/server/internal/authz"
	"github.com/yldm-tech/forma/server/internal/authz/spicedbtest"
	"github.com/yldm-tech/forma/server/internal/jsonschema"
	"github.com/yldm-tech/forma/server/internal/platform/audit"
	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/server/internal/platform/pgtest"
	"github.com/yldm-tech/forma/server/internal/platform/secret"
)

const webApp = "http://app.example.com"

// syncBuffer lets handlers log while the test reads.
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

// records returns the JSON log records with the given message.
func (b *syncBuffer) records(t *testing.T, msg string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range bytes.Split([]byte(b.String()), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal(line, &rec); err != nil {
			t.Fatalf("log line is not JSON: %s", line)
		}
		if rec["msg"] == msg {
			out = append(out, rec)
		}
	}
	return out
}

// dbEnv is a fresh database and an Authenticator on it, for the authentication tests that need no SpiceDB.
type dbEnv struct {
	t    *testing.T
	ctx  context.Context
	db   *gorm.DB
	logs *syncBuffer
	log  *slog.Logger
	repo *Repository
	auth *Authenticator

	mu    sync.Mutex
	clock time.Time
}

func newDBEnv(t *testing.T) *dbEnv {
	t.Helper()
	e := &dbEnv{t: t, ctx: context.Background(), db: pgtest.GORM(t), logs: &syncBuffer{}, clock: time.Now().UTC().Truncate(time.Microsecond)}
	e.log = slog.New(slog.NewJSONHandler(e.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	e.repo = NewRepository(e.db)
	e.auth = NewAuthenticator(e.db, e.log)
	e.auth.now = e.now
	return e
}

func (e *dbEnv) now() time.Time {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.clock
}

func (e *dbEnv) exec(query string, args ...any) {
	e.t.Helper()
	if err := e.db.Exec(query, args...).Error; err != nil {
		e.t.Fatalf("exec %q: %v", query, err)
	}
}

func (e *dbEnv) insert(query string, args ...any) uuid.UUID {
	e.t.Helper()
	var id string
	if err := e.db.Raw(query, args...).Row().Scan(&id); err != nil {
		e.t.Fatalf("insert %q: %v", query, err)
	}
	return uuid.MustParse(id)
}

func (e *dbEnv) org() uuid.UUID {
	return e.insert(`INSERT INTO organizations (name) VALUES ('Acme') RETURNING id`)
}

func (e *dbEnv) user() uuid.UUID {
	return e.insert(`INSERT INTO users (name, email) VALUES ('Ada', ?) RETURNING id`, uuid.NewString()+"@example.com")
}

func (e *dbEnv) member(org, user uuid.UUID, role string) {
	e.exec(`INSERT INTO memberships (organization_id, user_id, role, accepted) VALUES (?, ?, ?, true)`, org, user, role)
}

func (e *dbEnv) workspace(org uuid.UUID, name string) uuid.UUID {
	return e.insert(`INSERT INTO workspaces (organization_id, name) VALUES (?, ?) RETURNING id`, org, name)
}

// v2Key stores a current-format key the way Create does and returns its id and plaintext.
func (e *dbEnv) v2Key(org uuid.UUID, access OrganizationAccess, grants ...grantInput) (uuid.UUID, string) {
	e.t.Helper()
	k, err := newKey()
	if err != nil {
		e.t.Fatal(err)
	}
	raw, _ := json.Marshal(access)
	id, _, err := e.repo.create(e.ctx, newKeyRow{OrganizationID: org, CreatedBy: e.user(), Label: "key", HashedKey: k.HashedKey, LookupHash: k.LookupHash, OrganizationAccess: raw, Grants: grants})
	if err != nil {
		e.t.Fatal(err)
	}
	return id, k.Key
}

// legacyKey stores a pre-bcrypt key as the forma2 migration carries it over: the SHA-256 of the whole raw key in both hash columns.
func (e *dbEnv) legacyKey(org uuid.UUID, raw string, grants ...grantInput) uuid.UUID {
	e.t.Helper()
	digest := sha256Hex(raw)
	id, _, err := e.repo.create(e.ctx, newKeyRow{OrganizationID: org, CreatedBy: e.user(), Label: "legacy", HashedKey: digest, LookupHash: digest, OrganizationAccess: []byte(`{"accessControl":{"read":false,"write":false}}`), Grants: grants})
	if err != nil {
		e.t.Fatal(err)
	}
	return id
}

func (e *dbEnv) lastUsedAt(id uuid.UUID) *time.Time {
	e.t.Helper()
	var at []*time.Time
	if err := e.db.Raw(`SELECT last_used_at FROM api_keys WHERE id = ?`, id).Scan(&at).Error; err != nil || len(at) != 1 {
		e.t.Fatalf("read last_used_at: %v", err)
	}
	return at[0]
}

func (e *dbEnv) authenticate(headers map[string]string, opts Options) *Principal {
	e.t.Helper()
	h := http.Header{}
	for k, v := range headers {
		h.Set(k, v)
	}
	p, err := e.auth.Authenticate(e.ctx, h, opts)
	if err != nil {
		e.t.Fatalf("Authenticate: %v", err)
	}
	return p
}

// appEnv adds SpiceDB, the projection delivery, real sessions and the /api/app routes as the server mounts them.
type appEnv struct {
	*dbEnv
	cfg        config.Config
	authorizer *authz.Authorizer
	delivery   *authz.Delivery
	engine     *gin.Engine
}

var spicedbDefinitions = []string{"api_key", "organization", "team", "workspace", "survey", "dashboard", "response"}

func newAppEnv(t *testing.T) *appEnv {
	t.Helper()
	e := &appEnv{dbEnv: newDBEnv(t)}
	webURL, _ := url.Parse(webApp)
	e.cfg = config.Config{WebAppURL: webURL, TrustedProxyHopCount: 1, AuditLogEnabled: true, UserManagementMinimumRole: "manager", SessionMaxAge: 86400}

	schema, err := os.ReadFile("../../../authzed/schema.zed")
	if err != nil {
		t.Fatal(err)
	}
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	client, err := authz.NewClient(spicedbtest.Config(t, string(schema), spicedbDefinitions...), authz.RequestProfile, quiet)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	e.authorizer = authz.NewAuthorizer(e.cfg, e.db, client)
	e.delivery = authz.NewDelivery(e.db, client, quiet)

	schemas, err := jsonschema.New()
	if err != nil {
		t.Fatal(err)
	}
	sessions := auth.NewSessions(e.cfg, e.db, e.log)
	r := gin.New()
	r.Use(httpapi.NewClientIPResolver(e.cfg, e.log).Middleware())
	group := r.Group("/api/app", httpapi.ErrorHandler(e.log), httpapi.CSRF(e.cfg), authz.DecisionMemo())
	NewHandler(NewService(e.repo, e.authorizer, audit.New(e.cfg, e.log)), sessions, schemas).Register(group)
	e.engine = r
	return e
}

// drain delivers the authorization outbox to SpiceDB and fails the test unless everything was delivered.
func (e *appEnv) drain() {
	e.t.Helper()
	res, err := e.delivery.Drain(e.ctx, authz.DefaultDrainBatches)
	if err != nil {
		e.t.Fatalf("Drain: %v", err)
	}
	if res.Failed != 0 || res.Status != "drained" {
		e.t.Fatalf("Drain = %+v, want everything delivered", res)
	}
}

func (e *appEnv) can(actor authz.Actor, action authz.Action, resource authz.Resource) bool {
	e.t.Helper()
	ok, err := e.authorizer.Can(e.ctx, actor, action, resource)
	if err != nil {
		e.t.Fatalf("Can: %v", err)
	}
	return ok
}

// session signs a user in by storing a session the way the credential module does and returns its cookie token.
func (e *appEnv) session(user uuid.UUID) string {
	e.t.Helper()
	token, err := secret.NewToken()
	if err != nil {
		e.t.Fatal(err)
	}
	e.exec(`INSERT INTO sessions (user_id, token_hash, expires_at) VALUES (?, ?, ?)`, user, secret.HashToken(token), time.Now().Add(time.Hour))
	return token
}

type response struct {
	status int
	body   []byte
	header http.Header
}

func (r response) decode(t *testing.T, v any) {
	t.Helper()
	if err := json.Unmarshal(r.body, v); err != nil {
		t.Fatalf("decode %s: %v", r.body, err)
	}
}

func (r response) code(t *testing.T) string {
	t.Helper()
	var env httpapi.Envelope
	r.decode(t, &env)
	return env.Code
}

// do sends a request as the browser would: the session cookie, and on mutations the app's Origin and the CSRF header.
func (e *appEnv) do(method, path, token string, body any) response {
	e.t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			e.t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if method != http.MethodGet {
		req.Header.Set("Origin", webApp)
		req.Header.Set(httpapi.CSRFHeader, "XMLHttpRequest")
	}
	if token != "" {
		req.AddCookie(&http.Cookie{Name: "forma.session_token", Value: token})
	}
	w := httptest.NewRecorder()
	e.engine.ServeHTTP(w, req)
	return response{status: w.Code, body: w.Body.Bytes(), header: w.Header()}
}

func keysPath(org uuid.UUID) string {
	return "/api/app/organizations/" + org.String() + "/api-keys"
}

func keyPath(org, key uuid.UUID) string {
	return keysPath(org) + "/" + key.String()
}

func (e *appEnv) count(query string, args ...any) int {
	e.t.Helper()
	var n int
	if err := e.db.Raw(query, args...).Scan(&n).Error; err != nil {
		e.t.Fatalf("count: %v", err)
	}
	return n
}
