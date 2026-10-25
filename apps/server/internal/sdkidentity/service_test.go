package sdkidentity

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

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/authz/authztest"
	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/audit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/apps/server/internal/platform/pgtest"
	"github.com/yldm-tech/forma/apps/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/secret"
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

// env is the settings routes on a fresh database with real authorization, mounted as the server mounts them.
type env struct {
	t      *testing.T
	db     *gorm.DB
	authz  *authztest.Env
	cipher *secret.Cipher
	engine *gin.Engine
	logs   *syncBuffer
}

func newEnv(t *testing.T) *env {
	t.Helper()
	t.Parallel()
	u, _ := url.Parse(webApp)
	cfg := config.Config{WebAppURL: u, RateLimitingDisabled: true, AuditLogEnabled: true, UserManagementMinimumRole: "manager", TrustedProxyHopCount: 1,
		EncryptionKey: bytes.Repeat([]byte{7}, 32), EncryptionKeyVersion: 1}
	db := pgtest.GORM(t)
	e := &env{t: t, db: db, authz: authztest.New(t, cfg, db), logs: &syncBuffer{}}
	log := slog.New(slog.NewJSONHandler(e.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	var err error
	if e.cipher, err = secret.NewCipher(cfg); err != nil {
		t.Fatal(err)
	}
	svc := NewService(db, e.cipher, e.authz.Authorizer, ratelimit.New(cfg, nil, log), audit.New(cfg, log))
	e.engine = gin.New()
	group := e.engine.Group("/api/app", httpapi.ErrorHandler(log), httpapi.CSRF(cfg), authz.DecisionMemo())
	NewHandler(svc, headerSessions{}).Register(group)
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

func (e *env) exec(query string, args ...any) {
	e.t.Helper()
	if err := e.db.Exec(query, args...).Error; err != nil {
		e.t.Fatalf("exec %q: %v", query, err)
	}
}

func (e *env) user() uuid.UUID {
	return e.insert(`INSERT INTO users (name, email) VALUES ('Ada', ?) RETURNING id`, uuid.NewString()+"@example.com")
}

// workspace creates an organization whose members have the given roles, and its workspace with the default attribute keys and a custom key plan.
func (e *env) workspace(members map[uuid.UUID]string) uuid.UUID {
	e.t.Helper()
	org := e.insert(`INSERT INTO organizations (name) VALUES ('Acme') RETURNING id`)
	for user, role := range members {
		e.exec(`INSERT INTO memberships (user_id, organization_id, role, accepted) VALUES (?, ?, ?, true)`, user, org, role)
	}
	ws := e.insert(`INSERT INTO workspaces (organization_id, name) VALUES (?, 'Main') RETURNING id`, org)
	for _, k := range []string{"userId", "email", "firstName", "plan"} {
		e.exec(`INSERT INTO contact_attribute_keys (workspace_id, key) VALUES (?, ?)`, ws, k)
	}
	return ws
}

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

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder, status int) T {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status %d, want %d: %s", rec.Code, status, rec.Body.String())
	}
	var out T
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode %s: %v", rec.Body.String(), err)
	}
	return out
}

func path(ws uuid.UUID, rest string) string {
	return "/api/app/workspaces/" + ws.String() + "/sdk-identity" + rest
}

