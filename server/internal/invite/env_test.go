package invite

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
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/server/internal/auth"
	"github.com/yldm-tech/forma/server/internal/authz"
	"github.com/yldm-tech/forma/server/internal/authz/spicedbtest"
	"github.com/yldm-tech/forma/server/internal/platform/audit"
	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/server/internal/platform/mailer"
	"github.com/yldm-tech/forma/server/internal/platform/mailer/mailertest"
	"github.com/yldm-tech/forma/server/internal/platform/pgtest"
	"github.com/yldm-tech/forma/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/server/internal/platform/secret"
	"github.com/yldm-tech/forma/server/internal/platform/valkey/valkeytest"
)

const webApp = "http://forma.test"

// env is the invite routes on a real PostgreSQL and SpiceDB, with real sessions and captured mail. Rate limits are off unless the test asks for Valkey.
type env struct {
	t          *testing.T
	cfg        config.Config
	db         *gorm.DB
	delivery   *authz.Delivery
	authorizer *authz.Authorizer
	svc        *Service
	mail       *mailertest.Transport
	logs       *syncBuffer
	engine     *gin.Engine
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

func baseConfig() config.Config {
	webAppURL, _ := url.Parse(webApp)
	return config.Config{
		WebAppURL: webAppURL, UserManagementMinimumRole: "manager", AuditLogEnabled: true, SessionMaxAge: 86400,
		RateLimitingDisabled: true, InviteRateLimitPer24Hours: 50,
		SMTPHost: "smtp.example.com", SMTPPort: "587", MailFrom: "noreply@forma.yldm.ai", MailFromName: "Forma",
	}
}

func newEnv(t *testing.T, configure ...func(*config.Config)) *env {
	t.Helper()
	gin.SetMode(gin.TestMode)
	cfg := baseConfig()
	for _, f := range configure {
		f(&cfg)
	}
	schema, err := os.ReadFile("../authz/schema.zed")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Authz = spicedbtest.Config(t, string(schema))
	db := pgtest.GORM(t)
	logs := &syncBuffer{}
	log := slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	client, err := authz.NewClient(cfg.Authz, authz.RequestProfile, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	transport := &mailertest.Transport{}
	m, err := mailer.NewWithTransport(cfg, transport, log)
	if err != nil {
		t.Fatal(err)
	}
	limiter := ratelimit.New(cfg, nil, log)
	if !cfg.RateLimitingDisabled {
		limiter = ratelimit.New(cfg, valkeytest.Client(t), log)
	}

	e := &env{t: t, cfg: cfg, db: db, mail: transport, logs: logs, delivery: authz.NewDelivery(db, client, log), authorizer: authz.NewAuthorizer(cfg, db, client)}
	e.svc = NewService(cfg, NewRepository(db), e.authorizer, limiter, m, audit.New(cfg, log), log)
	r := gin.New()
	r.Use(httpapi.NewClientIPResolver(cfg, log).Middleware())
	group := r.Group("/api/app", httpapi.ErrorHandler(log), httpapi.CSRF(cfg), authz.DecisionMemo())
	NewHandler(e.svc, auth.NewSessions(cfg, db, log)).Register(group)
	e.engine = r
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
	if err := e.db.Raw(query, args...).Scan(&n).Error; err != nil {
		e.t.Fatalf("count %q: %v", query, err)
	}
	return n
}

// user is a signed-in user: the session token goes in the request's cookie.
type user struct {
	id    uuid.UUID
	email string
	token string
}

func (e *env) userWith(name, email, locale string) user {
	e.t.Helper()
	id := e.insert(`INSERT INTO users (name, email, email_verified, locale) VALUES (?, ?, true, ?) RETURNING id`, name, email, locale)
	token, err := secret.NewToken()
	if err != nil {
		e.t.Fatal(err)
	}
	e.exec(`INSERT INTO sessions (user_id, token_hash, expires_at) VALUES (?, ?, now() + interval '1 day')`, id, secret.HashToken(token))
	return user{id: id, email: email, token: token}
}

func (e *env) user(name string) user {
	return e.userWith(name, strings.ToLower(name)+"-"+uuid.NewString()[:8]+"@example.com", "en-US")
}

func (e *env) join(org uuid.UUID, u user, role string) user {
	e.exec(`INSERT INTO memberships (organization_id, user_id, role, accepted) VALUES (?, ?, ?, true)`, org, u.id, role)
	return u
}

type org struct {
	id                              uuid.UUID
	owner, manager, member, billing user
}

// organization is an organization with one user of each role, projected to SpiceDB.
func (e *env) organization() org {
	e.t.Helper()
	id := e.insert(`INSERT INTO organizations (name) VALUES ('Acme') RETURNING id`)
	o := org{id: id, owner: e.join(id, e.user("Owner"), "owner"), manager: e.join(id, e.user("Manager"), "manager"), member: e.join(id, e.user("Member"), "member"), billing: e.join(id, e.user("Billing"), "billing")}
	return o
}

func (e *env) team(org uuid.UUID, name string) uuid.UUID {
	return e.insert(`INSERT INTO teams (organization_id, name) VALUES (?, ?) RETURNING id`, org, name)
}

func (e *env) teamMember(team uuid.UUID, u user, role string) {
	e.exec(`INSERT INTO team_members (team_id, user_id, role) VALUES (?, ?, ?)`, team, u.id, role)
}

// drain delivers the projection outbox to SpiceDB, as `forma worker` does every five seconds.
func (e *env) drain() {
	e.t.Helper()
	res, err := e.delivery.Drain(context.Background(), authz.DefaultDrainBatches)
	if err != nil || res.Failed != 0 || res.Status != "drained" {
		e.t.Fatalf("Drain = %+v, %v; want everything delivered", res, err)
	}
}

type response struct {
	*httptest.ResponseRecorder
}

func (r response) json() map[string]any {
	var m map[string]any
	_ = json.Unmarshal(r.Body.Bytes(), &m)
	return m
}

func (r response) code() string {
	c, _ := r.json()["code"].(string)
	return c
}

func (r response) message() string {
	m, _ := r.json()["message"].(string)
	return m
}

// do sends a request as the user the way the SPA does: session cookie, the app's Origin and the CSRF header. The zero user is anonymous.
func (e *env) do(u user, method, path string, body any) response {
	e.t.Helper()
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			e.t.Fatal(err)
		}
		reader = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, "/api/app"+path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Origin", webApp)
	req.Header.Set(httpapi.CSRFHeader, "XMLHttpRequest")
	if u.token != "" {
		req.AddCookie(&http.Cookie{Name: "forma.session_token", Value: u.token})
	}
	w := httptest.NewRecorder()
	e.engine.ServeHTTP(w, req)
	return response{w}
}

// expect checks the status, the code when given and the message when given.
func expect(t *testing.T, r response, status int, code string, message ...string) {
	t.Helper()
	if r.Code != status || (code != "" && r.code() != code) || (len(message) > 0 && r.message() != message[0]) {
		t.Fatalf("got %d %s, want %d %s %v", r.Code, r.Body.String(), status, code, message)
	}
}

var tokenPattern = regexp.MustCompile(`/invite\?token=([A-Za-z0-9_-]+)`)

// sent returns the messages delivered to an address.
func (e *env) sent(to string) []mailer.Message {
	var out []mailer.Message
	for _, m := range e.mail.Messages() {
		if m.To == to {
			out = append(out, m)
		}
	}
	return out
}

// lastLink is the invite token in the last mail to an address.
func (e *env) lastLink(to string) string {
	e.t.Helper()
	msgs := e.sent(to)
	if len(msgs) == 0 {
		e.t.Fatalf("no mail to %s", to)
	}
	m := tokenPattern.FindStringSubmatch(msgs[len(msgs)-1].HTML)
	if m == nil {
		e.t.Fatalf("no invite link in the mail to %s", to)
	}
	return m[1]
}

// seedInvite stores an invite with one link directly, the way a migrated or stale invite would exist, and returns the link's token.
func (e *env) seedInvite(org, creator uuid.UUID, email, role string, inviteTTL time.Duration, teams ...uuid.UUID) (uuid.UUID, string) {
	e.t.Helper()
	id := e.insert(`INSERT INTO invites (organization_id, email, name, creator_id, expires_at, role) VALUES (?, ?, 'Invitee', ?, ?, ?) RETURNING id`, org, email, creator, time.Now().Add(inviteTTL), role)
	for _, team := range teams {
		e.exec(`INSERT INTO invite_teams (invite_id, team_id) VALUES (?, ?)`, id, team)
	}
	token, err := secret.NewToken()
	if err != nil {
		e.t.Fatal(err)
	}
	e.exec(`INSERT INTO invite_tokens (invite_id, token_hash, expires_at) VALUES (?, ?, ?)`, id, secret.HashToken(token), time.Now().Add(inviteLifetime))
	return id, token
}

// auditEvents returns the audit records logged so far with the action and target.
func (e *env) auditEvents(action audit.Action, target audit.Target) []map[string]any {
	e.t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(e.logs.String()), "\n") {
		var m map[string]any
		if json.Unmarshal([]byte(line), &m) != nil {
			continue
		}
		tgt, _ := m["target"].(map[string]any)
		if m["msg"] == "audit" && m["action"] == string(action) && tgt["type"] == string(target) {
			out = append(out, m)
		}
	}
	return out
}
