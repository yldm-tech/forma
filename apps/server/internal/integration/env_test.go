package integration

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
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/authz/authztest"
	"github.com/yldm-tech/forma/apps/server/internal/embeddeddata"
	"github.com/yldm-tech/forma/apps/server/internal/pipeline"
	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/audit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpserver"
	"github.com/yldm-tech/forma/apps/server/internal/platform/pgtest"
	"github.com/yldm-tech/forma/apps/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/secret"
	"github.com/yldm-tech/forma/apps/server/internal/platform/valkey/valkeytest"
	"github.com/yldm-tech/forma/apps/server/internal/response"
	"github.com/yldm-tech/forma/apps/server/internal/responsemail"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	os.Exit(m.Run())
}

const webApp = "https://app.example.com"

// Headers that stand in for the credential module's sessions, which have their own tests: the acting user and their session.
const (
	testUserHeader    = "X-Test-User"
	testSessionHeader = "X-Test-Session"
)

type headerSessions struct{}

func actorOf(c *gin.Context) (httpapi.Actor, bool) {
	id, err := uuid.Parse(c.GetHeader(testUserHeader))
	if err != nil {
		return httpapi.Actor{}, false
	}
	session, _ := uuid.Parse(c.GetHeader(testSessionHeader))
	return httpapi.Actor{UserID: id, SessionID: session}, true
}

func (headerSessions) Require() gin.HandlerFunc {
	return func(c *gin.Context) {
		a, ok := actorOf(c)
		if !ok {
			httpapi.Fail(c, apperr.NotAuthenticated("Not authenticated"))
			return
		}
		httpapi.SetActor(c, a)
		c.Next()
	}
}

func (headerSessions) Optional() gin.HandlerFunc {
	return func(c *gin.Context) {
		if a, ok := actorOf(c); ok {
			httpapi.SetActor(c, a)
		}
		c.Next()
	}
}

// syncBuffer collects the log, audit events included, from concurrent work.
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

// call is one request a fake provider received.
type call struct {
	Method string
	Path   string
	Query  url.Values
	Header http.Header
	Body   []byte
}

// fake speaks the providers' APIs on one local server, under a path prefix per provider; each test registers the endpoints it needs and reads back what was sent.
type fake struct {
	srv   *httptest.Server
	mux   *http.ServeMux
	mu    sync.Mutex
	calls []call
}

func newFake(t *testing.T) *fake {
	t.Helper()
	f := &fake{mux: http.NewServeMux()}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.calls = append(f.calls, call{Method: r.Method, Path: r.URL.Path, Query: r.URL.Query(), Header: r.Header.Clone(), Body: body})
		f.mu.Unlock()
		r.Body = io.NopCloser(bytes.NewReader(body))
		f.mux.ServeHTTP(w, r)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fake) handle(pattern string, h http.HandlerFunc) { f.mux.HandleFunc(pattern, h) }

// reply registers an endpoint that always answers status with body.
func (f *fake) reply(pattern string, status int, body string) {
	f.handle(pattern, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	})
}

// to are the requests sent with method to a path that starts with prefix.
func (f *fake) to(method, prefix string) []call {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []call
	for _, c := range f.calls {
		if c.Method == method && strings.HasPrefix(c.Path, prefix) {
			out = append(out, c)
		}
	}
	return out
}

// point sends the providers' requests to the fake.
func (f *fake) point(p *providers) {
	u := f.srv.URL
	p.http = f.srv.Client()
	p.googleAuthURL, p.googleTokenURL, p.googleTokenInfo = u+"/google/auth", u+"/google/token", u+"/google/tokeninfo"
	p.googleAPIs, p.sheets = u+"/googleapis/", u+"/sheets/"
	p.airtable, p.airtableAPI = u+"/airtable", u+"/airtableapi"
	p.notion, p.slack = u+"/notion", u+"/slack"
}

// env is one isolated instance of the integration module: a fresh database and Valkey database, real authorization and rate limits, its /api/app and OAuth routes as the server mounts them, the pipeline hand-off, and a fake of every provider.
type env struct {
	t         *testing.T
	cfg       config.Config
	db        *gorm.DB
	rdb       *redis.Client
	authz     *authztest.Env
	cipher    *secret.Cipher
	repo      *Repository
	svc       *Service
	deliverer *Deliverer
	engine    *gin.Engine
	logs      *syncBuffer
	fake      *fake
	now       time.Time
}