// A manager generates a secret, shown only in that answer and stored sealed; requires verified identities and protects keys; regenerating replaces the secret; removing it stops requiring verification. Every change is audited without the secret.
func TestSettingsLifecycle(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	ws := e.workspace(map[uuid.UUID]string{owner: "owner"})

	v := decode[View](t, e.do(owner, http.MethodGet, path(ws, ""), nil), http.StatusOK)
	if v.HasSecret || v.Required || len(v.ProtectedAttributeKeys) != 0 {
		t.Fatalf("a new workspace's settings %+v", v)
	}

	rec := e.do(owner, http.MethodPatch, path(ws, ""), map[string]any{"required": true})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), CodeSecretMissing) {
		t.Fatalf("requiring verification without a secret: %d %s", rec.Code, rec.Body.String())
	}

	rec = e.do(owner, http.MethodPost, path(ws, "/secret"), nil)
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("the answer that shows the secret has Cache-Control %q", got)
	}
	first := decode[SecretView](t, rec, http.StatusOK)
	if first.Secret == "" || !first.HasSecret {
		t.Fatalf("generated %+v", first)
	}
	settings, err := Load(t.Context(), e.db, e.cipher, ws)
	if err != nil || settings.Secret != first.Secret {
		t.Fatalf("stored secret %q, %v", settings.Secret, err)
	}
	var stored []byte
	_ = e.db.Raw(`SELECT sdk_identity_secret FROM workspaces WHERE id = ?`, ws).Row().Scan(&stored)
	if bytes.Contains(stored, []byte(first.Secret)) {
		t.Fatal("the secret is stored in clear")
	}
	if strings.Contains(e.do(owner, http.MethodGet, path(ws, ""), nil).Body.String(), first.Secret) {
		t.Fatal("the settings show the secret after it was generated")
	}

	v = decode[View](t, e.do(owner, http.MethodPatch, path(ws, ""), map[string]any{"required": true, "protectedAttributeKeys": []string{"plan", "firstName", "plan"}}), http.StatusOK)
	if !v.Required || strings.Join(v.ProtectedAttributeKeys, ",") != "plan,firstName" {
		t.Fatalf("after the change %+v", v)
	}
	// A change that leaves a field out keeps it.
	v = decode[View](t, e.do(owner, http.MethodPatch, path(ws, ""), map[string]any{"required": true}), http.StatusOK)
	if strings.Join(v.ProtectedAttributeKeys, ",") != "plan,firstName" {
		t.Fatalf("protected keys after a change without them %v", v.ProtectedAttributeKeys)
	}

	second := decode[SecretView](t, e.do(owner, http.MethodPost, path(ws, "/secret"), nil), http.StatusOK)
	if second.Secret == first.Secret || !second.Required {
		t.Fatalf("regenerated %+v", second)
	}
	if settings, _ = Load(t.Context(), e.db, e.cipher, ws); settings.Verified("u1", UserHash(first.Secret, "u1")) || !settings.Verified("u1", UserHash(second.Secret, "u1")) {
		t.Fatal("the old secret still verifies, or the new one does not")
	}

	v = decode[View](t, e.do(owner, http.MethodDelete, path(ws, "/secret"), nil), http.StatusOK)
	if v.HasSecret || v.Required || len(v.ProtectedAttributeKeys) != 2 {
		t.Fatalf("after removing the secret %+v", v)
	}
	v = decode[View](t, e.do(owner, http.MethodPatch, path(ws, ""), map[string]any{"protectedAttributeKeys": []string{}}), http.StatusOK)
	if len(v.ProtectedAttributeKeys) != 0 {
		t.Fatalf("an empty list did not clear the protected keys: %v", v.ProtectedAttributeKeys)
	}

	logs := e.logs.String()
	if strings.Contains(logs, first.Secret) || strings.Contains(logs, second.Secret) {
		t.Fatal("an audit event carries the secret")
	}
	for _, mark := range []string{"sdkIdentitySecretRotated", "sdkIdentitySecretRemoved", `"sdkIdentityRequired":true`} {
		if !strings.Contains(logs, mark) {
			t.Errorf("no audit event records %s", mark)
		}
	}
}

// Only the workspace's own keys can be protected, and never userId or email.
func TestProtectedKeysMustBeTheWorkspaces(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	ws := e.workspace(map[uuid.UUID]string{owner: "owner"})
	for _, keys := range []any{[]string{"userId"}, []string{"email"}, []string{"unknown"}, []any{"plan", 5}, nil} {
		rec := e.do(owner, http.MethodPatch, path(ws, ""), map[string]any{"protectedAttributeKeys": keys})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("protecting %v: %d %s", keys, rec.Code, rec.Body.String())
		}
	}
	var keys string
	_ = e.db.Raw(`SELECT array_to_string(sdk_protected_attribute_keys, ',') FROM workspaces WHERE id = ?`, ws).Row().Scan(&keys)
	if keys != "" {
		t.Fatalf("a refused change stored %q", keys)
	}
}

// Members read the settings; only those who manage the workspace change them; outsiders and unknown workspaces are refused alike.
func TestSettingsAuthorization(t *testing.T) {
	e := newEnv(t)
	owner, member, outsider := e.user(), e.user(), e.user()
	ws := e.workspace(map[uuid.UUID]string{owner: "owner", member: "member"})
	e.workspace(map[uuid.UUID]string{outsider: "owner"})

	if rec := e.do(outsider, http.MethodGet, path(ws, ""), nil); rec.Code != http.StatusForbidden {
		t.Fatalf("an outsider read the settings: %d", rec.Code)
	}
	for _, req := range []struct {
		method, path string
		body         any
	}{
		{http.MethodPatch, path(ws, ""), map[string]any{"protectedAttributeKeys": []string{"plan"}}},
		{http.MethodPost, path(ws, "/secret"), nil},
		{http.MethodDelete, path(ws, "/secret"), nil},
	} {
		if rec := e.do(member, req.method, req.path, req.body); rec.Code != http.StatusForbidden {
			t.Fatalf("a member's %s %s: %d %s", req.method, req.path, rec.Code, rec.Body.String())
		}
	}
	if rec := e.do(owner, http.MethodPost, path(uuid.New(), "/secret"), nil); rec.Code != http.StatusForbidden && rec.Code != http.StatusNotFound {
		t.Fatalf("an unknown workspace: %d", rec.Code)
	}
	var hasSecret bool
	_ = e.db.Raw(`SELECT sdk_identity_secret IS NOT NULL FROM workspaces WHERE id = ?`, ws).Row().Scan(&hasSecret)
	if hasSecret {
		t.Fatal("a refused request generated a secret")
	}
}
