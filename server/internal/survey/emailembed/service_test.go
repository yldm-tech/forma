package emailembed

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
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
	"github.com/yldm-tech/forma/server/internal/platform/httpserver"
	"github.com/yldm-tech/forma/server/internal/platform/mailer"
	"github.com/yldm-tech/forma/server/internal/platform/mailer/mailertest"
	"github.com/yldm-tech/forma/server/internal/platform/pgtest"
	"github.com/yldm-tech/forma/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/server/internal/platform/secret"
	"github.com/yldm-tech/forma/server/internal/survey"
	"github.com/yldm-tech/forma/server/internal/survey/surveydoc"
)

const (
	webApp    = "http://app.example.com"
	publicApp = "https://surveys.example.com"
)

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

// routeEnv is the module behind its /api/app routes: a fresh database, real authorization, and a mailer that keeps what it sends.
type routeEnv struct {
	t      *testing.T
	db     *gorm.DB
	authz  *authztest.Env
	mail   *mailertest.Transport
	engine *gin.Engine
}

func newRouteEnv(t *testing.T) *routeEnv {
	t.Helper()
	t.Parallel()
	gin.SetMode(gin.TestMode)
	u, _ := url.Parse(webApp)
	pub, _ := url.Parse(publicApp)
	cfg := config.Config{
		WebAppURL: u, PublicURL: pub, RateLimitingDisabled: true, UserManagementMinimumRole: "manager", TrustedProxyHopCount: 1, SurveySchedulingTimeZone: "UTC",
		EncryptionKey: bytes.Repeat([]byte{7}, 32), EncryptionKeyText: config.Secret(strings.Repeat("07", 32)), EncryptionKeyVersion: 1, MailFrom: "noreply@example.com",
	}
	db := pgtest.GORM(t)
	e := &routeEnv{t: t, db: db, authz: authztest.New(t, cfg, db), mail: &mailertest.Transport{}}
	log := slog.New(slog.DiscardHandler)
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
	limiter := ratelimit.New(cfg, nil, log)
	surveys := survey.NewService(cfg, survey.NewRepository(db), e.authz.Authorizer, limiter, audit.New(cfg, log), surveydoc.NewValidator(schemas), cipher, catalog, nil, log)
	m, err := mailer.NewWithTransport(cfg, e.mail, log)
	if err != nil {
		t.Fatal(err)
	}
	svc, err := NewService(cfg, db, surveys, e.authz.Authorizer, m)
	if err != nil {
		t.Fatal(err)
	}
	e.engine = httpserver.NewEngine(log)
	group := e.engine.Group("/api/app", httpapi.ErrorHandler(log), httpapi.CSRF(cfg), authz.DecisionMemo())
	NewHandler(svc, headerSessions{}).Register(group)
	return e
}

func (e *routeEnv) insert(query string, args ...any) uuid.UUID {
	e.t.Helper()
	var id uuid.UUID
	if err := e.db.Raw(query, args...).Row().Scan(&id); err != nil {
		e.t.Fatalf("insert %q: %v", query, err)
	}
	return id
}

func (e *routeEnv) exec(query string, args ...any) {
	e.t.Helper()
	if err := e.db.Exec(query, args...).Error; err != nil {
		e.t.Fatalf("exec %q: %v", query, err)
	}
}

func (e *routeEnv) user(email, locale string) uuid.UUID {
	return e.insert(`INSERT INTO users (name, email, locale) VALUES ('Ada', ?, ?) RETURNING id`, email, locale)
}

func (e *routeEnv) do(user uuid.UUID, method, path string) *httptest.ResponseRecorder {
	e.t.Helper()
	e.authz.Deliver(e.t)
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Origin", webApp)
	req.Header.Set(httpapi.CSRFHeader, "XMLHttpRequest")
	req.Header.Set(testUserHeader, user.String())
	rec := httptest.NewRecorder()
	e.engine.ServeHTTP(rec, req)
	return rec
}

func (e *routeEnv) status(rec *httptest.ResponseRecorder, status int, code string) {
	e.t.Helper()
	var body httpapi.Envelope
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Code != status || (code != "" && body.Code != code) {
		e.t.Fatalf("got %d %s, want %d %s", rec.Code, rec.Body.String(), status, code)
	}
}

