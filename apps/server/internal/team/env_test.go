package team

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
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/auth"
	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/authz/spicedbtest"
	"github.com/yldm-tech/forma/apps/server/internal/platform/audit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/apps/server/internal/platform/pgtest"
	"github.com/yldm-tech/forma/apps/server/internal/platform/secret"
)

const webApp = "http://forma.test"

// env is the team routes on a real PostgreSQL and SpiceDB, with real sessions. Nothing is shared between tests except the containers, so tests run in parallel.
type env struct {
	t          *testing.T
	cfg        config.Config
	db         *gorm.DB
	delivery   *authz.Delivery
	authorizer *authz.Authorizer
	svc        *Service
	logs       *syncBuffer
	engine     *gin.Engine
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

func newEnv(t *testing.T, configure ...func(*config.Config)) *env {
	t.Helper()
	gin.SetMode(gin.TestMode)
	webAppURL, _ := url.Parse(webApp)
	cfg := config.Config{WebAppURL: webAppURL, UserManagementMinimumRole: "manager", AuditLogEnabled: true, SessionMaxAge: 86400}
	for _, f := range configure {
		f(&cfg)
	}
	schema, err := os.ReadFile("../../authzed/schema.zed")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Authz = spicedbtest.Config(t, string(schema))
	db := pgtest.GORM(t)
	logs := &syncBuffer{}
	log := slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	client, err := authz.NewClient(cfg.Authz, authz.RequestProfile, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })

	e := &env{t: t, cfg: cfg, db: db, logs: logs, delivery: authz.NewDelivery(db, client, log)}
	e.authorizer = authz.NewAuthorizer(cfg, db, client)
	e.svc = NewService(NewRepository(db), e.authorizer, audit.New(cfg, log), log)
	r := gin.New()
	r.Use(httpapi.NewClientIPResolver(cfg, log).Middleware())
	group := r.Group("/api/app", httpapi.ErrorHandler(log), httpapi.CSRF(cfg), authz.DecisionMemo())
	NewHandler(e.svc, auth.NewSessions(cfg, db, log)).Register(group)
	e.engine = r
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

func (e *env) count(query string, args ...any) int {
	e.t.Helper()
	var n int
	if err := e.db.Raw(query, args...).Scan(&n).Error; err != nil {
		e.t.Fatalf("count %q: %v", query, err)
	}
	return n
}

func (e *env) org() uuid.UUID {
	return e.insert(`INSERT INTO organizations (name) VALUES ('Acme') RETURNING id`)
}

// user is a signed-in user: the returned session token goes in the request's cookie.
type user struct {
	id    uuid.UUID
	token string
}

func (e *env) user(name string) user {
	e.t.Helper()
	id := e.insert(`INSERT INTO users (name, email, email_verified) VALUES (?, ?, true) RETURNING id`, name, uuid.NewString()+"@example.com")
	token, err := secret.NewToken()
	if err != nil {
		e.t.Fatal(err)
	}
	e.exec(`INSERT INTO sessions (user_id, token_hash, expires_at) VALUES (?, ?, now() + interval '1 day')`, id, secret.HashToken(token))
	return user{id: id, token: token}
}

// member adds a user to an organization with a role.
func (e *env) member(org uuid.UUID, role string) user {
	u := e.user(role)
	e.exec(`INSERT INTO memberships (organization_id, user_id, role, accepted) VALUES (?, ?, ?, true)`, org, u.id, role)
	return u
}

func (e *env) team(org uuid.UUID, name string) uuid.UUID {
	return e.insert(`INSERT INTO teams (organization_id, name) VALUES (?, ?) RETURNING id`, org, name)
}

func (e *env) teamMember(team uuid.UUID, u user, role string) {
	e.exec(`INSERT INTO team_members (team_id, user_id, role) VALUES (?, ?, ?)`, team, u.id, role)
}

func (e *env) workspace(org uuid.UUID, name string) uuid.UUID {
	return e.insert(`INSERT INTO workspaces (organization_id, name) VALUES (?, ?) RETURNING id`, org, name)
}

func (e *env) workspaceTeam(workspace, team uuid.UUID, permission string) {
	e.exec(`INSERT INTO workspace_teams (workspace_id, team_id, permission) VALUES (?, ?, ?)`, workspace, team, permission)
}

// drain delivers the projection outbox to SpiceDB, as `forma worker` does every five seconds.
func (e *env) drain() {
	e.t.Helper()
	res, err := e.delivery.Drain(context.Background(), authz.DefaultDrainBatches)
	if err != nil || res.Failed != 0 || res.Status != "drained" {
		e.t.Fatalf("Drain = %+v, %v; want everything delivered", res, err)
	}
}

// can asks SpiceDB, through the authorizer, whether the user may act on a workspace.
func (e *env) can(u user, action string, workspace uuid.UUID) bool {
	e.t.Helper()
	ok, err := e.authorizer.Can(context.Background(), authz.User(u.id), authz.Action(action), authz.Workspace(workspace))
	if err != nil {
		e.t.Fatalf("Can: %v", err)
	}
	return ok
}

type response struct {
	*httptest.ResponseRecorder
}

func (r response) json() map[string]any {
	var m map[string]any
	_ = json.Unmarshal(r.Body.Bytes(), &m)
	return m
}

func (r response) code() string {
	c, _ := r.json()["code"].(string)
	return c
}

// do sends a request as the user the way the SPA does: session cookie, the app's Origin and the CSRF header.
func (e *env) do(u user, method, path string, body any) response {
	e.t.Helper()
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			e.t.Fatal(err)
		}
		reader = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, "/api/app"+path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Origin", webApp)
	req.Header.Set(httpapi.CSRFHeader, "XMLHttpRequest")
	if u.token != "" {
		req.AddCookie(&http.Cookie{Name: "forma.session_token", Value: u.token})
	}
	w := httptest.NewRecorder()
	e.engine.ServeHTTP(w, req)
	return response{w}
}

func expect(t *testing.T, r response, status int, code string) {
	t.Helper()
	if r.Code != status || (code != "" && r.code() != code) {
		t.Fatalf("got %d %s, want %d %s", r.Code, r.Body.String(), status, code)
	}
}

// auditEvents returns the audit records logged so far with the action and target.
func (e *env) auditEvents(action audit.Action, target audit.Target) []map[string]any {
	e.t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(e.logs.String()), "\n") {
		var m map[string]any
		if json.Unmarshal([]byte(line), &m) != nil {
			continue
		}
		tgt, _ := m["target"].(map[string]any)
		if m["msg"] == "audit" && m["action"] == string(action) && tgt["type"] == string(target) {
			out = append(out, m)
		}
	}
	return out
}
