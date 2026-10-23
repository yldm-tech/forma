package survey

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
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

	"github.com/yldm-tech/forma/apps/server/internal/ai"
	"github.com/yldm-tech/forma/apps/server/internal/apikey"
	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/authz/authztest"
	"github.com/yldm-tech/forma/apps/server/internal/jsonschema"
	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/audit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpserver"
	"github.com/yldm-tech/forma/apps/server/internal/platform/pgtest"
	"github.com/yldm-tech/forma/apps/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/secret"
	"github.com/yldm-tech/forma/apps/server/internal/platform/v3api"
	"github.com/yldm-tech/forma/apps/server/internal/survey/surveydoc"
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

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// env is one isolated instance of the survey module: a fresh database, real authorization, and its routes as the server mounts them.
type env struct {
	t      *testing.T
	cfg    config.Config
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
	cfg := config.Config{
		WebAppURL: u, RateLimitingDisabled: true, AuditLogEnabled: true, UserManagementMinimumRole: "manager", TrustedProxyHopCount: 1,
		EncryptionKey: bytes.Repeat([]byte{7}, 32), EncryptionKeyText: config.Secret(strings.Repeat("07", 32)), EncryptionKeyVersion: 1, SurveySchedulingTimeZone: "Europe/Berlin",
	}
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
	cipher, err := secret.NewCipher(cfg)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := LoadCatalog()
	if err != nil {
		t.Fatal(err)
	}
	limiter := ratelimit.New(cfg, nil, log)
	e.repo = NewRepository(db)
	aiService := ai.NewService(ai.NewClient(cfg, log), ai.NewRepository(db), e.authz.Authorizer, log)
	e.svc = NewService(cfg, e.repo, e.authz.Authorizer, limiter, audit.New(cfg, log), surveydoc.NewValidator(schemas), cipher, catalog, aiService, log)
	h := NewHandler(e.svc, headerSessions{}, v3api.NewGuard(cfg, headerSessions{}, apikey.NewAuthenticator(db, log), limiter, log), limiter, log)
	e.engine = httpserver.NewEngine(cfg, log)
	group := e.engine.Group("/api/app", httpapi.ErrorHandler(log), httpapi.CSRF(cfg), authz.DecisionMemo())
	h.Register(group)
	h.RegisterV3(e.engine)
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

func (e *env) text(query string, args ...any) string {
	e.t.Helper()
	var s *string
	if err := e.db.Raw(query, args...).Row().Scan(&s); err != nil {
		e.t.Fatalf("query %q: %v", query, err)
	}
	if s == nil {
		return ""
	}
	return *s
}

func (e *env) user() uuid.UUID {
	return e.insert(`INSERT INTO users (name, email) VALUES ('Ada', ?) RETURNING id`, uuid.NewString()+"@example.com")
}

// organization creates an organization owned by owner, with one workspace that has the en-US language every new workspace gets.
func (e *env) organization(owner uuid.UUID) (org, ws uuid.UUID) {
	e.t.Helper()
	org = e.insert(`INSERT INTO organizations (name) VALUES ('Acme') RETURNING id`)
	e.member(org, owner, "owner")
	return org, e.workspace(org)
}

func (e *env) workspace(org uuid.UUID) uuid.UUID {
	ws := e.insert(`INSERT INTO workspaces (organization_id, name) VALUES (?, ?) RETURNING id`, org, "Workspace "+uuid.NewString()[:8])
	e.exec(`INSERT INTO languages (workspace_id, code) VALUES (?, 'en-US')`, ws)
	return ws
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

// apiKey mints a legacy-format API key of org with a permission on ws and returns its secret.
func (e *env) apiKey(org, ws uuid.UUID, permission string) string {
	raw := "test-key-" + uuid.NewString()
	sum := sha256.Sum256([]byte(raw))
	digest := hex.EncodeToString(sum[:])
	id := e.insert(`INSERT INTO api_keys (organization_id, label, hashed_key, lookup_hash) VALUES (?, 'test', ?, ?) RETURNING id`, org, digest, digest)
	e.exec(`INSERT INTO api_key_workspaces (api_key_id, workspace_id, permission) VALUES (?, ?, ?)`, id, ws, permission)
	return raw
}

func (e *env) actionClass(ws uuid.UUID, name string) uuid.UUID {
	return e.insert(`INSERT INTO action_classes (workspace_id, name, type, key) VALUES (?, ?, 'code', ?) RETURNING id`, ws, name, name)
}

func (e *env) request(method, path string, body any, headers map[string]string) *httptest.ResponseRecorder {
	e.t.Helper()
	e.authz.Deliver(e.t)
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
	req.Header.Set("Origin", webApp)
	req.Header.Set(httpapi.CSRFHeader, "XMLHttpRequest")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	e.engine.ServeHTTP(rec, req)
	return rec
}

// do sends an /api/app request as user.
func (e *env) do(user uuid.UUID, method, path string, body any) *httptest.ResponseRecorder {
	return e.request(method, path, body, map[string]string{testUserHeader: user.String()})
}

// v3 sends an /api/v3 request as user.
func (e *env) v3(user uuid.UUID, method, path string, body any) *httptest.ResponseRecorder {
	return e.request(method, path, body, map[string]string{testUserHeader: user.String()})
}

// v3Key sends an /api/v3 request with an API key.
func (e *env) v3Key(key, method, path string, body any) *httptest.ResponseRecorder {
	return e.request(method, path, body, map[string]string{"x-api-key": key})
}

func (e *env) ok(rec *httptest.ResponseRecorder, status int, out any) {
	e.t.Helper()
	if rec.Code != status {
		e.t.Fatalf("status %d, want %d: %s\n%s", rec.Code, status, rec.Body.String(), e.errorLogs())
	}
	if out != nil {
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			e.t.Fatalf("decode %s: %v", rec.Body.String(), err)
		}
	}
}

// errorLogs are the error records logged so far, for a failing assertion's message.
func (e *env) errorLogs() string {
	var out []string
	for _, line := range strings.Split(e.logs.String(), "\n") {
		if strings.Contains(line, `"level":"ERROR"`) {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
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

type problem struct {
	Title         string               `json:"title"`
	Status        int                  `json:"status"`
	Detail        string               `json:"detail"`
	Code          string               `json:"code"`
	RequestID     string               `json:"requestId"`
	InvalidParams []v3api.InvalidParam `json:"invalid_params"`
}

// problem asserts an RFC 9457 problem with the status and detail, and returns it.
func (e *env) problem(rec *httptest.ResponseRecorder, status int, detail string) problem {
	e.t.Helper()
	var p problem
	_ = json.Unmarshal(rec.Body.Bytes(), &p)
	if rec.Code != status || p.Status != status || (detail != "" && p.Detail != detail) || rec.Header().Get("Content-Type") != "application/problem+json" || rec.Header().Get("Cache-Control") != v3api.CacheNoStore {
		e.t.Fatalf("got %d %s (%s), want %d %q\n%s", rec.Code, rec.Body.String(), rec.Header().Get("Content-Type"), status, detail, e.errorLogs())
	}
	return p
}

// param finds the invalid param with the name.
func (p problem) param(t *testing.T, name string) v3api.InvalidParam {
	t.Helper()
	for _, ip := range p.InvalidParams {
		if ip.Name == name {
			return ip
		}
	}
	t.Fatalf("no invalid param %q among %+v", name, p.InvalidParams)
	return v3api.InvalidParam{}
}

type resource struct {
	Data map[string]any `json:"data"`
}

func surveysPath(rest string) string { return "/api/v3/surveys" + rest }

// holdSurveyLock locks the survey's row in a transaction of its own until release is called with the statements to run before the commit; a request that writes the survey meanwhile waits for the lock.
func (e *env) holdSurveyLock(id string) (release func(statements ...string)) {
	e.t.Helper()
	tx := e.db.Begin()
	if err := tx.Exec(`SELECT id FROM surveys WHERE id = ? FOR UPDATE`, id).Error; err != nil {
		e.t.Fatal(err)
	}
	return func(statements ...string) {
		e.t.Helper()
		for _, stmt := range statements {
			if err := tx.Exec(stmt, id).Error; err != nil {
				tx.Rollback()
				e.t.Fatalf("exec %q: %v", stmt, err)
			}
		}
		if err := tx.Commit().Error; err != nil {
			e.t.Fatal(err)
		}
	}
}

// async sends a request from another goroutine and returns where its response arrives.
func (e *env) async(method, path string, body any, headers map[string]string) <-chan *httptest.ResponseRecorder {
	e.t.Helper()
	e.authz.Deliver(e.t)
	raw, err := json.Marshal(body)
	if err != nil {
		e.t.Fatal(err)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", webApp)
	req.Header.Set(httpapi.CSRFHeader, "XMLHttpRequest")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	out := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		rec := httptest.NewRecorder()
		e.engine.ServeHTTP(rec, req)
		out <- rec
	}()
	return out
}

// waitForLockWaiter returns once a session of the test database waits for a lock.
func (e *env) waitForLockWaiter() {
	e.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if e.count(`SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND wait_event_type = 'Lock'`) > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	e.t.Fatal("no request waited for the survey's row lock")
}

// auditEvent is an audit event as the emitter logs it.
type auditEvent struct {
	Action string `json:"action"`
	Status string `json:"status"`
	Actor  struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	} `json:"actor"`
	Target struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	} `json:"target"`
	OrganizationID string `json:"organizationId"`
}

// lastAudit is the latest audit event with the action, failing the test when there is none.
func (e *env) lastAudit(action audit.Action) auditEvent {
	e.t.Helper()
	var found *auditEvent
	for _, line := range strings.Split(e.logs.String(), "\n") {
		var rec struct {
			Msg string `json:"msg"`
			auditEvent
		}
		if json.Unmarshal([]byte(line), &rec) == nil && rec.Msg == "audit" && rec.Action == string(action) {
			found = &rec.auditEvent
		}
	}
	if found == nil {
		e.t.Fatalf("no %s audit event", action)
		return auditEvent{}
	}
	return *found
}

// expectAudit asserts the latest audit event with the action.
func (e *env) expectAudit(action audit.Action, status audit.Status, actorType audit.ActorType, actorID string, target audit.Target, targetID string) {
	e.t.Helper()
	got := e.lastAudit(action)
	if got.Status != string(status) || got.Actor.Type != string(actorType) || (actorID != "" && got.Actor.ID != actorID) || got.Target.Type != string(target) || got.Target.ID != targetID {
		e.t.Fatalf("%s audit %+v, want %s by %s %s on %s %s", action, got, status, actorType, actorID, target, targetID)
	}
}
