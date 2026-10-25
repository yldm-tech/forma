package contactsurvey

import (
	"bytes"
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
	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"
	"gorm.io/gorm"

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
	"github.com/yldm-tech/forma/apps/server/internal/platform/valkey/valkeytest"
	"github.com/yldm-tech/forma/apps/server/internal/survey"
	"github.com/yldm-tech/forma/apps/server/internal/survey/surveydoc"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	os.Exit(m.Run())
}

const (
	webApp    = "http://app.example.com"
	publicApp = "https://surveys.example.com"
)

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

// env is one isolated instance of the module: a fresh database and Valkey, real authorization and rate limits on a frozen clock, and its /api/app routes as the server mounts them.
type env struct {
	t       *testing.T
	cfg     config.Config
	db      *gorm.DB
	authz   *authztest.Env
	limiter *ratelimit.Limiter
	surveys *survey.Service
	svc     *Service
	engine  *gin.Engine
	logs    *syncBuffer
	now     time.Time
}

func newEnv(t *testing.T) *env {
	t.Helper()
	t.Parallel()
	u, _ := url.Parse(webApp)
	pub, _ := url.Parse(publicApp)
	cfg := config.Config{
		WebAppURL: u, PublicURL: pub, AuditLogEnabled: true, UserManagementMinimumRole: "manager", TrustedProxyHopCount: 1, SurveySchedulingTimeZone: "UTC",
		EncryptionKey: bytes.Repeat([]byte{7}, 32), EncryptionKeyText: config.Secret(strings.Repeat("07", 32)), EncryptionKeyVersion: 1,
	}
	db := pgtest.GORM(t)
	e := &env{t: t, cfg: cfg, db: db, authz: authztest.New(t, cfg, db), logs: &syncBuffer{}, now: time.Date(2026, 10, 23, 9, 30, 0, 0, time.UTC)}
	log := slog.New(slog.NewJSONHandler(e.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	cipher, err := secret.NewCipher(cfg)
	if err != nil {
		t.Fatal(err)
	}
	schemas, err := jsonschema.New()
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := survey.LoadCatalog()
	if err != nil {
		t.Fatal(err)
	}
	e.limiter = ratelimit.New(cfg, valkeytest.Client(t), log).WithClock(func() time.Time { return e.now })
	emitter := audit.New(cfg, log)
	e.surveys = survey.NewService(cfg, survey.NewRepository(db), e.authz.Authorizer, e.limiter, emitter, surveydoc.NewValidator(schemas), cipher, catalog, nil, log)
	e.svc = NewService(cfg, db, e.surveys, e.authz.Authorizer, e.limiter, emitter, log)
	e.svc.now = func() time.Time { return e.now }
	e.engine = httpserver.NewEngine(cfg, log)
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

func (e *env) user() uuid.UUID {
	return e.insert(`INSERT INTO users (name, email) VALUES ('Ada', ?) RETURNING id`, uuid.NewString()+"@example.com")
}

// organization creates an organization owned by owner, with a workspace.
func (e *env) organization(owner uuid.UUID) (org, ws uuid.UUID) {
	e.t.Helper()
	org = e.insert(`INSERT INTO organizations (name) VALUES ('Acme') RETURNING id`)
	e.exec(`INSERT INTO memberships (user_id, organization_id, role, accepted) VALUES (?, ?, 'owner', true)`, owner, org)
	return org, e.workspace(org)
}

func (e *env) workspace(org uuid.UUID) uuid.UUID {
	return e.insert(`INSERT INTO workspaces (organization_id, name) VALUES (?, ?) RETURNING id`, org, "Workspace "+uuid.NewString())
}

// grant makes user a member of org who may read ws through a team.
func (e *env) grant(org, ws, user uuid.UUID) {
	e.t.Helper()
	e.exec(`INSERT INTO memberships (user_id, organization_id, role, accepted) VALUES (?, ?, 'member', true)`, user, org)
	team := e.insert(`INSERT INTO teams (organization_id, name) VALUES (?, ?) RETURNING id`, org, "Team "+uuid.NewString())
	e.exec(`INSERT INTO team_members (team_id, user_id, role) VALUES (?, ?, 'contributor')`, team, user)
	e.exec(`INSERT INTO workspace_teams (workspace_id, team_id, permission) VALUES (?, ?, 'read')`, ws, team)
}

// contact creates a contact created at the given time, with text values; their keys are created as needed.
func (e *env) contact(ws uuid.UUID, createdAt time.Time, attrs map[string]string) uuid.UUID {
	e.t.Helper()
	id := e.insert(`INSERT INTO contacts (workspace_id, created_at) VALUES (?, ?) RETURNING id`, ws, createdAt)
	for k, v := range attrs {
		e.exec(`INSERT INTO contact_attribute_keys (workspace_id, key, name) VALUES (?, ?, ?) ON CONFLICT (workspace_id, key) DO NOTHING`, ws, k, k)
		key := e.insert(`SELECT id FROM contact_attribute_keys WHERE workspace_id = ? AND key = ?`, ws, k)
		e.exec(`INSERT INTO contact_attributes (contact_id, attribute_key_id, value_text) VALUES (?, ?, ?)`, id, key, v)
	}
	return id
}

// linkSurvey creates a published link survey; singleUse is its singleUse document, or "" for the default (off).
func (e *env) linkSurvey(ws uuid.UUID, name, singleUse string) uuid.UUID {
	if singleUse == "" {
		return e.insert(`INSERT INTO surveys (workspace_id, name, type, status) VALUES (?, ?, 'link', 'inProgress') RETURNING id`, ws, name)
	}
	return e.insert(`INSERT INTO surveys (workspace_id, name, type, status, single_use) VALUES (?, ?, 'link', 'inProgress', ?) RETURNING id`, ws, name, singleUse)
}

func (e *env) segment(ws uuid.UUID, filters string) uuid.UUID {
	return e.insert(`INSERT INTO segments (workspace_id, title, is_private, filters) VALUES (?, ?, false, ?) RETURNING id`, ws, "Segment "+uuid.NewString(), filters)
}

// do sends a request as user, after delivering the projection.
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

func (e *env) ok(rec *httptest.ResponseRecorder, out any) {
	e.t.Helper()
	if rec.Code != http.StatusOK {
		e.t.Fatalf("status %d: %s\nlog: %s", rec.Code, rec.Body.String(), e.logs.String())
	}
	if out != nil {
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			e.t.Fatalf("decode %s: %v", rec.Body.String(), err)
		}
	}
}

func (e *env) fails(rec *httptest.ResponseRecorder, status int, code string) {
	e.t.Helper()
	var body httpapi.Envelope
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Code != status || body.Code != code {
		e.t.Fatalf("got %d %s, want %d %s", rec.Code, rec.Body.String(), status, code)
	}
}

// auditEvents are the audit events logged so far.
func (e *env) auditEvents() []map[string]any {
	var out []map[string]any
	for _, line := range strings.Split(e.logs.String(), "\n") {
		var entry map[string]any
		if json.Unmarshal([]byte(line), &entry) == nil && entry["msg"] == "audit" {
			out = append(out, entry)
		}
	}
	return out
}

// personalLink is what a personal link says once its token is verified and its ids decrypted, as the link page reads it.
type personalLink struct {
	contactID, surveyID string
	issuedAt, expiry    time.Time
	query               url.Values
}

func (e *env) decodeLink(link string) personalLink {
	e.t.Helper()
	rest, ok := strings.CutPrefix(link, publicApp+"/c/")
	if !ok {
		e.t.Fatalf("link %q is not a personal link on the public domain", link)
	}
	token, rawQuery, _ := strings.Cut(rest, "?")
	parsed, err := jwt.ParseSigned(token, []jose.SignatureAlgorithm{jose.HS256})
	if err != nil {
		e.t.Fatal(err)
	}
	var claims struct {
		ContactID string `json:"contactId"`
		SurveyID  string `json:"surveyId"`
		jwt.Claims
	}
	if err := parsed.Claims([]byte(e.cfg.EncryptionKeyText), &claims); err != nil {
		e.t.Fatal(err)
	}
	out := personalLink{issuedAt: claims.IssuedAt.Time()}
	if claims.Expiry != nil {
		out.expiry = claims.Expiry.Time()
	}
	if out.contactID, err = secret.DecryptLinkValue(claims.ContactID, e.cfg.EncryptionKey); err != nil {
		e.t.Fatal(err)
	}
	if out.surveyID, err = secret.DecryptLinkValue(claims.SurveyID, e.cfg.EncryptionKey); err != nil {
		e.t.Fatal(err)
	}
	if out.query, err = url.ParseQuery(rawQuery); err != nil {
		e.t.Fatal(err)
	}
	return out
}
