package workflow

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
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/yldm-tech/forma/apps/server/internal/apikey"
	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/authz/authztest"
	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/audit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpserver"
	"github.com/yldm-tech/forma/apps/server/internal/platform/jobs"
	"github.com/yldm-tech/forma/apps/server/internal/platform/mailer"
	"github.com/yldm-tech/forma/apps/server/internal/platform/mailer/mailertest"
	"github.com/yldm-tech/forma/apps/server/internal/platform/pgtest"
	"github.com/yldm-tech/forma/apps/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/v3api"
	"github.com/yldm-tech/forma/apps/server/internal/response"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	os.Exit(m.Run())
}

const webApp = "https://app.example.com"

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

// env is one isolated workflows module: a fresh database, real authorization, the routes as the server mounts them, and the runner with a mail transport that records.
type env struct {
	t          *testing.T
	cfg        config.Config
	dsn        string
	db         *gorm.DB
	authz      *authztest.Env
	engine     *gin.Engine
	mail       *mailertest.Transport
	logs       *syncBuffer
	log        *slog.Logger
	mailer     *mailer.Mailer
	jobs       *jobs.Enqueuer
	enqueuer   *Enqueuer
	meter      *meterRecorder
	runner     *Runner
	reconciler *Reconciler
}

func newEnv(t *testing.T) *env {
	t.Helper()
	t.Parallel()
	u, _ := url.Parse(webApp)
	public, _ := url.Parse("https://surveys.example.com")
	dsn := pgtest.DSN(t)
	cfg := config.Config{
		WebAppURL: u, PublicURL: public, DatabaseURL: dsn, DatabaseMaxOpenConns: 10, ShutdownTimeout: 5 * time.Second, WorkerConcurrency: 2,
		RateLimitingDisabled: true, AuditLogEnabled: true, UserManagementMinimumRole: "manager", TrustedProxyHopCount: 1,
		EncryptionKey: bytes.Repeat([]byte{7}, 32), EncryptionKeyText: config.Secret(strings.Repeat("07", 32)), EncryptionKeyVersion: 1,
		MailFrom: "noreply@forma.yldm.ai", MailFromName: "Forma",
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	e := &env{t: t, cfg: cfg, dsn: dsn, db: db, authz: authztest.New(t, cfg, db), mail: &mailertest.Transport{}, logs: &syncBuffer{}}
	e.log = slog.New(slog.NewJSONHandler(e.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	if e.mailer, err = mailer.NewWithTransport(cfg, e.mail, e.log); err != nil {
		t.Fatal(err)
	}
	if e.jobs, err = jobs.NewEnqueuer(e.log); err != nil {
		t.Fatal(err)
	}
	limiter := ratelimit.New(cfg, nil, e.log)
	svc := NewService(NewRepository(db), db, e.authz.Authorizer, e.log)
	h := NewHandler(cfg, svc, v3api.NewGuard(cfg, headerSessions{}, apikey.NewAuthenticator(db, e.log), limiter, e.log), headerSessions{}, audit.New(cfg, e.log), e.log)
	e.engine = httpserver.NewEngine(cfg, e.log)
	h.Register(e.engine.Group("/api/app", httpapi.ErrorHandler(e.log), httpapi.CSRF(cfg), authz.DecisionMemo()))
	h.RegisterV3(e.engine)
	e.meter = &meterRecorder{}
	e.enqueuer = NewEnqueuer(db, e.jobs, e.meter, e.log)
	e.runner = NewRunner(cfg, db, e.mailer, e.log)
	e.reconciler = NewReconciler(db, e.jobs, e.log)
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

func (e *env) user(email string) uuid.UUID {
	return e.insert(`INSERT INTO users (name, email) VALUES ('Ada', ?) RETURNING id`, email)
}

func (e *env) member(org, user uuid.UUID, role string) {
	e.exec(`INSERT INTO memberships (user_id, organization_id, role, accepted) VALUES (?, ?, ?, true)`, user, org, role)
}

// grant puts user in a new team of org with a permission on ws.
func (e *env) grant(org, ws, user uuid.UUID, permission string) {
	team := e.insert(`INSERT INTO teams (organization_id, name) VALUES (?, ?) RETURNING id`, org, "Team "+uuid.NewString())
	e.exec(`INSERT INTO team_members (team_id, user_id, role) VALUES (?, ?, 'contributor')`, team, user)
	e.exec(`INSERT INTO workspace_teams (workspace_id, team_id, permission) VALUES (?, ?, ?)`, ws, team, permission)
}

// apiKey mints a legacy-format API key of org with a permission on ws and returns its secret.
func (e *env) apiKey(org, ws uuid.UUID, permission string) string {
	secret := "test-key-" + uuid.NewString()
	sum := sha256.Sum256([]byte(secret))
	digest := hex.EncodeToString(sum[:])
	id := e.insert(`INSERT INTO api_keys (organization_id, label, hashed_key, lookup_hash) VALUES (?, 'test', ?, ?) RETURNING id`, org, digest, digest)
	e.exec(`INSERT INTO api_key_workspaces (api_key_id, workspace_id, permission) VALUES (?, ?, ?)`, id, ws, permission)
	return secret
}

// fixture is an organization owned by owner@example.com with one workspace and a survey in it whose first element asks for an email.
type fixture struct {
	org, ws, survey, owner uuid.UUID
}

const blocks = `[{"id":"b1","name":"Block","elements":[
	{"id":"q1","type":"openText","headline":{"default":"Your email?"},"required":false,"inputType":"email"}
]}]`

func (e *env) fixture() fixture {
	e.t.Helper()
	org := e.insert(`INSERT INTO organizations (name, whitelabel) VALUES ('Acme', '{"logoUrl":"/storage/ws/public/logo.png"}') RETURNING id`)
	ws := e.insert(`INSERT INTO workspaces (organization_id, name) VALUES (?, 'Main') RETURNING id`, org)
	sv := e.insert(`INSERT INTO surveys (workspace_id, name, status, blocks, endings) VALUES (?, 'Feedback', 'inProgress', ?::jsonb, ?::jsonb) RETURNING id`, ws, blocks,
		`[{"id":"`+testEndingID+`","type":"endScreen"},{"id":"cm9zr4q7i000108l84gozfggs","type":"endScreen"}]`)
	owner := e.user("owner@example.com")
	e.member(org, owner, "owner")
	return fixture{org: org, ws: ws, survey: sv, owner: owner}
}

// definition is an executable definition on the survey: one send_email to `to` after the trigger, for the given endings.
func definition(survey uuid.UUID, to string, endings ...string) map[string]any {
	d := testDefinition()
	ids := []any{}
	for _, id := range endings {
		ids = append(ids, id)
	}
	d["trigger"].(map[string]any)["config"] = map[string]any{"surveyId": survey.String(), "endingCardIds": ids}
	cfg := d["nodes"].([]any)[0].(map[string]any)["config"].(map[string]any)
	cfg["to"] = to
	cfg["body"] = "<p>Hello #recall:q1/fallback:friend#</p><script>alert(1)</script>"
	cfg["replyTo"] = []any{"support@example.com", "help@example.com"}
	return d
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
		r = bytes.NewReader(raw(e.t, b))
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

// v3 sends an /api/v3 request as user.
func (e *env) v3(user uuid.UUID, method, path string, body any) *httptest.ResponseRecorder {
	return e.request(method, path, body, map[string]string{testUserHeader: user.String()})
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

func (e *env) errorLogs() string {
	var out []string
	for _, line := range strings.Split(e.logs.String(), "\n") {
		if strings.Contains(line, `"level":"ERROR"`) {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

type problem struct {
	Title         string               `json:"title"`
	Status        int                  `json:"status"`
	Detail        string               `json:"detail"`
	Code          string               `json:"code"`
	Instance      string               `json:"instance"`
	RequestID     string               `json:"requestId"`
	InvalidParams []v3api.InvalidParam `json:"invalid_params"`
}

// problem asserts an RFC 9457 problem with the status, code and detail, and returns it.
func (e *env) problem(rec *httptest.ResponseRecorder, status int, code, detail string) problem {
	e.t.Helper()
	var p problem
	_ = json.Unmarshal(rec.Body.Bytes(), &p)
	if rec.Code != status || p.Status != status || p.Code != code || (detail != "" && p.Detail != detail) ||
		rec.Header().Get("Content-Type") != "application/problem+json" || rec.Header().Get("Cache-Control") != v3api.CacheNoStore || p.RequestID == "" {
		e.t.Fatalf("got %d %s (%s), want %d %s %q\n%s", rec.Code, rec.Body.String(), rec.Header().Get("Content-Type"), status, code, detail, e.errorLogs())
	}
	return p
}

// respond stores a response to the survey and returns it as the pipeline carries it.
func (e *env) respond(survey uuid.UUID, finished bool, data string, ending *string) response.Response {
	e.t.Helper()
	id := e.insert(`INSERT INTO responses (survey_id, finished, data, ending_id) VALUES (?, ?, ?::jsonb, ?) RETURNING id`, survey, finished, data, ending)
	r, err := response.Load(context.Background(), e.db, id)
	if err != nil || r == nil {
		e.t.Fatalf("load response: %v", err)
	}
	return *r
}

// runJob is the run's job on its given attempt, as River hands it to the worker.
func runJob(run, workflowID, workspaceID uuid.UUID, attempt int) *river.Job[RunArgs] {
	return &river.Job[RunArgs]{
		JobRow: &rivertype.JobRow{ID: 1, Attempt: attempt, MaxAttempts: 3, Kind: RunArgs{}.Kind()},
		Args:   RunArgs{WorkflowRunID: run, WorkflowID: workflowID, WorkspaceID: workspaceID},
	}
}

// waitFor polls cond until it holds, failing the test after the deadline.
func waitFor(t *testing.T, what string, deadline time.Duration, cond func() bool) {
	t.Helper()
	end := time.Now().Add(deadline)
	for !cond() {
		if time.Now().After(end) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// meterRecorder records the usage the enqueuer reports, in place of Stripe's meter events.
type meterRecorder struct {
	mu   sync.Mutex
	runs []string
}

func (m *meterRecorder) ResponseCreated(context.Context, *string, uuid.UUID, time.Time) {}

func (m *meterRecorder) WorkflowRunCreated(_ context.Context, stripeCustomerID *string, runID uuid.UUID, createdAt time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	customer := "<nil>"
	if stripeCustomerID != nil {
		customer = *stripeCustomerID
	}
	m.runs = append(m.runs, customer+" "+runID.String()+" "+createdAt.UTC().Format(time.RFC3339Nano))
}

func (m *meterRecorder) metered() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.runs...)
}
