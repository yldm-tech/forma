package plain

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/server/internal/platform/pgtest"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	os.Exit(m.Run())
}

// forma2's identity.test.ts: the HMAC-SHA256 hex of the email with the secret, none without one.
func TestEmailHash(t *testing.T) {
	if got := EmailHash("", "john@example.com"); got != nil {
		t.Fatalf("hash without a secret: %v", *got)
	}
	if got := EmailHash("test-secret", "john@example.com"); got == nil || *got != "831412b17524e7d41b2f9085360840f35558068db4efe31cebb6375fbd1ac0a8" {
		t.Fatalf("hash %v", got)
	}
	if *EmailHash("test-secret", "a@example.com") == *EmailHash("test-secret", "b@example.com") {
		t.Fatal("the hash is not email-specific")
	}
}

const testUserHeader = "X-Test-User"

// headerSessions stands in for the credential module's sessions, which have their own tests.
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

type env struct {
	t      *testing.T
	db     *gorm.DB
	engine *gin.Engine
}

func newEnv(t *testing.T, plain config.PlainConfig, cloud bool) *env {
	t.Helper()
	u, _ := url.Parse("https://app.example.com")
	cfg := config.Config{WebAppURL: u, IsFormaCloud: cloud, Plain: plain}
	e := &env{t: t, db: pgtest.GORM(t), engine: gin.New()}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	NewHandler(cfg, e.db, headerSessions{}).Register(e.engine.Group("/api/app", httpapi.ErrorHandler(log)))
	return e
}

func (e *env) insert(query string, args ...any) uuid.UUID {
	e.t.Helper()
	var id uuid.UUID
	if err := e.db.Raw(query, args...).Row().Scan(&id); err != nil {
		e.t.Fatalf("insert %q: %v", query, err)
	}
	return id
}

// member adds the user to a new organization billed with the given Stripe snapshot (none when empty).
func (e *env) member(user uuid.UUID, snapshot string) {
	e.t.Helper()
	org := e.insert(`INSERT INTO organizations (name) VALUES ('Acme') RETURNING id`)
	if err := e.db.Exec(`INSERT INTO memberships (user_id, organization_id, role, accepted) VALUES (?, ?, 'owner', true)`, user, org).Error; err != nil {
		e.t.Fatal(err)
	}
	if snapshot != "" {
		if err := e.db.Exec(`INSERT INTO organization_billing (organization_id, limits, stripe) VALUES (?, '{"workspaces":1,"monthly":{"responses":250,"workflowRuns":null}}', ?::jsonb)`, org, snapshot).Error; err != nil {
			e.t.Fatal(err)
		}
	}
}

func (e *env) session(user uuid.UUID) (int, map[string]any) {
	e.t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/app/plain/session", nil)
	req.Header.Set(testUserHeader, user.String())
	rec := httptest.NewRecorder()
	e.engine.ServeHTTP(rec, req)
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return rec.Code, body
}

// Without PLAIN_APP_ID there is no chat, so the app never loads the widget.
func TestSessionUnconfigured(t *testing.T) {
	t.Parallel()
	e := newEnv(t, config.PlainConfig{ChatHMACSecret: "test-secret"}, true)
	user := e.insert(`INSERT INTO users (name, email) VALUES ('John Doe', 'john@example.com') RETURNING id`)
	if status, body := e.session(user); status != http.StatusNotFound || body["code"] != apperr.CodeNotFound {
		t.Fatalf("%d %v", status, body)
	}
}

// forma2's PlainChat props: the app id, the signed-in user, the email hash, and the active-customer label when the user is in an organization on a paid, active plan (customer.test.ts).
func TestSession(t *testing.T) {
	t.Parallel()
	plain := config.PlainConfig{AppID: "liveChatApp_123", ChatHMACSecret: "test-secret", ActiveCustomerLabelTypeID: "lt_paying"}
	e := newEnv(t, plain, true)
	user := e.insert(`INSERT INTO users (name, email) VALUES ('John Doe', 'john@example.com') RETURNING id`)
	status, body := e.session(user)
	if status != http.StatusOK || body["appId"] != "liveChatApp_123" || body["userId"] != user.String() || body["email"] != "john@example.com" || body["name"] != "John Doe" ||
		body["emailHash"] != "831412b17524e7d41b2f9085360840f35558068db4efe31cebb6375fbd1ac0a8" || body["activeCustomerLabelTypeId"] != nil {
		t.Fatalf("%d %v", status, body)
	}
	for _, tc := range []struct {
		name     string
		snapshot string
		active   bool
	}{
		{"no stripe billing", `null`, false},
		{"hobby, active", `{"plan":"hobby","subscriptionStatus":"active"}`, false},
		{"custom, active", `{"plan":"custom","subscriptionStatus":"active"}`, false},
		{"pro, canceled", `{"plan":"pro","subscriptionStatus":"canceled"}`, false},
		{"pro, active", `{"plan":"pro","subscriptionStatus":"active"}`, true},
		{"scale, trialing", `{"plan":"scale","subscriptionStatus":"trialing"}`, true},
	} {
		u := e.insert(`INSERT INTO users (name, email) VALUES ('U', ?) RETURNING id`, uuid.NewString()+"@example.com")
		e.member(u, `{"plan":"hobby","subscriptionStatus":"active"}`)
		e.member(u, tc.snapshot)
		_, got := e.session(u)
		if want := tc.active; (got["activeCustomerLabelTypeId"] == "lt_paying") != want {
			t.Errorf("%s: %v", tc.name, got)
		}
	}
}

// Stripe plans exist only on Forma Cloud; a self-hosted instance never labels a customer, and without a secret the chat runs unverified.
func TestSessionSelfHosted(t *testing.T) {
	t.Parallel()
	e := newEnv(t, config.PlainConfig{AppID: "liveChatApp_123", ActiveCustomerLabelTypeID: "lt_paying"}, false)
	user := e.insert(`INSERT INTO users (name, email) VALUES ('John Doe', 'john@example.com') RETURNING id`)
	e.member(user, `{"plan":"scale","subscriptionStatus":"active"}`)
	if status, body := e.session(user); status != http.StatusOK || body["emailHash"] != nil || body["activeCustomerLabelTypeId"] != nil {
		t.Fatalf("%d %v", status, body)
	}
}
