package analysis

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/authz/authztest"
	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/audit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpserver"
	"github.com/yldm-tech/forma/apps/server/internal/platform/jobs"
	"github.com/yldm-tech/forma/apps/server/internal/platform/pgtest"
	"github.com/yldm-tech/forma/apps/server/internal/platform/ratelimit"
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

// env is one isolated instance of the analysis module: a fresh database, real authorization and job enqueueing, and its /api/app routes as the server mounts them.
type env struct {
	t      *testing.T
	cfg    config.Config
	db     *gorm.DB
	authz  *authztest.Env
	svc    *Service
	engine *gin.Engine
}

func newEnv(t *testing.T) *env {
	t.Helper()
	t.Parallel()
	app, _ := url.Parse(webApp)
	public, _ := url.Parse("https://surveys.example.com")
	cfg := config.Config{WebAppURL: app, PublicURL: public, RateLimitingDisabled: true, AuditLogEnabled: true, UserManagementMinimumRole: "manager"}
	db := pgtest.GORM(t)
	e := &env{t: t, cfg: cfg, db: db, authz: authztest.New(t, cfg, db)}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	enqueuer, err := jobs.NewEnqueuer(log)
	if err != nil {
		t.Fatal(err)
	}
	e.svc = NewService(cfg, db, e.authz.Authorizer, enqueuer, audit.New(cfg, log), log)
	e.engine = httpserver.NewEngine(cfg, log)
	group := e.engine.Group("/api/app", httpapi.ErrorHandler(log), httpapi.CSRF(cfg), authz.DecisionMemo())
	NewHandler(e.svc, nil, headerSessions{}, ratelimit.New(cfg, nil, log)).Register(group)
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

// organization creates an organization owned by owner, with a display time zone, and one workspace.
func (e *env) organization(owner uuid.UUID, timeZone *string) (org, ws uuid.UUID) {
	e.t.Helper()
	org = e.insert(`INSERT INTO organizations (name, display_time_zone) VALUES ('Acme', ?) RETURNING id`, timeZone)
	e.exec(`INSERT INTO memberships (user_id, organization_id, role, accepted) VALUES (?, ?, 'owner', true)`, owner, org)
	ws = e.insert(`INSERT INTO workspaces (organization_id, name) VALUES (?, ?) RETURNING id`, org, "Workspace "+uuid.NewString()[:8])
	e.exec(`INSERT INTO contact_attribute_keys (workspace_id, key, type) VALUES (?, 'userId', 'default'), (?, 'email', 'default')`, ws, ws)
	return org, ws
}

// grant makes user a member of org in a new team with access to ws.
func (e *env) grant(org, ws, user uuid.UUID, permission string) {
	e.t.Helper()
	e.exec(`INSERT INTO memberships (user_id, organization_id, role, accepted) VALUES (?, ?, 'member', true) ON CONFLICT DO NOTHING`, user, org)
	team := e.insert(`INSERT INTO teams (organization_id, name) VALUES (?, ?) RETURNING id`, org, "Team "+uuid.NewString())
	e.exec(`INSERT INTO team_members (team_id, user_id, role) VALUES (?, ?, 'contributor')`, team, user)
	e.exec(`INSERT INTO workspace_teams (workspace_id, team_id, permission) VALUES (?, ?, ?)`, ws, team, permission)
}

type surveyOpts struct {
	name        string
	blocks      string
	welcomeCard bool
	captureIP   bool
	anonymize   bool
	verifyEmail bool
	archived    bool
}

func (e *env) survey(ws uuid.UUID, o surveyOpts) uuid.UUID {
	e.t.Helper()
	if o.name == "" {
		o.name = "Survey"
	}
	var archivedAt *time.Time
	if o.archived {
		now := time.Now()
		archivedAt = &now
	}
	welcome, _ := json.Marshal(map[string]any{"enabled": o.welcomeCard})
	return e.insert(`INSERT INTO surveys (workspace_id, name, type, status, blocks, welcome_card, endings, is_capture_ip_enabled, is_anonymize_responses_enabled, is_verify_email_enabled, archived_at)
		VALUES (?, ?, 'link', 'inProgress', ?::jsonb, ?::jsonb, '[]'::jsonb, ?, ?, ?, ?) RETURNING id`,
		ws, o.name, o.blocks, string(welcome), o.captureIP, o.anonymize, o.verifyEmail, archivedAt)
}

// field links a survey-local Embedded Data field.
func (e *env) field(ws, survey uuid.UUID, key, name, source, dataType string) {
	e.t.Helper()
	def := e.insert(`INSERT INTO embedded_data (workspace_id, survey_id, name, source, data_type) VALUES (?, ?, ?, ?, ?) RETURNING id`, ws, survey, name, source, dataType)
	position := e.count(`SELECT count(*) FROM survey_embedded_data WHERE survey_id = ?`, survey)
	e.exec(`INSERT INTO survey_embedded_data (workspace_id, survey_id, embedded_data_id, storage_key, position) VALUES (?, ?, ?, ?, ?)`, ws, survey, def, key, position)
}

// contact creates a contact of ws with the attributes given as key, value pairs.
func (e *env) contact(ws uuid.UUID, attrs ...string) uuid.UUID {
	e.t.Helper()
	c := e.insert(`INSERT INTO contacts (workspace_id) VALUES (?) RETURNING id`, ws)
	for i := 0; i+1 < len(attrs); i += 2 {
		e.exec(`INSERT INTO contact_attribute_keys (workspace_id, key) VALUES (?, ?) ON CONFLICT DO NOTHING`, ws, attrs[i])
		e.exec(`INSERT INTO contact_attributes (contact_id, attribute_key_id, value_text) SELECT ?, id, ? FROM contact_attribute_keys WHERE workspace_id = ? AND key = ?`, c, attrs[i+1], ws, attrs[i])
	}
	return c
}

func (e *env) display(survey uuid.UUID, contact *uuid.UUID, at time.Time) uuid.UUID {
	return e.insert(`INSERT INTO displays (survey_id, contact_id, created_at) VALUES (?, ?, ?) RETURNING id`, survey, contact, at)
}

// row is a response as the test seeds it; JSON columns are given as JSON text.
type row struct {
	at                time.Time
	finished          bool
	data, ttc, meta   string
	variables         string
	contactAttributes *string
	language          *string
	contact, display  *uuid.UUID
}

func (e *env) response(survey uuid.UUID, r row) uuid.UUID {
	e.t.Helper()
	for _, s := range []*string{&r.data, &r.ttc, &r.meta, &r.variables} {
		if *s == "" {
			*s = "{}"
		}
	}
	return e.insert(`INSERT INTO responses (survey_id, created_at, updated_at, finished, data, ttc, meta, variables, contact_attributes, language, contact_id, display_id)
		VALUES (?, ?, ?, ?, ?::jsonb, ?::jsonb, ?::jsonb, ?::jsonb, ?::jsonb, ?, ?, ?) RETURNING id`,
		survey, r.at, r.at, r.finished, r.data, r.ttc, r.meta, r.variables, r.contactAttributes, r.language, r.contact, r.display)
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

// decode reads a 200 answer into out.
func decode(t *testing.T, rec *httptest.ResponseRecorder, out any) {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
		t.Fatalf("decode %s: %v", rec.Body, err)
	}
}

func ptr[T any](v T) *T { return &v }
