package org

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/server/internal/authz"
	"github.com/yldm-tech/forma/server/internal/authz/authztest"
	"github.com/yldm-tech/forma/server/internal/jsonschema"
	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/platform/audit"
	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/server/internal/platform/pgtest"
	"github.com/yldm-tech/forma/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/server/internal/storage"
	"github.com/yldm-tech/forma/server/internal/workspace"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	os.Exit(m.Run())
}

const webApp = "http://app.example.com"

// testUserHeader carries the acting user. headerSessions stands in for the credential module's sessions, which have their own tests; what is under test here is everything behind the actor.
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

// env is one isolated instance of the organization and workspace modules: a fresh database, real authorization, and the routes as the server mounts them.
type env struct {
	t      *testing.T
	cfg    config.Config
	db     *gorm.DB
	authz  *authztest.Env
	svc    *Service
	engine *gin.Engine
	logs   *syncBuffer
}

func newEnv(t *testing.T, configure ...func(*config.Config)) *env {
	t.Helper()
	t.Parallel()
	u, _ := url.Parse(webApp)
	cfg := config.Config{WebAppURL: u, RateLimitingDisabled: true, AuditLogEnabled: true, UserManagementMinimumRole: "manager", TrustedProxyHopCount: 1}
	for _, f := range configure {
		f(&cfg)
	}
	db := pgtest.GORM(t)
	e := &env{t: t, cfg: cfg, db: db, authz: authztest.New(t, cfg, db), logs: &syncBuffer{}}
	log := slog.New(slog.NewJSONHandler(e.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	schemas, err := jsonschema.New()
	if err != nil {
		t.Fatal(err)
	}
	limiter := ratelimit.New(cfg, nil, log)
	emitter := audit.New(cfg, log)
	wsRepo := workspace.NewRepository(db, log)
	// No bucket is configured, so deleting a workspace has no files to delete.
	bucket, err := storage.NewBucket(cfg, log)
	if err != nil {
		t.Fatal(err)
	}
	wsSvc := workspace.NewService(cfg, wsRepo, e.authz.Authorizer, limiter, emitter, schemas, storage.NewService(bucket), log)
	e.svc = NewService(cfg, NewRepository(db), wsRepo, wsSvc, e.authz.Authorizer, limiter, emitter, log)

	e.engine = gin.New()
	group := e.engine.Group("/api/app", httpapi.ErrorHandler(log), httpapi.CSRF(cfg), authz.DecisionMemo())
	NewHandler(e.svc, headerSessions{}).Register(group)
	workspace.NewHandler(wsSvc, headerSessions{}).Register(group)
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

func (e *env) scalar(dst any, query string, args ...any) {
	e.t.Helper()
	if err := e.db.Raw(query, args...).Row().Scan(dst); err != nil {
		e.t.Fatalf("query %q: %v", query, err)
	}
}

func (e *env) count(query string, args ...any) int {
	e.t.Helper()
	var n int
	e.scalar(&n, query, args...)
	return n
}

func (e *env) user() uuid.UUID {
	return e.insert(`INSERT INTO users (name, email) VALUES ('Ada', ?) RETURNING id`, uuid.NewString()+"@example.com")
}

// organization creates an organization through the API, as its owner.
func (e *env) organization(owner uuid.UUID, name string) Created {
	e.t.Helper()
	var c Created
	e.ok(e.do(owner, http.MethodPost, "/api/app/organizations", map[string]string{"name": name}), http.StatusCreated, &c)
	return c
}

func (e *env) member(org, user uuid.UUID, role string) {
	e.exec(`INSERT INTO memberships (user_id, organization_id, role, accepted) VALUES (?, ?, ?, true)`, user, org, role)
}

func (e *env) team(org uuid.UUID) uuid.UUID {
	return e.insert(`INSERT INTO teams (organization_id, name) VALUES (?, ?) RETURNING id`, org, "Team "+uuid.NewString())
}

// teamMember adds user to team as a contributor.
func (e *env) teamMember(team, user uuid.UUID) {
	e.exec(`INSERT INTO team_members (team_id, user_id, role) VALUES (?, ?, 'contributor')`, team, user)
}

func (e *env) workspaceTeam(ws, team uuid.UUID, permission string) {
	e.exec(`INSERT INTO workspace_teams (workspace_id, team_id, permission) VALUES (?, ?, ?)`, ws, team, permission)
}

func (e *env) survey(ws uuid.UUID) {
	e.exec(`INSERT INTO surveys (workspace_id, name) VALUES (?, 'Survey')`, ws)
}

// do sends a request as user (uuid.Nil for none) after delivering the projection, so every request sees what the test wrote before it.
func (e *env) do(user uuid.UUID, method, path string, body any) *httptest.ResponseRecorder {
	e.t.Helper()
	e.authz.Deliver(e.t)
	return e.doUndelivered(user, method, path, body)
}

// doUndelivered sends a request without delivering the projection first, as a request that arrives before the worker's next tick does.
func (e *env) doUndelivered(user uuid.UUID, method, path string, body any) *httptest.ResponseRecorder {
	e.t.Helper()
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			e.t.Fatal(err)
		}
		r = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, r)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Origin", webApp)
	req.Header.Set(httpapi.CSRFHeader, "XMLHttpRequest")
	if user != uuid.Nil {
		req.Header.Set(testUserHeader, user.String())
	}
	rec := httptest.NewRecorder()
	e.engine.ServeHTTP(rec, req)
	return rec
}

// ok asserts the status and decodes the body into out when given.
func (e *env) ok(rec *httptest.ResponseRecorder, status int, out any) {
	e.t.Helper()
	if rec.Code != status {
		e.t.Fatalf("status %d, want %d: %s", rec.Code, status, rec.Body.String())
	}
	if out != nil {
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			e.t.Fatalf("decode %s: %v", rec.Body.String(), err)
		}
	}
}

// fails asserts an error response with the status, code and, when given, message.
func (e *env) fails(rec *httptest.ResponseRecorder, status int, code, message string) {
	e.t.Helper()
	var body httpapi.Envelope
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Code != status || body.Code != code || (message != "" && body.Message != message) {
		e.t.Fatalf("got %d %s, want %d %s %q", rec.Code, rec.Body.String(), status, code, message)
	}
}

// auditEvents returns the audit records logged so far.
func (e *env) auditEvents() []map[string]any {
	var out []map[string]any
	for _, line := range strings.Split(e.logs.String(), "\n") {
		var rec map[string]any
		if json.Unmarshal([]byte(line), &rec) == nil && rec["msg"] == "audit" {
			out = append(out, rec)
		}
	}
	return out
}
