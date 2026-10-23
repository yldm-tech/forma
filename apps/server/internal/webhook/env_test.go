package webhook

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
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
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/apikey"
	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/authz/authztest"
	"github.com/yldm-tech/forma/apps/server/internal/mgmtapi/mgmtv1"
	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/audit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpserver"
	"github.com/yldm-tech/forma/apps/server/internal/platform/pgtest"
	"github.com/yldm-tech/forma/apps/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/ssrf"
	"github.com/yldm-tech/forma/apps/server/internal/platform/valkey/valkeytest"
	"github.com/yldm-tech/forma/apps/server/internal/workspace"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	os.Exit(m.Run())
}

const webApp = "http://app.example.com"

// testUserHeader carries the acting user; headerSessions stands in for the credential module's sessions, which have their own tests.
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

// syncBuffer collects the log, audit events included, from concurrent requests.
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

// env is one isolated instance of the webhook module: a fresh database, real authorization and rate limits, its /api/app routes and the v1 API as the server mounts them.
type env struct {
	t      *testing.T
	db     *gorm.DB
	authz  *authztest.Env
	svc    *Service
	engine *gin.Engine
	logs   *syncBuffer
}

// newEnv builds the module. allowInternal is DANGEROUSLY_ALLOW_WEBHOOK_INTERNAL_URLS, which the tests that reach a local receiver need.
func newEnv(t *testing.T, allowInternal bool) *env {
	t.Helper()
	t.Parallel()
	app, _ := url.Parse(webApp)
	cfg := config.Config{WebAppURL: app, AuditLogEnabled: true, UserManagementMinimumRole: "manager", DangerouslyAllowWebhookInternalURLs: allowInternal}
	db := pgtest.GORM(t)
	e := &env{t: t, db: db, authz: authztest.New(t, cfg, db), logs: &syncBuffer{}}
	log := slog.New(slog.NewJSONHandler(e.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	frozen := time.Now()
	limiter := ratelimit.New(cfg, valkeytest.Client(t), log).WithClock(func() time.Time { return frozen })
	e.svc = NewService(NewRepository(db), e.authz.Authorizer, limiter, audit.New(cfg, log), ssrf.New(cfg), log)
	e.engine = httpserver.NewEngine(cfg, log)
	group := e.engine.Group("/api/app", httpapi.ErrorHandler(log), httpapi.CSRF(cfg), authz.DecisionMemo())
	NewHandler(e.svc, headerSessions{}).Register(group)
	v1 := mgmtv1.New(cfg, apikey.NewAuthenticator(db, log), limiter, audit.New(cfg, log), e.authz.Authorizer, workspace.NewRepository(db, log), log)
	NewV1Handler(e.svc, v1, log).Register(e.engine)
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

func (e *env) user() uuid.UUID {
	return e.insert(`INSERT INTO users (name, email) VALUES ('Ada', ?) RETURNING id`, uuid.NewString()+"@example.com")
}

// organization creates an organization owned by owner, with one workspace.
func (e *env) organization(owner uuid.UUID) (org, ws uuid.UUID) {
	org = e.insert(`INSERT INTO organizations (name) VALUES ('Acme') RETURNING id`)
	e.exec(`INSERT INTO memberships (user_id, organization_id, role, accepted) VALUES (?, ?, 'owner', true)`, owner, org)
	return org, e.workspace(org)
}

func (e *env) workspace(org uuid.UUID) uuid.UUID {
	return e.insert(`INSERT INTO workspaces (organization_id, name) VALUES (?, ?) RETURNING id`, org, "W "+uuid.NewString()[:8])
}

// grant makes user a member of org on a team with permission on ws.
func (e *env) grant(org, ws, user uuid.UUID, permission string) {
	e.exec(`INSERT INTO memberships (user_id, organization_id, role, accepted) VALUES (?, ?, 'member', true) ON CONFLICT DO NOTHING`, user, org)
	team := e.insert(`INSERT INTO teams (organization_id, name) VALUES (?, ?) RETURNING id`, org, "Team "+uuid.NewString())
	e.exec(`INSERT INTO team_members (team_id, user_id, role) VALUES (?, ?, 'contributor')`, team, user)
	e.exec(`INSERT INTO workspace_teams (workspace_id, team_id, permission) VALUES (?, ?, ?)`, ws, team, permission)
}

func (e *env) survey(ws uuid.UUID) uuid.UUID {
	return e.insert(`INSERT INTO surveys (workspace_id, name) VALUES (?, 'Survey') RETURNING id`, ws)
}

// apiKey mints a legacy-format API key of org with permissions on workspaces, given as workspace, permission pairs, and returns it with its id.
func (e *env) apiKey(org uuid.UUID, grants ...any) (string, uuid.UUID) {
	raw := "test-key-" + uuid.NewString()
	sum := sha256.Sum256([]byte(raw))
	digest := hex.EncodeToString(sum[:])
	id := e.insert(`INSERT INTO api_keys (organization_id, label, hashed_key, lookup_hash) VALUES (?, 'test', ?, ?) RETURNING id`, org, digest, digest)
	for i := 0; i+1 < len(grants); i += 2 {
		e.exec(`INSERT INTO api_key_workspaces (api_key_id, workspace_id, permission) VALUES (?, ?, ?)`, id, grants[i], grants[i+1])
	}
	return raw, id
}

type reqHeaders map[string]string

func asUser(id uuid.UUID) reqHeaders { return reqHeaders{testUserHeader: id.String()} }

func asKey(key string) reqHeaders { return reqHeaders{"x-api-key": key} }

func (e *env) do(method, path string, h reqHeaders, body any) *httptest.ResponseRecorder {
	e.t.Helper()
	e.authz.Deliver(e.t)
	var reader io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		reader = strings.NewReader(b)
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			e.t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", webApp)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	for k, v := range h {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	e.engine.ServeHTTP(rec, req)
	return rec
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %s: %v", rec.Body.String(), err)
	}
	return v
}

// auditEvent is the part of an audit record the tests check.
type auditEvent struct {
	Actor          struct{ ID, Type string }
	Target         struct{ ID, Type string }
	Action         string
	Status         string
	OrganizationID string `json:"organizationId"`
	EventID        string `json:"eventId"`
	Changes        map[string]any
}

func (e *env) audits() []auditEvent {
	e.t.Helper()
	var out []auditEvent
	for line := range strings.SplitSeq(e.logs.String(), "\n") {
		if !strings.Contains(line, `"msg":"audit"`) {
			continue
		}
		var ev auditEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			e.t.Fatal(err)
		}
		out = append(out, ev)
	}
	return out
}

func (e *env) lastAudit() auditEvent {
	e.t.Helper()
	all := e.audits()
	if len(all) == 0 {
		e.t.Fatal("no audit event")
	}
	return all[len(all)-1]
}

// receiver is an HTTP endpoint that records the requests it gets and answers with status.
type receiver struct {
	*httptest.Server
	mu       sync.Mutex
	requests []received
	status   int
}

type received struct {
	Header http.Header
	Body   []byte
}

func newReceiver(t *testing.T, status int) *receiver {
	t.Helper()
	r := &receiver{status: status}
	r.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		r.mu.Lock()
		r.requests = append(r.requests, received{Header: req.Header.Clone(), Body: body})
		status := r.status
		r.mu.Unlock()
		w.WriteHeader(status)
	}))
	t.Cleanup(r.Close)
	return r
}

func (r *receiver) received() []received {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]received(nil), r.requests...)
}
