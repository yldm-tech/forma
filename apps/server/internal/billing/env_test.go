package billing

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
	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/audit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/apps/server/internal/platform/pgtest"
	"github.com/yldm-tech/forma/apps/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/valkey/valkeytest"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	os.Exit(m.Run())
}

const webApp = "https://app.example.com"

// syncBuffer lets handlers log while the test reads.
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

// env is billing on a fresh database against the fake Stripe API: Forma Cloud with Stripe configured unless a test configures otherwise.
type env struct {
	t      *testing.T
	cfg    config.Config
	db     *gorm.DB
	rdb    *redis.Client
	stripe *fakeStripe
	engine *Engine
	logs   *syncBuffer
	log    *slog.Logger
	now    time.Time
}

func newEnv(t *testing.T, configure ...func(*config.Config)) *env {
	t.Helper()
	t.Parallel()
	u, _ := url.Parse(webApp)
	cfg := config.Config{WebAppURL: u, IsFormaCloud: true, RateLimitingDisabled: true, AuditLogEnabled: true, TrustedProxyHopCount: 1}
	cfg.Stripe.SecretKey, cfg.Stripe.WebhookSecret = "sk_test_123", "whsec_test"
	for _, f := range configure {
		f(&cfg)
	}
	e := &env{t: t, cfg: cfg, db: pgtest.GORM(t), rdb: valkeytest.Client(t), stripe: newFakeStripe(t), logs: &syncBuffer{}, now: time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC)}
	e.log = slog.New(slog.NewJSONHandler(e.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	s := stripeFor(t, cfg, e.stripe, e.log)
	if cfg.Stripe.SecretKey == "" {
		s = newStripe(cfg, nil, e.log)
	}
	s.now = e.clock
	e.engine = NewEngine(cfg, s, NewRepository(e.db), e.rdb, e.log)
	e.engine.now = e.clock
	return e
}

func (e *env) clock() time.Time { return e.now }

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

func (e *env) user(name, email string) uuid.UUID {
	return e.insert(`INSERT INTO users (name, email) VALUES (?, ?) RETURNING id`, name, email)
}

// organization creates an organization owned by a new user, with no billing row: what sign-up leaves behind.
func (e *env) organization() (org, owner uuid.UUID) {
	org = e.insert(`INSERT INTO organizations (name) VALUES ('Acme') RETURNING id`)
	owner = e.user("Ada Owner", "ada-"+uuid.NewString()+"@example.com")
	e.exec(`INSERT INTO memberships (user_id, organization_id, role, accepted) VALUES (?, ?, 'owner', true)`, owner, org)
	return org, owner
}

// customerOrganization is an organization whose billing row holds the given snapshot and a Stripe customer: cus_123 for the first one of the test, a fresh id after that (the column is unique).
func (e *env) customerOrganization(snapshot string) uuid.UUID {
	org, _ := e.organization()
	customer := "cus_123"
	if e.count(`SELECT count(*) FROM organization_billing WHERE stripe_customer_id = 'cus_123'`) > 0 {
		customer = "cus_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	}
	e.exec(`INSERT INTO organization_billing (organization_id, stripe_customer_id, limits, stripe) VALUES (?, ?, '{"workspaces":1,"monthly":{"responses":250,"workflowRuns":null}}', ?::jsonb)`, org, customer, snapshot)
	return org
}

func (e *env) record(org uuid.UUID) *Record {
	e.t.Helper()
	rec, err := e.engine.repo.scanRecord(context.Background(), e.db, `SELECT `+recordColumns+` FROM organization_billing WHERE organization_id = ?`, org)
	if err != nil || rec == nil {
		e.t.Fatalf("billing of %s: %v", org, err)
	}
	return rec
}

// subscriptionsOn answers the customer's subscription list, the products of its prices, the customer and its entitlements.
func (e *env) subscriptionsOn(subs []any, entitlements []string, customer map[string]any) {
	e.stripe.on("GET /v1/subscriptions", func(*http.Request) any { return list(subs...) })
	e.stripe.on("GET /v1/products/{id}", func(r *http.Request) any {
		return product(strings.TrimPrefix(r.PathValue("id"), "prod_"))
	})
	var ents []any
	for i, key := range entitlements {
		ents = append(ents, map[string]any{"id": "ent_" + string(rune('a'+i)), "object": "entitlements.active_entitlement", "lookup_key": key})
	}
	e.stripe.on("GET /v1/entitlements/active_entitlements", func(*http.Request) any { return list(ents...) })
	if customer == nil {
		customer = map[string]any{"id": "cus_123", "object": "customer", "email": "ada@example.com", "invoice_settings": map[string]any{"default_payment_method": nil}}
	}
	e.stripe.on("GET /v1/customers/{id}", func(*http.Request) any { return customer })
}

// app is the billing routes and the Stripe webhook as the server mounts them, with real authorization; the acting user arrives in a test header.
type app struct {
	*env
	authz  *authztest.Env
	svc    *Service
	router *gin.Engine
}

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
	return func(c *gin.Context) { c.Next() }
}

func newApp(t *testing.T, configure ...func(*config.Config)) *app {
	e := newEnv(t, configure...)
	a := &app{env: e, authz: authztest.New(t, e.cfg, e.db)}
	a.svc = NewService(e.engine, a.authz.Authorizer, ratelimit.New(e.cfg, nil, e.log), audit.New(e.cfg, e.log))
	a.svc.pollDelay = 0
	a.router = gin.New()
	NewWebhookHandler(e.engine, e.log).Register(a.router)
	NewHandler(a.svc, headerSessions{}).Register(a.router.Group("/api/app", httpapi.ErrorHandler(e.log), httpapi.CSRF(e.cfg), authz.DecisionMemo()))
	return a
}

func (a *app) member(org, user uuid.UUID, role string) {
	a.exec(`INSERT INTO memberships (user_id, organization_id, role, accepted) VALUES (?, ?, ?, true)`, user, org, role)
}

// do sends a request as user after delivering the authorization projection.
func (a *app) do(user uuid.UUID, method, path string, body any) *httptest.ResponseRecorder {
	a.t.Helper()
	a.authz.Deliver(a.t)
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			a.t.Fatal(err)
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
	a.router.ServeHTTP(rec, req)
	return rec
}

func (a *app) ok(rec *httptest.ResponseRecorder, out any) {
	a.t.Helper()
	if rec.Code != http.StatusOK {
		a.t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if out != nil {
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			a.t.Fatalf("decode %s: %v", rec.Body.String(), err)
		}
	}
}

func (a *app) fails(rec *httptest.ResponseRecorder, status int, code string) {
	a.t.Helper()
	var body httpapi.Envelope
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Code != status || body.Code != code {
		a.t.Fatalf("got %d %s, want %d %s", rec.Code, rec.Body.String(), status, code)
	}
}

func billingPath(org uuid.UUID, suffix string) string {
	return "/api/app/organizations/" + org.String() + "/billing" + suffix
}

// auditEvents returns the audit records logged so far.
func (e *env) auditEvents() []map[string]any {
	var out []map[string]any
	for _, line := range strings.Split(e.logs.String(), "\n") {
		var rec map[string]any
		if json.Unmarshal([]byte(line), &rec) == nil && rec["msg"] == "audit" {
			out = append(out, rec)
		}
	}
	return out
}