func newEnv(t *testing.T) *env {
	t.Helper()
	t.Parallel()
	app, _ := url.Parse(webApp)
	public, _ := url.Parse("https://surveys.example.com")
	cfg := config.Config{
		WebAppURL: app, PublicURL: public, AuditLogEnabled: true, UserManagementMinimumRole: "manager",
		EncryptionKey: bytes.Repeat([]byte{7}, 32), EncryptionKeyVersion: 1,
		Integrations: config.IntegrationsConfig{
			GoogleSheetsClientID: "google-id", GoogleSheetsClientSecret: "google-secret", GoogleSheetsRedirectURL: webApp + "/api/google-sheet/callback",
			AirtableClientID: "airtable-id", NotionClientID: "notion-id", NotionClientSecret: "notion-secret", SlackClientID: "slack-id", SlackClientSecret: "slack-secret",
		},
	}
	db := pgtest.GORM(t)
	e := &env{t: t, cfg: cfg, db: db, rdb: valkeytest.Client(t), authz: authztest.New(t, cfg, db), logs: &syncBuffer{}, fake: newFake(t), now: time.Now()}
	log := slog.New(slog.NewJSONHandler(e.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	var err error
	if e.cipher, err = secret.NewCipher(cfg); err != nil {
		t.Fatal(err)
	}
	e.repo = NewRepository(db, e.cipher, log)
	frozen := e.now
	limiter := ratelimit.New(cfg, e.rdb, log).WithClock(func() time.Time { return frozen })
	e.svc = NewService(cfg, e.repo, e.authz.Authorizer, limiter, audit.New(cfg, log), e.rdb, log)
	e.fake.point(e.svc.p)
	e.deliverer = NewDeliverer(cfg, e.repo, e.rdb, log)
	e.fake.point(e.deliverer.p)
	// The writers' and the refresh lock's pauses, shortened so the tests do not sleep.
	e.deliverer.airtime = airtableTiming{fieldCreateGap: time.Millisecond, fieldWaitRetries: 3, fieldWaitGap: time.Millisecond}
	fast := airtableRefreshTiming{lockTTL: 30 * time.Second, waitGap: time.Millisecond, waitAttempts: 3}
	e.deliverer.tokens.refresh, e.svc.tokens.refresh = fast, fast
	e.engine = httpserver.NewEngine(cfg, log)
	group := e.engine.Group("/api/app", httpapi.ErrorHandler(log), httpapi.CSRF(cfg), authz.DecisionMemo())
	NewHandler(e.svc, headerSessions{}).Register(group)
	NewOAuthHandler(e.svc, headerSessions{}, limiter, log).Register(e.engine)
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
	return org, e.insert(`INSERT INTO workspaces (organization_id, name) VALUES (?, ?) RETURNING id`, org, "W "+uuid.NewString()[:8])
}

// grant makes user a member of org on a team with permission on ws.
func (e *env) grant(org, ws, user uuid.UUID, permission string) {
	e.exec(`INSERT INTO memberships (user_id, organization_id, role, accepted) VALUES (?, ?, 'member', true) ON CONFLICT DO NOTHING`, user, org)
	team := e.insert(`INSERT INTO teams (organization_id, name) VALUES (?, ?) RETURNING id`, org, "Team "+uuid.NewString())
	e.exec(`INSERT INTO team_members (team_id, user_id, role) VALUES (?, ?, 'contributor')`, team, user)
	e.exec(`INSERT INTO workspace_teams (workspace_id, team_id, permission) VALUES (?, ?, ?)`, ws, team, permission)
}

// surveyBlocks is forma2's handle-integrations test survey: an open text whose headline recalls the second element, a multiple choice, and a picture selection.
const surveyBlocks = `[{"id":"block1","name":"Block 1","elements":[
	{"id":"q1","type":"openText","headline":{"default":"Question 1 #recall:q2/fallback:none#"},"required":true,"inputType":"text"},
	{"id":"q2","type":"multipleChoiceMulti","headline":{"default":"Question 2"},"required":true,"choices":[{"id":"choice1","label":{"default":"Choice 1"}},{"id":"choice2","label":{"default":"Choice 2"}}]},
	{"id":"q3_picture","type":"pictureSelection","headline":{"default":"Question 3"},"required":true,"choices":[{"id":"picChoice1","imageUrl":"/storage/ws1/public/1.png"},{"id":"picChoice2","imageUrl":"http://image.com/2"}]},
	{"id":"q4_files","type":"fileUpload","headline":{"default":"Files"},"required":false}
]}]`

// survey creates the test survey in ws with the hidden field hidden1 and the variable var1 ("Variable 1").
func (e *env) survey(ws uuid.UUID) uuid.UUID {
	e.t.Helper()
	id := e.insert(`INSERT INTO surveys (workspace_id, name, status, blocks) VALUES (?, 'Test Survey', 'inProgress', ?::jsonb) RETURNING id`, ws, surveyBlocks)
	vars := []embeddeddata.Variable{{ID: "var1", Name: "Variable 1", Type: "text", Value: ""}}
	hidden := []string{"hidden1"}
	if err := embeddeddata.Reconcile(context.Background(), e.db, id, ws, embeddeddata.Patch{Variables: &vars, HiddenFieldIDs: &hidden}); err != nil {
		e.t.Fatal(err)
	}
	return id
}

const responseData = `{"q1":"Answer 1","q2":["Choice 1","Choice 2"],"q3_picture":["picChoice1"],"q4_files":["/storage/ws1/private/a.pdf","https://cdn.example.com/b.png"],"hidden1":"Hidden Value"}`

const responseMeta = `{"url":"http://example.com","source":"web","userAgent":{"browser":"Chrome","os":"Mac OS","device":"Desktop"},"country":"USA","action":"Action Name","ipAddress":"203.0.113.7"}`

// respond stores a finished response to sv created at 2024-01-01T12:00:00Z and returns the pipeline event for it.
func (e *env) respond(ws, sv uuid.UUID, contactAttributes string) *pipeline.Event {
	e.t.Helper()
	id := e.insert(`INSERT INTO responses (survey_id, finished, data, variables, meta, contact_attributes, created_at) VALUES (?, true, ?::jsonb, '{"var1":"Variable Value"}'::jsonb, ?::jsonb, ?::jsonb, '2024-01-01T12:00:00Z') RETURNING id`,
		sv, responseData, responseMeta, contactAttributes)
	r, err := response.Load(context.Background(), e.db, id)
	if err != nil || r == nil {
		e.t.Fatalf("load response: %v", err)
	}
	s, err := responsemail.LoadSurvey(context.Background(), e.db, sv)
	if err != nil || s == nil {
		e.t.Fatalf("load survey: %v", err)
	}
	return &pipeline.Event{Name: response.EventResponseFinished, WorkspaceID: ws, Response: *r, DisplayTimeZone: "UTC", Survey: &pipeline.Survey{Survey: s}}
}

// connect stores an integration of ws with credentials and links, as a callback and a save would have.
func (e *env) connect(ws uuid.UUID, typ string, key any, email string, links any) uuid.UUID {
	e.t.Helper()
	raw, err := json.Marshal(key)
	if err != nil {
		e.t.Fatal(err)
	}
	id, err := e.repo.Connect(context.Background(), ws, typ, Credentials{Key: raw, Email: email})
	if err != nil {
		e.t.Fatal(err)
	}
	if links != nil {
		config, err := json.Marshal(map[string]any{"data": links})
		if err != nil {
			e.t.Fatal(err)
		}
		if err := e.repo.SaveConfig(context.Background(), id, config); err != nil {
			e.t.Fatal(err)
		}
	}
	return id
}

type reqHeaders map[string]string

func asUser(id uuid.UUID) reqHeaders { return reqHeaders{testUserHeader: id.String()} }

func (e *env) do(method, path string, h reqHeaders, body any) *httptest.ResponseRecorder {
	e.t.Helper()
	e.authz.Deliver(e.t)
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
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

func ptr[T any](v T) *T { return &v }
