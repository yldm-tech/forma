package response

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

	"github.com/yldm-tech/forma/server/internal/authz"
	"github.com/yldm-tech/forma/server/internal/authz/authztest"
	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/platform/audit"
	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/server/internal/platform/httpserver"
	"github.com/yldm-tech/forma/server/internal/platform/jobs"
	"github.com/yldm-tech/forma/server/internal/platform/jsvalue"
	"github.com/yldm-tech/forma/server/internal/platform/pgtest"
	"github.com/yldm-tech/forma/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/server/internal/platform/secret"
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

// env is one isolated instance of the response module: a fresh database, real authorization and job enqueueing, and its /api/app routes as the server mounts them.
type env struct {
	t        *testing.T
	cfg      config.Config
	db       *gorm.DB
	authz    *authztest.Env
	svc      *Service
	engine   *gin.Engine
	logs     *syncBuffer
	resolver *httpapi.ClientIPResolver
}

func newEnv(t *testing.T) *env {
	t.Helper()
	t.Parallel()
	u, _ := url.Parse(webApp)
	cfg := config.Config{
		WebAppURL: u, RateLimitingDisabled: true, AuditLogEnabled: true, UserManagementMinimumRole: "manager", TrustedProxyHopCount: 1,
		EncryptionKey: bytes.Repeat([]byte{7}, 32), EncryptionKeyText: config.Secret(strings.Repeat("07", 32)), EncryptionKeyVersion: 1,
	}
	db := pgtest.GORM(t)
	e := &env{t: t, cfg: cfg, db: db, authz: authztest.New(t, cfg, db), logs: &syncBuffer{}}
	log := slog.New(slog.NewJSONHandler(e.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	cipher, err := secret.NewCipher(cfg)
	if err != nil {
		t.Fatal(err)
	}
	enqueuer, err := jobs.NewEnqueuer(log)
	if err != nil {
		t.Fatal(err)
	}
	e.svc = NewService(cfg, db, enqueuer, cipher, e.authz.Authorizer, ratelimit.New(cfg, nil, log), audit.New(cfg, log), log)
	e.resolver = httpapi.NewClientIPResolver(cfg, log)
	e.engine = httpserver.NewEngine(log)
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

// jsonOf reads one JSON value and decodes it.
func (e *env) jsonOf(query string, args ...any) any {
	e.t.Helper()
	var raw []byte
	if err := e.db.Raw(query, args...).Row().Scan(&raw); err != nil {
		e.t.Fatalf("query %q: %v", query, err)
	}
	if raw == nil {
		return nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		e.t.Fatal(err)
	}
	return v
}

func (e *env) user() uuid.UUID {
	return e.insert(`INSERT INTO users (name, email) VALUES ('Ada', ?) RETURNING id`, uuid.NewString()+"@example.com")
}

// organization creates an organization owned by owner with one workspace, which has the userId attribute key every workspace gets.
func (e *env) organization(owner uuid.UUID) (org, ws uuid.UUID) {
	e.t.Helper()
	org = e.insert(`INSERT INTO organizations (name) VALUES ('Acme') RETURNING id`)
	e.exec(`INSERT INTO memberships (user_id, organization_id, role, accepted) VALUES (?, ?, 'owner', true)`, owner, org)
	return org, e.workspace(org)
}

func (e *env) workspace(org uuid.UUID) uuid.UUID {
	ws := e.insert(`INSERT INTO workspaces (organization_id, name) VALUES (?, ?) RETURNING id`, org, "Workspace "+uuid.NewString()[:8])
	e.exec(`INSERT INTO contact_attribute_keys (workspace_id, key, type) VALUES (?, 'userId', 'default'), (?, 'email', 'default')`, ws, ws)
	return ws
}

// grant puts user in a new team of org with access to ws.
func (e *env) grant(org, ws, user uuid.UUID, permission string) {
	e.t.Helper()
	team := e.insert(`INSERT INTO teams (organization_id, name) VALUES (?, ?) RETURNING id`, org, "Team "+uuid.NewString())
	e.exec(`INSERT INTO team_members (team_id, user_id, role) VALUES (?, ?, 'contributor')`, team, user)
	e.exec(`INSERT INTO workspace_teams (workspace_id, team_id, permission) VALUES (?, ?, ?)`, ws, team, permission)
}

// defaultBlocks is one page with an open text question, a single choice with an Other box, a file upload and a number question.
const defaultBlocks = `[{"id":"b1","name":"Block 1","elements":[
	{"id":"q1","type":"openText","headline":{"default":"Q1"},"required":false,"inputType":"text"},
	{"id":"q2","type":"multipleChoiceSingle","headline":{"default":"Q2"},"required":false,"choices":[{"id":"c1","label":{"default":"One","de":"Eins"}},{"id":"c2","label":{"default":"Two"}},{"id":"other","label":{"default":"Other"}}]},
	{"id":"f1","type":"fileUpload","headline":{"default":"F1"},"required":false},
	{"id":"n1","type":"openText","headline":{"default":"N1"},"required":false,"inputType":"number"}
]}]`

type surveyOpts struct {
	typ, status     string
	singleUse       string
	captureIP       bool
	anonymize       bool
	verifyEmail     bool
	hiddenFieldIDs  []string
	defaultLanguage string
}

func (e *env) survey(ws uuid.UUID, o surveyOpts) uuid.UUID {
	e.t.Helper()
	if o.typ == "" {
		o.typ = "link"
	}
	if o.status == "" {
		o.status = "inProgress"
	}
	if o.singleUse == "" {
		o.singleUse = `{"enabled": false, "isEncrypted": true}`
	}
	hidden, _ := json.Marshal(map[string]any{"enabled": true, "fieldIds": append([]string{}, o.hiddenFieldIDs...)})
	id := e.insert(`INSERT INTO surveys (workspace_id, name, type, status, blocks, endings, hidden_fields, single_use, is_capture_ip_enabled, is_anonymize_responses_enabled, is_verify_email_enabled)
		VALUES (?, 'Survey', ?, ?, ?::jsonb, '[{"id":"end1","type":"endScreen"}]'::jsonb, ?::jsonb, ?::jsonb, ?, ?, ?) RETURNING id`,
		ws, o.typ, o.status, defaultBlocks, string(hidden), o.singleUse, o.captureIP, o.anonymize, o.verifyEmail)
	if o.defaultLanguage != "" {
		lang := e.insert(`INSERT INTO languages (workspace_id, code) VALUES (?, ?) RETURNING id`, ws, o.defaultLanguage)
		e.exec(`INSERT INTO survey_languages (survey_id, language_id, is_default) VALUES (?, ?, true)`, id, lang)
	}
	return id
}

// field links a survey-local Embedded Data field.
func (e *env) field(ws, survey uuid.UUID, key, source, dataType string, locked bool) {
	e.t.Helper()
	def := e.insert(`INSERT INTO embedded_data (workspace_id, survey_id, name, source, data_type, locked) VALUES (?, ?, ?, ?, ?, ?) RETURNING id`, ws, survey, key, source, dataType, locked)
	position := e.count(`SELECT count(*) FROM survey_embedded_data WHERE survey_id = ?`, survey)
	e.exec(`INSERT INTO survey_embedded_data (workspace_id, survey_id, embedded_data_id, storage_key, position) VALUES (?, ?, ?, ?, ?)`, ws, survey, def, key, position)
}

// quotaOn creates a quota whose single condition is `q1 equals value`.
func (e *env) quotaOn(survey uuid.UUID, name, value string, limit int, action string, ending *string, countPartial bool) uuid.UUID {
	logic := `{"connector":"and","conditions":[{"id":"c1","leftOperand":{"type":"element","value":"q1"},"operator":"equals","rightOperand":{"type":"static","value":"` + value + `"}}]}`
	return e.insert(`INSERT INTO survey_quotas (survey_id, name, "limit", logic, action, ending_card_id, count_partial_submissions) VALUES (?, ?, ?, ?::jsonb, ?, ?, ?) RETURNING id`,
		survey, name, limit, logic, action, ending, countPartial)
}

// contact creates a contact of ws with a userId and the further text attributes given as key, value pairs.
func (e *env) contact(ws uuid.UUID, userID string, attrs ...string) uuid.UUID {
	e.t.Helper()
	c := e.insert(`INSERT INTO contacts (workspace_id) VALUES (?) RETURNING id`, ws)
	set := func(key, value string) {
		e.exec(`INSERT INTO contact_attribute_keys (workspace_id, key) VALUES (?, ?) ON CONFLICT DO NOTHING`, ws, key)
		e.exec(`INSERT INTO contact_attributes (contact_id, attribute_key_id, value_text) SELECT ?, id, ? FROM contact_attribute_keys WHERE workspace_id = ? AND key = ?`, c, value, ws, key)
	}
	set("userId", userID)
	for i := 0; i+1 < len(attrs); i += 2 {
		set(attrs[i], attrs[i+1])
	}
	return c
}

func (e *env) display(survey uuid.UUID, contact *uuid.UUID) uuid.UUID {
	return e.insert(`INSERT INTO displays (survey_id, contact_id) VALUES (?, ?) RETURNING id`, survey, contact)
}

// clientContext is a request context as the client IP middleware leaves it, for a request forwarded by one proxy for ip.
func (e *env) clientContext(ip string) context.Context {
	e.t.Helper()
	var ctx context.Context
	engine := gin.New()
	engine.Use(e.resolver.Middleware())
	engine.GET("/", func(c *gin.Context) { ctx = c.Request.Context() })
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if ip != "" {
		req.Header.Set("X-Forwarded-For", ip)
	}
	engine.ServeHTTP(httptest.NewRecorder(), req)
	return ctx
}

func members(t *testing.T, raw string) []jsvalue.Member {
	t.Helper()
	m, err := jsvalue.DecodeObject([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// submission is the client API's create: the survey, its checks and the ingest, then the write, as the v1 route runs them.
func (e *env) submit(ctx context.Context, ws, survey uuid.UUID, data string, in CreateInput) (*Result, error) {
	e.t.Helper()
	sv, err := e.svc.Survey(ctx, survey.String())
	if err != nil {
		return nil, err
	}
	if err := sv.CheckWorkspace(ws); err != nil {
		return nil, err
	}
	if err := sv.CheckAccepting(); err != nil {
		return nil, err
	}
	ingest := e.svc.Ingest(sv, members(e.t, data))
	in.WorkspaceID, in.Data, in.IngestFlags = ws, ingest.Data, ingest.Flags
	if in.SingleUseID, err = e.svc.SingleUseID(sv, ws, false, in.SingleUseID, in.Meta.URL); err != nil {
		return nil, err
	}
	return e.svc.Create(ctx, sv, in)
}

// update is the client API's update, as the PUT route runs it.
func (e *env) update(ctx context.Context, ws uuid.UUID, responseID, data string, in UpdateInput) (*Result, error) {
	e.t.Helper()
	existing, sv, err := e.svc.ResponseForUpdate(ctx, ws, responseID)
	if err != nil {
		return nil, err
	}
	var ingest = e.svc.IngestUpdate(sv, nil)
	if data != "" {
		ingest = e.svc.IngestUpdate(sv, members(e.t, data))
		in.Data = ingest.Data
	}
	in.IngestFlags = ingest.Flags
	if err := CheckUpdate(sv, existing); err != nil {
		return nil, err
	}
	return e.svc.Update(ctx, sv, responseID, in)
}

func (e *env) request(user uuid.UUID, method, path string, body any) *httptest.ResponseRecorder {
	e.t.Helper()
	e.authz.Deliver(e.t)
	var r io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
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
	req.Header.Set(testUserHeader, user.String())
	rec := httptest.NewRecorder()
	e.engine.ServeHTTP(rec, req)
	return rec
}

// appErr asserts err is the client API error with status, message and details.
func appErr(t *testing.T, err error, status int, message string, wantDetails map[string]any) {
	t.Helper()
	ae, ok := apperr.As(err)
	if !ok {
		t.Fatalf("error %v is not an expected failure", err)
	}
	if ae.Status != status || ae.Message != message {
		t.Fatalf("error %d %q, want %d %q", ae.Status, ae.Message, status, message)
	}
	if wantDetails != nil {
		got, _ := json.Marshal(ae.Details)
		want, _ := json.Marshal(wantDetails)
		if !bytes.Equal(got, want) {
			t.Fatalf("details %s, want %s", got, want)
		}
	}
}

func ptr[T any](v T) *T { return &v }

func contains(haystack, needle string) bool { return strings.Contains(haystack, needle) }
