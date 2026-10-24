// Package v3apitest gives a feature module's tests the server around its handlers: a fresh migrated database, real authorization, rate limits and audit events, and the /api/app and /api/v3 groups mounted with their production middleware. A signed-in user is named by a request header instead of a session cookie (the credential module tests sessions); API keys are real.
package v3apitest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/apikey"
	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/authz/authztest"
	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/audit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/apps/server/internal/platform/pgtest"
	"github.com/yldm-tech/forma/apps/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/apps/server/internal/v3api"
)

// WebApp is the origin requests are sent from.
const WebApp = "http://app.example.com"

// userHeader names the signed-in user of a request.
const userHeader = "X-Test-User"

// Sessions stands in for the credential module's sessions: the user named by the X-Test-User header is signed in.
type Sessions struct{}

func (Sessions) authenticate(c *gin.Context) bool {
	id, err := uuid.Parse(c.GetHeader(userHeader))
	if err != nil {
		return false
	}
	httpapi.SetActor(c, httpapi.Actor{UserID: id})
	return true
}

func (s Sessions) Require() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !s.authenticate(c) {
			httpapi.Fail(c, apperr.NotAuthenticated("Not authenticated"))
			return
		}
		c.Next()
	}
}

func (s Sessions) Optional() gin.HandlerFunc {
	return func(c *gin.Context) {
		s.authenticate(c)
		c.Next()
	}
}

// Logs is a log sink tests can read while handlers write to it.
type Logs struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *Logs) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// Records returns the JSON log records whose msg is msg.
func (b *Logs) Records(t testing.TB, msg string) []map[string]any {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(b.buf.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("log line %q: %v", line, err)
		}
		if rec["msg"] == msg {
			out = append(out, rec)
		}
	}
	return out
}

// Env is one isolated server for a test.
type Env struct {
	t       *testing.T
	DB      *gorm.DB
	Config  config.Config
	Log     *slog.Logger
	Logs    *Logs
	Authz   *authztest.Env
	Limiter *ratelimit.Limiter
	Audit   *audit.Emitter
	V3      *v3api.Middleware
	Auditor *v3api.Auditor
	engine  *gin.Engine
	// App and API are the /api/app and /api/v3 groups, for the module's Register and RegisterV3.
	App gin.IRouter
	API gin.IRouter
}