// The share modal's Email tab: the embed for writers of the survey's workspace, the preview mailed to any reader, in their language, the survey's links without the preview mark and under the organization's logo.
func TestEmailRoutes(t *testing.T) {
	e := newRouteEnv(t)
	owner, reader, outsider := e.user("owner@example.com", "ja-JP"), e.user("reader@example.com", "en-US"), e.user("out@example.com", "en-US")
	org := e.insert(`INSERT INTO organizations (name, whitelabel) VALUES ('Acme', '{"logoUrl": "/storage/org/public/logo.png"}') RETURNING id`)
	e.exec(`INSERT INTO memberships (user_id, organization_id, role, accepted) VALUES (?, ?, 'owner', true)`, owner, org)
	ws := e.insert(`INSERT INTO workspaces (organization_id, name, styling) VALUES (?, 'W', '{"allowStyleOverwrite": true, "brandColor": {"light": "#ff0000"}}') RETURNING id`, org)
	e.exec(`INSERT INTO memberships (user_id, organization_id, role, accepted) VALUES (?, ?, 'member', true)`, reader, org)
	team := e.insert(`INSERT INTO teams (organization_id, name) VALUES (?, 'T') RETURNING id`, org)
	e.exec(`INSERT INTO team_members (team_id, user_id, role) VALUES (?, ?, 'contributor')`, team, reader)
	e.exec(`INSERT INTO workspace_teams (workspace_id, team_id, permission) VALUES (?, ?, 'read')`, ws, team)
	outsiderOrg := e.insert(`INSERT INTO organizations (name) VALUES ('Other') RETURNING id`)
	e.exec(`INSERT INTO memberships (user_id, organization_id, role, accepted) VALUES (?, ?, 'owner', true)`, outsider, outsiderOrg)
	blocks := `[{"id":"b1","name":"Block 1","elements":[{"id":"q1","type":"consent","headline":{"default":"Agree?"},"label":{"default":"I agree"},"required":false}]}]`
	sv := e.insert(`INSERT INTO surveys (workspace_id, name, type, status, blocks, styling) VALUES (?, 'S', 'link', 'inProgress', ?, '{"overwriteThemeStyling": true, "brandColor": {"light": "#00ff00"}}') RETURNING id`, ws, blocks)
	path := "/api/app/surveys/" + sv.String()

	rec := e.do(owner, http.MethodGet, path+"/email-html")
	e.status(rec, http.StatusOK, "")
	var embed struct {
		HTML string `json:"html"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &embed); err != nil {
		t.Fatal(err)
	}
	surveyLink := publicApp + "/s/" + sv.String() + "?preview=true&amp;q1=accepted&amp;skipPrefilled=true"
	if !strings.Contains(embed.HTML, "Agree?") || !strings.Contains(embed.HTML, surveyLink) || !strings.Contains(embed.HTML, "承認") || !strings.Contains(embed.HTML, "#00ff00") || strings.Contains(embed.HTML, "#ff0000") {
		t.Fatalf("embed: %s", embed.HTML)
	}
	e.status(e.do(reader, http.MethodGet, path+"/email-html"), http.StatusForbidden, "forbidden")
	e.status(e.do(owner, http.MethodGet, "/api/app/surveys/"+uuid.NewString()+"/email-html"), http.StatusNotFound, "not_found")

	rec = e.do(reader, http.MethodPost, path+"/email-preview")
	e.status(rec, http.StatusOK, "")
	if rec.Body.String() != `{"sent":true}` {
		t.Fatalf("preview answer %s", rec.Body.String())
	}
	sent := e.mail.Messages()
	if len(sent) != 1 || sent[0].To != "reader@example.com" || sent[0].Subject != "Forma Email Survey Preview" {
		t.Fatalf("sent %+v", sent)
	}
	body := sent[0].HTML
	if strings.Contains(body, "preview=true") || !strings.Contains(body, publicApp+"/s/"+sv.String()+"?q1=accepted&amp;skipPrefilled=true") || !strings.Contains(body, "Accept") ||
		!strings.Contains(body, ws.String()) || !strings.Contains(body, publicApp+"/storage/org/public/logo.png") {
		t.Fatalf("preview email: %s", body)
	}
	e.status(e.do(outsider, http.MethodPost, path+"/email-preview"), http.StatusForbidden, "forbidden")
	if len(e.mail.Messages()) != 1 {
		t.Fatal("a refused preview was sent")
	}
}
