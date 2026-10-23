package contact

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/server/internal/apikey"
	"github.com/yldm-tech/forma/server/internal/apiv3"
	"github.com/yldm-tech/forma/server/internal/authz"
	"github.com/yldm-tech/forma/server/internal/authz/authztest"
	"github.com/yldm-tech/forma/server/internal/jsonschema"
	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/platform/audit"
	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/server/internal/platform/httpserver"
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
		if !setTestActor(c) {
			httpapi.Fail(c, apperr.NotAuthenticated("Not authenticated"))
			return
		}
		c.Next()
	}
}

func (headerSessions) Optional() gin.HandlerFunc {
	return func(c *gin.Context) {
		setTestActor(c)
		c.Next()
	}
}

func setTestActor(c *gin.Context) bool {
	id, err := uuid.Parse(c.GetHeader(testUserHeader))
	if err != nil {
		return false
	}
	httpapi.SetActor(c, httpapi.Actor{UserID: id})
	return true
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

// env is one isolated instance of the contact module: a fresh database, real authorization, and its /api/app and /api/v3 routes as the server mounts them.
type env struct {
	t         *testing.T
	db        *gorm.DB
	authz     *authztest.Env
	repo      *Repository
	svc       *Service
	evaluator *Evaluator
	apiKeys   *apikey.Service
	engine    *gin.Engine
	logs      *syncBuffer
}

func newEnv(t *testing.T) *env {
	t.Helper()
	t.Parallel()
	u, _ := url.Parse(webApp)
	cfg := config.Config{WebAppURL: u, RateLimitingDisabled: true, AuditLogEnabled: true, UserManagementMinimumRole: "manager", TrustedProxyHopCount: 1}
	db := pgtest.GORM(t)
	e := &env{t: t, db: db, authz: authztest.New(t, cfg, db), logs: &syncBuffer{}}
	log := slog.New(slog.NewJSONHandler(e.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	schemas, err := jsonschema.New()
	if err != nil {
		t.Fatal(err)
	}
	limiter := ratelimit.New(cfg, nil, log)
	emitter := audit.New(cfg, log)
	e.repo = NewRepository(db)
	e.evaluator = NewEvaluator(db, log)
	e.svc = NewService(e.repo, e.authz.Authorizer, limiter, emitter, schemas, log)
	e.apiKeys = apikey.NewService(apikey.NewRepository(db), e.authz.Authorizer, emitter)

	e.engine = httpserver.NewEngine(cfg, log)
	group := e.engine.Group("/api/app", httpapi.ErrorHandler(log), httpapi.CSRF(cfg), authz.DecisionMemo())
	NewHandler(e.svc, headerSessions{}).Register(group)
	v3 := e.engine.Group("/api/v3", append([]gin.HandlerFunc{apiv3.ErrorHandler(log), authz.DecisionMemo()}, apiv3.Authenticate(headerSessions{}, apikey.NewAuthenticator(db, log), limiter)...)...)
	NewV3Handler(e.svc).Register(v3)
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

// organization creates an organization owned by owner, with a workspace seeded with the default attribute keys.
func (e *env) organization(owner uuid.UUID) (org, ws uuid.UUID) {
	e.t.Helper()
	org = e.insert(`INSERT INTO organizations (name) VALUES ('Acme') RETURNING id`)
	e.member(org, owner, "owner")
	return org, e.workspace(org)
}

// workspace creates a workspace with forma2's default keys, as the workspace module seeds them.
func (e *env) workspace(org uuid.UUID) uuid.UUID {
	e.t.Helper()
	ws := e.insert(`INSERT INTO workspaces (organization_id, name) VALUES (?, ?) RETURNING id`, org, "Workspace "+uuid.NewString())
	for _, k := range []struct {
		key, name string
		unique    bool
	}{{"userId", "User Id", true}, {"email", "Email", true}, {"firstName", "First Name", false}, {"lastName", "Last Name", false}, {"language", "Language", false}} {
		e.exec(`INSERT INTO contact_attribute_keys (workspace_id, key, name, type, is_unique) VALUES (?, ?, ?, 'default', ?)`, ws, k.key, k.name, k.unique)
	}
	return ws
}

func (e *env) member(org, user uuid.UUID, role string) {
	e.exec(`INSERT INTO memberships (user_id, organization_id, role, accepted) VALUES (?, ?, ?, true)`, user, org, role)
}

// grant makes user a member of org who may read ws through a team.
func (e *env) grant(org, ws, user uuid.UUID) {
	e.t.Helper()
	e.member(org, user, "member")
	team := e.insert(`INSERT INTO teams (organization_id, name) VALUES (?, ?) RETURNING id`, org, "Team "+uuid.NewString())
	e.exec(`INSERT INTO team_members (team_id, user_id, role) VALUES (?, ?, 'contributor')`, team, user)
	e.exec(`INSERT INTO workspace_teams (workspace_id, team_id, permission) VALUES (?, ?, 'read')`, ws, team)
}

func (e *env) key(ws uuid.UUID, key, dataType string) uuid.UUID {
	return e.insert(`INSERT INTO contact_attribute_keys (workspace_id, key, name, data_type) VALUES (?, ?, ?, ?) RETURNING id`, ws, key, TitleCase(key), dataType)
}

func (e *env) keyID(ws uuid.UUID, key string) uuid.UUID {
	return e.insert(`SELECT id FROM contact_attribute_keys WHERE workspace_id = ? AND key = ?`, ws, key)
}

// contact creates a contact with text values; a key that does not exist yet is created as a string key.
func (e *env) contact(ws uuid.UUID, attrs map[string]string) uuid.UUID {
	e.t.Helper()
	id := e.insert(`INSERT INTO contacts (workspace_id) VALUES (?) RETURNING id`, ws)
	for k, v := range attrs {
		e.setText(ws, id, k, v)
	}
	return id
}

func (e *env) ensureKey(ws uuid.UUID, key, dataType string) uuid.UUID {
	e.exec(`INSERT INTO contact_attribute_keys (workspace_id, key, name, data_type) VALUES (?, ?, ?, ?) ON CONFLICT (workspace_id, key) DO NOTHING`, ws, key, TitleCase(key), dataType)
	return e.keyID(ws, key)
}

func (e *env) setText(ws, contact uuid.UUID, key, value string) {
	e.exec(`INSERT INTO contact_attributes (contact_id, attribute_key_id, value_text) VALUES (?, ?, ?)`, contact, e.ensureKey(ws, key, TypeString), value)
}

func (e *env) setNumber(ws, contact uuid.UUID, key string, value float64) {
	e.exec(`INSERT INTO contact_attributes (contact_id, attribute_key_id, value_number) VALUES (?, ?, ?)`, contact, e.ensureKey(ws, key, TypeNumber), value)
}

func (e *env) setDate(ws, contact uuid.UUID, key string, value time.Time) {
	e.exec(`INSERT INTO contact_attributes (contact_id, attribute_key_id, value_date) VALUES (?, ?, ?)`, contact, e.ensureKey(ws, key, TypeDate), value)
}

// values reads a contact's values back as the API shows them.
func (e *env) values(contact uuid.UUID) map[string]string {
	e.t.Helper()
	attrs, err := e.repo.attributes(e.t.Context(), contact)
	if err != nil {
		e.t.Fatal(err)
	}
	return textValues(attrs)
}

func (e *env) survey(ws uuid.UUID, status string) uuid.UUID {
	return e.insert(`INSERT INTO surveys (workspace_id, name, status) VALUES (?, 'Survey', ?) RETURNING id`, ws, status)
}

func (e *env) segmentRow(ws uuid.UUID, title string, isPrivate bool, filters string) uuid.UUID {
	return e.insert(`INSERT INTO segments (workspace_id, title, is_private, filters) VALUES (?, ?, ?, ?) RETURNING id`, ws, title, isPrivate, filters)
}

// request sends a request as user, after delivering the projection; a nil user sends none.
func (e *env) request(user *uuid.UUID, method, path string, body io.Reader, contentType string, headers map[string]string) *httptest.ResponseRecorder {
	e.t.Helper()
	e.authz.Deliver(e.t)
	req := httptest.NewRequest(method, path, body)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Origin", webApp)
	req.Header.Set(httpapi.CSRFHeader, "XMLHttpRequest")
	if user != nil {
		req.Header.Set(testUserHeader, user.String())
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	e.engine.ServeHTTP(rec, req)
	return rec
}

func (e *env) do(user uuid.UUID, method, path string, body any) *httptest.ResponseRecorder {
	e.t.Helper()
	var r io.Reader
	contentType := ""
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			e.t.Fatal(err)
		}
		r, contentType = bytes.NewReader(b), "application/json"
	}
	return e.request(&user, method, path, r, contentType, nil)
}

// upload posts a CSV import form as a browser does, the file declared as text/csv.
func (e *env) upload(user, ws uuid.UUID, csv, action string, attributeMap map[string]string) *httptest.ResponseRecorder {
	e.t.Helper()
	return e.uploadFile(user, ws, csv, "contacts.csv", "text/csv", action, attributeMap)
}

func (e *env) uploadFile(user, ws uuid.UUID, csv, filename, contentType, action string, attributeMap map[string]string) *httptest.ResponseRecorder {
	e.t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	if action != "" {
		_ = w.WriteField("duplicateContactsAction", action)
	}
	if attributeMap != nil {
		raw, _ := json.Marshal(attributeMap)
		_ = w.WriteField("attributeMap", string(raw))
	}
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", `form-data; name="file"; filename="`+filename+`"`)
	if contentType != "" {
		header.Set("Content-Type", contentType)
	}
	part, err := w.CreatePart(header)
	if err != nil {
		e.t.Fatal(err)
	}
	_, _ = part.Write([]byte(csv))
	_ = w.Close()
	return e.request(&user, http.MethodPost, wsPath(ws, "/contacts/import"), &body, w.FormDataContentType(), nil)
}

func (e *env) ok(rec *httptest.ResponseRecorder, status int, out any) {
	e.t.Helper()
	if rec.Code != status {
		e.t.Fatalf("status %d, want %d: %s%s", rec.Code, status, rec.Body.String(), e.failureLog(rec))
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
		e.t.Fatalf("got %d %s, want %d %s %q%s", rec.Code, rec.Body.String(), status, code, message, e.failureLog(rec))
	}
	return body
}

func wsPath(id uuid.UUID, rest string) string {
	return "/api/app/workspaces/" + id.String() + rest
}

// failureLog is the server log of an unexpected 500, which the response hides.
func (e *env) failureLog(rec *httptest.ResponseRecorder) string {
	if rec.Code != http.StatusInternalServerError {
		return ""
	}
	return "\nlog: " + e.logs.String()
}