// New builds the environment; the test runs in parallel with others. Tests that need Docker are skipped under `go test -short`.
func New(t *testing.T) *Env {
	t.Helper()
	t.Parallel()
	gin.SetMode(gin.TestMode)
	u, _ := url.Parse(WebApp)
	cfg := config.Config{WebAppURL: u, RateLimitingDisabled: true, AuditLogEnabled: true, UserManagementMinimumRole: "manager", TrustedProxyHopCount: 1}
	db := pgtest.GORM(t)
	e := &Env{t: t, DB: db, Config: cfg, Logs: &Logs{}, Authz: authztest.New(t, cfg, db)}
	e.Log = slog.New(slog.NewJSONHandler(e.Logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	e.Limiter = ratelimit.New(cfg, nil, e.Log)
	e.Audit = audit.New(cfg, e.Log)
	e.V3 = v3api.NewMiddleware(cfg, apikey.NewAuthenticator(db, e.Log), e.Limiter)
	e.Auditor = v3api.NewAuditor(cfg, e.Audit)
	e.engine = gin.New()
	e.App = e.engine.Group("/api/app", httpapi.ErrorHandler(e.Log), httpapi.CSRF(cfg), authz.DecisionMemo())
	e.API = e.engine.Group("/api/v3", v3api.ErrorHandler(e.Log), authz.DecisionMemo(), Sessions{}.Optional())
	return e
}

func (e *Env) Exec(query string, args ...any) {
	e.t.Helper()
	if err := e.DB.Exec(query, args...).Error; err != nil {
		e.t.Fatalf("exec %q: %v", query, err)
	}
}

func (e *Env) Insert(query string, args ...any) uuid.UUID {
	e.t.Helper()
	var id uuid.UUID
	if err := e.DB.Raw(query, args...).Row().Scan(&id); err != nil {
		e.t.Fatalf("insert %q: %v", query, err)
	}
	return id
}

func (e *Env) Count(query string, args ...any) int {
	e.t.Helper()
	var n int
	if err := e.DB.Raw(query, args...).Row().Scan(&n); err != nil {
		e.t.Fatalf("query %q: %v", query, err)
	}
	return n
}

func (e *Env) User() uuid.UUID {
	return e.Insert(`INSERT INTO users (name, email) VALUES ('Ada', ?) RETURNING id`, uuid.NewString()+"@example.com")
}

// Organization creates an organization owned by owner, with one workspace.
func (e *Env) Organization(owner uuid.UUID) (org, ws uuid.UUID) {
	org = e.Insert(`INSERT INTO organizations (name) VALUES ('Acme') RETURNING id`)
	e.Member(org, owner, "owner")
	return org, e.Workspace(org, "My workspace")
}

// Workspace creates a workspace with English as its survey language, as the workspace module seeds one.
func (e *Env) Workspace(org uuid.UUID, name string) uuid.UUID {
	ws := e.Insert(`INSERT INTO workspaces (organization_id, name) VALUES (?, ?) RETURNING id`, org, name)
	e.Exec(`INSERT INTO languages (workspace_id, code) VALUES (?, 'en-US')`, ws)
	return ws
}

func (e *Env) Member(org, user uuid.UUID, role string) {
	e.Exec(`INSERT INTO memberships (user_id, organization_id, role, accepted) VALUES (?, ?, ?, true)`, user, org, role)
}

// Grant puts user in a new team of org with permission (read, readWrite, manage) on ws.
func (e *Env) Grant(org, ws, user uuid.UUID, permission string) {
	team := e.Insert(`INSERT INTO teams (organization_id, name) VALUES (?, ?) RETURNING id`, org, "Team "+uuid.NewString())
	e.Exec(`INSERT INTO team_members (team_id, user_id, role) VALUES (?, ?, 'contributor')`, team, user)
	e.Exec(`INSERT INTO workspace_teams (workspace_id, team_id, permission) VALUES (?, ?, ?)`, ws, team, permission)
}

// APIKey stores a key of org with the given workspace permissions (read, write, manage) and returns the raw key to send in x-api-key. It is a pre-bcrypt key, which authenticates the same way and is cheap to verify.
func (e *Env) APIKey(org uuid.UUID, grants map[uuid.UUID]string) string {
	raw := "test-key-" + uuid.NewString()
	sum := sha256.Sum256([]byte(raw))
	digest := hex.EncodeToString(sum[:])
	id := e.Insert(`INSERT INTO api_keys (organization_id, label, hashed_key, lookup_hash) VALUES (?, 'test', ?, ?) RETURNING id`, org, digest, digest)
	for ws, permission := range grants {
		e.Exec(`INSERT INTO api_key_workspaces (api_key_id, workspace_id, permission) VALUES (?, ?, ?)`, id, ws, permission)
	}
	return raw
}

// Request is one call: a signed-in user (uuid.Nil for none), an API key, or both.
type Request struct {
	Method string
	Path   string
	User   uuid.UUID
	APIKey string
	// Body is marshalled as JSON unless it is a string, which is sent as is.
	Body any
	// CrossSite drops the Origin and X-Requested-With headers the SPA sends.
	CrossSite bool
}

// Do delivers the authorization projection, then serves the request.
func (e *Env) Do(r Request) *httptest.ResponseRecorder {
	e.t.Helper()
	e.Authz.Deliver(e.t)
	var body io.Reader
	switch b := r.Body.(type) {
	case nil:
	case string:
		body = strings.NewReader(b)
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			e.t.Fatal(err)
		}
		body = bytes.NewReader(raw)
	}
	req := httptest.NewRequestWithContext(context.Background(), r.Method, r.Path, body)
	if r.Body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if !r.CrossSite {
		req.Header.Set("Origin", WebApp)
		req.Header.Set(httpapi.CSRFHeader, "XMLHttpRequest")
	}
	if r.User != uuid.Nil {
		req.Header.Set(userHeader, r.User.String())
	}
	if r.APIKey != "" {
		req.Header.Set(apikey.HeaderAPIKey, r.APIKey)
	}
	rec := httptest.NewRecorder()
	e.engine.ServeHTTP(rec, req)
	return rec
}

// OK asserts the status and decodes the body into out, when given.
func (e *Env) OK(rec *httptest.ResponseRecorder, status int, out any) {
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

// Fails asserts an /api/app error envelope; an empty message is not compared.
func (e *Env) Fails(rec *httptest.ResponseRecorder, status int, code, message string) httpapi.Envelope {
	e.t.Helper()
	var body httpapi.Envelope
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Code != status || body.Code != code || (message != "" && body.Message != message) {
		e.t.Fatalf("got %d %s, want %d %s %q", rec.Code, rec.Body.String(), status, code, message)
	}
	return body
}

// Problem is a decoded v3 problem response.
type Problem struct {
	Title         string               `json:"title"`
	Status        int                  `json:"status"`
	Detail        string               `json:"detail"`
	Code          string               `json:"code"`
	Instance      string               `json:"instance"`
	RequestID     string               `json:"requestId"`
	InvalidParams []v3api.InvalidParam `json:"invalid_params"`
}

// Problem asserts a v3 problem with status, code and detail (an empty detail is not compared), served as application/problem+json with the no-store cache header.
func (e *Env) Problem(rec *httptest.ResponseRecorder, status int, code, detail string) Problem {
	e.t.Helper()
	var p Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		e.t.Fatalf("decode problem %s: %v", rec.Body.String(), err)
	}
	if rec.Code != status || p.Status != status || p.Code != code || (detail != "" && p.Detail != detail) {
		e.t.Fatalf("got %d %s, want %d %s %q", rec.Code, rec.Body.String(), status, code, detail)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		e.t.Fatalf("content type %q", ct)
	}
	if rec.Header().Get("Cache-Control") != v3api.CacheNoStore || p.RequestID == "" || rec.Header().Get("X-Request-Id") != p.RequestID {
		e.t.Fatalf("headers %v body %s", rec.Header(), rec.Body.String())
	}
	return p
}
