package workspace

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"net/url"
	"os"
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
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	os.Exit(m.Run())
}

const webApp = "http://app.example.com"

// testUserHeader carries the acting user. headerSessions stands in for the credential module's sessions, which have their own tests.
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

func (headerSessions) Optional() gin.HandlerFunc { return func(c *gin.Context) { c.Next() } }

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

// env is one isolated instance of the workspace module: a fresh database, real authorization, and its routes as the server mounts them.
type env struct {
	t      *testing.T
	db     *gorm.DB
	authz  *authztest.Env
	repo   *Repository
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
	e := &env{t: t, db: db, authz: authztest.New(t, cfg, db), logs: &syncBuffer{}}
	log := slog.New(slog.NewJSONHandler(e.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	schemas, err := jsonschema.New()
	if err != nil {
		t.Fatal(err)
	}
	e.repo = NewRepository(db, log)
	e.svc = NewService(cfg, e.repo, e.authz.Authorizer, ratelimit.New(cfg, nil, log), audit.New(cfg, log), schemas, log)
	e.engine = gin.New()
	group := e.engine.Group("/api/app", httpapi.ErrorHandler(log), httpapi.CSRF(cfg), authz.DecisionMemo())
	NewHandler(e.svc, headerSessions{}).Register(group)
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
	if err := e.db.Raw(query, args...).Row().Scan(&n); err != nil {
		e.t.Fatalf("query %q: %v", query, err)
	}
	return n
}

func (e *env) user() uuid.UUID {
	return e.insert(`INSERT INTO users (name, email) VALUES ('Ada', ?) RETURNING id`, uuid.NewString()+"@example.com")
}

// organization creates an organization with the owner and its default workspace, as the organization module does.
func (e *env) organization(owner uuid.UUID) (org, ws uuid.UUID) {
	e.t.Helper()
	org = e.insert(`INSERT INTO organizations (name) VALUES ('Acme') RETURNING id`)
	e.member(org, owner, "owner")
	return org, e.workspace(org, DefaultName)
}

// workspace creates a workspace with the module's own seeding.
func (e *env) workspace(org uuid.UUID, name string) uuid.UUID {
	e.t.Helper()
	var id uuid.UUID
	err := e.repo.transaction(e.t.Context(), func(tx *Repository) error {
		var err error
		id, err = tx.Create(e.t.Context(), org, NewWorkspace{Name: name})
		return err
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return id
}

func (e *env) member(org, user uuid.UUID, role string) {
	e.exec(`INSERT INTO memberships (user_id, organization_id, role, accepted) VALUES (?, ?, ?, true)`, user, org, role)
}

// grant puts user in a new team of org with access to ws.
func (e *env) grant(org, ws, user uuid.UUID, permission string) {
	e.t.Helper()
	team := e.insert(`INSERT INTO teams (organization_id, name) VALUES (?, ?) RETURNING id`, org, "Team "+uuid.NewString())
	e.exec(`INSERT INTO team_members (team_id, user_id, role) VALUES (?, ?, 'contributor')`, team, user)
	e.exec(`INSERT INTO workspace_teams (workspace_id, team_id, permission) VALUES (?, ?, ?)`, ws, team, permission)
}

func (e *env) survey(ws uuid.UUID) {
	e.exec(`INSERT INTO surveys (workspace_id, name) VALUES (?, 'Survey')`, ws)
}

// do sends a request as user after delivering the projection.
func (e *env) do(user uuid.UUID, method, path string, body any) *httptest.ResponseRecorder {
	e.t.Helper()
	e.authz.Deliver(e.t)
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
	req.Header.Set(testUserHeader, user.String())
	rec := httptest.NewRecorder()
	e.engine.ServeHTTP(rec, req)
	return rec
}

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

func (e *env) fails(rec *httptest.ResponseRecorder, status int, code, message string) httpapi.Envelope {
	e.t.Helper()
	var body httpapi.Envelope
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Code != status || body.Code != code || (message != "" && body.Message != message) {
		e.t.Fatalf("got %d %s, want %d %s %q", rec.Code, rec.Body.String(), status, code, message)
	}
	return body
}

func wsPath(id uuid.UUID, rest string) string {
	return "/api/app/workspaces/" + id.String() + rest
}

func orgWorkspaces(org uuid.UUID, query string) string {
	return "/api/app/organizations/" + org.String() + "/workspaces" + query
}
