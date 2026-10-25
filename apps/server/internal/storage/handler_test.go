package storage

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/apikey"
	"github.com/yldm-tech/forma/apps/server/internal/authz/authztest"
	"github.com/yldm-tech/forma/apps/server/internal/platform/audit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpserver"
	"github.com/yldm-tech/forma/apps/server/internal/platform/pgtest"
	"github.com/yldm-tech/forma/apps/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/valkey/valkeytest"
	"github.com/yldm-tech/forma/apps/server/internal/storage/s3test"
	"github.com/yldm-tech/forma/apps/server/internal/workspace"
)

// testUserHeader carries the signed-in user; sessionDownHeader makes the session store fail. headerSessions stands in for the credential module's sessions, which have their own tests.
const (
	testUserHeader    = "X-Test-User"
	sessionDownHeader = "X-Test-Session-Down"
)

type headerSessions struct{}

func (s headerSessions) Require() gin.HandlerFunc { return s.Optional() }

func (headerSessions) Optional() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.GetHeader(sessionDownHeader) != "" {
			httpapi.Fail(c, errors.New("session store unreachable"))
			return
		}
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

// handlerEnv is the /storage route as the server mounts it, over a fresh database, a fresh bucket and real authorization.
type handlerEnv struct {
	t      *testing.T
	db     *gorm.DB
	authz  *authztest.Env
	s3     config.S3Config
	engine *gin.Engine
	logs   *syncBuffer
}

func newHandlerEnv(t *testing.T, configure ...func(*config.Config)) *handlerEnv {
	t.Helper()
	t.Parallel()
	web, _ := url.Parse("http://app.example.com")
	s3 := s3test.Config(t)
	cfg := config.Config{WebAppURL: web, AuditLogEnabled: true, UserManagementMinimumRole: "manager", TrustedProxyHopCount: 1, S3: s3}
	for _, f := range configure {
		f(&cfg)
	}
	db := pgtest.GORM(t)
	e := &handlerEnv{t: t, db: db, authz: authztest.New(t, cfg, db), s3: cfg.S3, logs: &syncBuffer{}}
	log := slog.New(slog.NewJSONHandler(e.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	bucket, err := NewBucket(cfg, log)
	if err != nil {
		t.Fatal(err)
	}
	// Windows are aligned to the wall clock; a frozen one keeps a scenario inside one window.
	frozen := time.Now()
	limiter := ratelimit.New(cfg, valkeytest.Client(t), log).WithClock(func() time.Time { return frozen })
	h := NewHandler(cfg, NewService(bucket), workspace.NewRepository(db, log), headerSessions{}, apikey.NewAuthenticator(db, log), e.authz.Authorizer, limiter, audit.New(cfg, log), log)
	e.engine = httpserver.NewEngine(cfg, log)
	e.engine.Use(httpapi.NewClientIPResolver(cfg, log).Middleware())
	h.Register(e.engine)
	return e
}

func (e *handlerEnv) exec(query string, args ...any) {
	e.t.Helper()
	if err := e.db.Exec(query, args...).Error; err != nil {
		e.t.Fatalf("exec %q: %v", query, err)
	}
}

func (e *handlerEnv) insert(query string, args ...any) uuid.UUID {
	e.t.Helper()
	var id uuid.UUID
	if err := e.db.Raw(query, args...).Row().Scan(&id); err != nil {
		e.t.Fatalf("insert %q: %v", query, err)
	}
	return id
}

func (e *handlerEnv) user() uuid.UUID {
	return e.insert(`INSERT INTO users (name, email) VALUES ('Ada', ?) RETURNING id`, uuid.NewString()+"@example.com")
}

// organization creates an organization owned by owner, with one workspace.
func (e *handlerEnv) organization(owner uuid.UUID) (org, ws uuid.UUID) {
	org = e.insert(`INSERT INTO organizations (name) VALUES ('Acme') RETURNING id`)
	e.member(org, owner, "owner")
	return org, e.insert(`INSERT INTO workspaces (organization_id, name) VALUES (?, 'Main') RETURNING id`, org)
}

func (e *handlerEnv) member(org, user uuid.UUID, role string) {
	e.exec(`INSERT INTO memberships (user_id, organization_id, role, accepted) VALUES (?, ?, ?, true)`, user, org, role)
}

// grant puts user in a new team of org with access to ws.
func (e *handlerEnv) grant(org, ws, user uuid.UUID, permission string) {
	team := e.insert(`INSERT INTO teams (organization_id, name) VALUES (?, ?) RETURNING id`, org, "Team "+uuid.NewString())
	e.exec(`INSERT INTO team_members (team_id, user_id, role) VALUES (?, ?, 'contributor')`, team, user)
	e.exec(`INSERT INTO workspace_teams (workspace_id, team_id, permission) VALUES (?, ?, ?)`, ws, team, permission)
}

// apiKey mints a legacy-format API key of org with a permission on ws and returns its secret.
func (e *handlerEnv) apiKey(org, ws uuid.UUID, permission string) string {
	raw := "test-key-" + uuid.NewString()
	sum := sha256.Sum256([]byte(raw))
	digest := hex.EncodeToString(sum[:])
	id := e.insert(`INSERT INTO api_keys (organization_id, label, hashed_key, lookup_hash) VALUES (?, 'test', ?, ?) RETURNING id`, org, digest, digest)
	e.exec(`INSERT INTO api_key_workspaces (api_key_id, workspace_id, permission) VALUES (?, ?, ?)`, id, ws, permission)
	return raw
}

func (e *handlerEnv) put(key, contentType, body string) { put(e.t, e.s3, key, contentType, body) }

func (e *handlerEnv) exists(key string) bool { return exists(e.t, e.s3, key) }

// as is who a request comes from.
type as map[string]string

func asUser(id uuid.UUID) as { return as{testUserHeader: id.String()} }

func asKey(key string) as { return as{"x-api-key": key} }

func (e *handlerEnv) do(method, path string, headers as) *httptest.ResponseRecorder {
	e.t.Helper()
	e.authz.Deliver(e.t)
	req := httptest.NewRequest(method, path, nil)
	for k, v := range headers {
		if k == "Host" {
			req.Host = v
			continue
		}
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	e.engine.ServeHTTP(rec, req)
	return rec
}

// expectJSON checks a forma2 JSON answer: status, body, Cache-Control, and whether the route's CORS headers came with it.
func expectJSON(t *testing.T, rec *httptest.ResponseRecorder, status int, body string, cors bool) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status %d, want %d (body %s)", rec.Code, status, rec.Body.String())
	}
	var got, want any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body %q: %v", rec.Body.String(), err)
	}
	if err := json.Unmarshal([]byte(body), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("body %s, want %s", rec.Body.String(), body)
	}
	h := rec.Header()
	if h.Get("Content-Type") != "application/json" || h.Get("Cache-Control") != "private, no-store" {
		t.Errorf("Content-Type %q, Cache-Control %q", h.Get("Content-Type"), h.Get("Cache-Control"))
	}
	wantCORS := map[string]string{}
	if cors {
		wantCORS = map[string]string{"Access-Control-Allow-Origin": "*", "Access-Control-Allow-Methods": "GET, POST, PUT, DELETE, OPTIONS", "Access-Control-Allow-Headers": "Content-Type, Authorization"}
	}
	for _, k := range []string{"Access-Control-Allow-Origin", "Access-Control-Allow-Methods", "Access-Control-Allow-Headers"} {
		if h.Get(k) != wantCORS[k] {
			t.Errorf("%s = %q, want %q", k, h.Get(k), wantCORS[k])
		}
	}
}

const notAuthenticatedBody = `{"code":"not_authenticated","message":"Not authenticated","details":{"x-Api-Key":"Header not provided or API Key invalid"}}`
const unauthorizedBody = `{"code":"unauthorized","message":"You are not authorized to access this resource","details":{}}`

func TestDownloadPublicFile(t *testing.T) {
	e := newHandlerEnv(t)
	_, ws := e.organization(e.user())
	e.put(ws.String()+"/public/logo--fid--1.png", "image/png", "png-bytes")
	path := "/storage/" + ws.String() + "/public/logo--fid--1.png"

	// No authentication, no rate limit: survey pages load their images from here.
	for range 8 {
		rec := e.do(http.MethodGet, path, nil)
		if rec.Code != http.StatusOK || rec.Body.String() != "png-bytes" {
			t.Fatalf("GET %d %q", rec.Code, rec.Body.String())
		}
	}
	rec := e.do(http.MethodGet, path, nil)
	want := map[string]string{
		"Content-Type":                "image/png",
		"Content-Length":              "9",
		"Cache-Control":               "public, max-age=31536000, immutable",
		"X-Frame-Options":             "SAMEORIGIN",
		"X-Content-Type-Options":      "nosniff",
		"Access-Control-Allow-Origin": "",
		"Content-Disposition":         "",
		"Set-Cookie":                  "",
	}
	for k, v := range want {
		if got := rec.Header().Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.HasSuffix(csp, "frame-ancestors 'self'") {
		t.Errorf("CSP %q", csp)
	}

	head := e.do(http.MethodHead, path, nil)
	if head.Code != http.StatusOK || head.Body.Len() != 0 || head.Header().Get("Content-Type") != "image/png" || head.Header().Get("Content-Length") != "9" {
		t.Fatalf("HEAD %d %q %v", head.Code, head.Body.String(), head.Header())
	}

	// A public file needs no session, so the session store is never asked: its outage does not take survey images down.
	if rec := e.do(http.MethodGet, path, as{sessionDownHeader: "1"}); rec.Code != http.StatusOK {
		t.Fatalf("public GET with the session store down: %d", rec.Code)
	}

	options := e.do(http.MethodOptions, path, nil)
	if options.Code != http.StatusNoContent || options.Header().Get("Allow") != "DELETE, GET, HEAD, OPTIONS" || options.Body.Len() != 0 {
		t.Fatalf("OPTIONS %d %v %q", options.Code, options.Header(), options.Body.String())
	}
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch} {
		if rec := e.do(method, path, nil); rec.Code != http.StatusMethodNotAllowed || rec.Body.Len() != 0 {
			t.Errorf("%s %d %q", method, rec.Code, rec.Body.String())
		}
	}
}

func TestDownloadPrivateFile(t *testing.T) {
	e := newHandlerEnv(t)
	owner, reader, outsider := e.user(), e.user(), e.user()
	org, ws := e.organization(owner)
	e.member(org, reader, "member")
	e.grant(org, ws, reader, "read")
	otherOrg, otherWS := e.organization(outsider)

	const name = "surveys/s1/elements/e1/report--fid--1.pdf"
	e.put(ws.String()+"/private/"+name, "application/pdf", "secret")
	path := "/storage/" + ws.String() + "/private/" + name

	expectJSON(t, e.do(http.MethodGet, path, nil), http.StatusUnauthorized, notAuthenticatedBody, false)
	expectJSON(t, e.do(http.MethodGet, path, asKey("not-a-key")), http.StatusUnauthorized, notAuthenticatedBody, false)
	expectJSON(t, e.do(http.MethodGet, path, asUser(outsider)), http.StatusUnauthorized, unauthorizedBody, false)
	// A key of another organization authenticates but may not read this workspace.
	expectJSON(t, e.do(http.MethodGet, path, asKey(e.apiKey(otherOrg, otherWS, "manage"))), http.StatusUnauthorized, unauthorizedBody, false)

	for name, who := range map[string]as{"owner": asUser(owner), "reader": asUser(reader), "api key": asKey(e.apiKey(org, ws, "read"))} {
		rec := e.do(http.MethodGet, path, who)
		if rec.Code != http.StatusOK || rec.Body.String() != "secret" {
			t.Fatalf("%s: %d %q", name, rec.Code, rec.Body.String())
		}
		if got := rec.Header().Get("Cache-Control"); got != "no-store, no-cache, must-revalidate" {
			t.Errorf("%s: Cache-Control %q", name, got)
		}
	}

	// A private file is never served through the public access type.
	expectJSON(t, e.do(http.MethodGet, "/storage/"+ws.String()+"/public/"+name, nil), http.StatusNotFound,
		`{"code":"not_found","message":"file not found","details":{"resource_id":"`+name+`","resource_type":"file"}}`, true)

	// A session store outage is not a denial.
	if rec := e.do(http.MethodGet, path, as{sessionDownHeader: "1"}); rec.Code != http.StatusInternalServerError || rec.Body.Len() != 0 {
		t.Fatalf("session store down: %d %q", rec.Code, rec.Body.String())
	}

	// A private file is answered with its extension's type, whatever type it was stored with: browsers take the last type of a list, so "text/plain;,text/html" would run a respondent's script on the admin origin.
	for stored, want := range map[string]string{
		"crafted--fid--1.txt": "text/plain",
		"photo--fid--2.JPG":   "image/jpeg",
		"page--fid--3.html":   "application/octet-stream",
	} {
		e.put(ws.String()+"/private/"+stored, "text/plain;,text/html", "<script>alert(1)</script>")
		rec := e.do(http.MethodGet, "/storage/"+ws.String()+"/private/"+stored, asUser(owner))
		if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != want {
			t.Errorf("%s: %d %q, want %q", stored, rec.Code, rec.Header().Get("Content-Type"), want)
		}
	}
}

func TestDownloadErrors(t *testing.T) {
	e := newHandlerEnv(t)
	_, ws := e.organization(e.user())
	base := "/storage/" + ws.String()

	expectJSON(t, e.do(http.MethodGet, "/storage/clunknownaaaaaaaaaaaaaaa1/public/a.png", nil), http.StatusNotFound,
		`{"code":"not_found","message":"Workspace not found","details":{"resource_id":"clunknownaaaaaaaaaaaaaaa1","resource_type":"Workspace"}}`, false)
	expectJSON(t, e.do(http.MethodGet, base+"/public/missing.png", nil), http.StatusNotFound,
		`{"code":"not_found","message":"file not found","details":{"resource_id":"missing.png","resource_type":"file"}}`, true)
	expectJSON(t, e.do(http.MethodGet, base+"/shared/README", nil), http.StatusBadRequest,
		`{"code":"bad_request","message":"Fields are missing or incorrectly formatted","details":{"fileName":"File name must have an extension","accessType":"Invalid option: expected one of \"public\"|\"private\""}}`, true)
	expectJSON(t, e.do(http.MethodGet, base+"/public/a.", nil), http.StatusBadRequest,
		`{"code":"bad_request","message":"Fields are missing or incorrectly formatted","details":{"fileName":"File name must have an extension"}}`, true)
	// Any extension may be downloaded; only uploads are restricted.
	e.put(ws.String()+"/public/page.html", "text/html", "<p>")
	if rec := e.do(http.MethodGet, base+"/public/page.html", nil); rec.Code != http.StatusOK || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("GET page.html %d", rec.Code)
	}

	// A path that steps out of the workspace's prefix is refused before the bucket is asked, decoded once by the router or twice by the guard.
	e.put("victim/private/secret.pdf", "application/pdf", "secret")
	for _, path := range []string{
		base + "/public/a/../../../victim/private/secret.pdf",
		base + "/public/%2e%2e/%2e%2e/victim/private/secret.pdf",
		base + "/public/%252e%252e/%252e%252e/victim/private/secret.pdf",
	} {
		rec := e.do(http.MethodGet, path, nil)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"message":"Invalid input"`) {
			t.Errorf("GET %s: %d %s", path, rec.Code, rec.Body.String())
		}
	}
}

func TestDownloadWithoutStorage(t *testing.T) {
	e := newHandlerEnv(t, func(c *config.Config) { c.S3 = config.S3Config{} })
	_, ws := e.organization(e.user())
	expectJSON(t, e.do(http.MethodGet, "/storage/"+ws.String()+"/public/a.png", nil), http.StatusInternalServerError,
		`{"code":"internal_server_error","message":"File storage is not configured correctly. Please check your file upload settings.","details":{"storage_error_code":"s3_client_error"}}`, true)
}

func TestPublicDomain(t *testing.T) {
	public, _ := url.Parse("https://surveys.example.com")
	e := newHandlerEnv(t, func(c *config.Config) { c.PublicURL = public })
	owner := e.user()
	_, ws := e.organization(owner)
	e.put(ws.String()+"/public/logo.png", "image/png", "png")
	e.put(ws.String()+"/private/a.pdf", "application/pdf", "pdf")

	if rec := e.do(http.MethodGet, "/storage/"+ws.String()+"/public/logo.png", as{"Host": "surveys.example.com"}); rec.Code != http.StatusOK {
		t.Fatalf("public file on the public domain: %d", rec.Code)
	}
	// Private files exist only on the admin domain; the public domain answers a bare 404 whatever the method.
	for _, method := range []string{http.MethodGet, http.MethodDelete, http.MethodOptions} {
		rec := e.do(method, "/storage/"+ws.String()+"/private/a.pdf", as{"Host": "surveys.example.com", testUserHeader: owner.String()})
		if rec.Code != http.StatusNotFound || rec.Body.Len() != 0 {
			t.Errorf("%s private file on the public domain: %d %q", method, rec.Code, rec.Body.String())
		}
	}
	if rec := e.do(http.MethodGet, "/storage/"+ws.String()+"/private/a.pdf", as{"Host": "app.example.com", testUserHeader: owner.String()}); rec.Code != http.StatusOK {
		t.Fatalf("private file on the admin domain: %d", rec.Code)
	}
}

// auditEvent is the part of an audit record the tests check.
type auditEvent struct {
	Actor          struct{ ID string }
	Target         struct{ ID, Type string }
	Action         string
	Status         string
	OrganizationID string `json:"organizationId"`
	APIURL         string `json:"apiUrl"`
	Changes        map[string]any
}

func (e *handlerEnv) audits() []auditEvent {
	e.t.Helper()
	var out []auditEvent
	for line := range strings.SplitSeq(e.logs.String(), "\n") {
		if !strings.Contains(line, `"msg":"audit"`) {
			continue
		}
		var ev auditEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			e.t.Fatal(err)
		}
		out = append(out, ev)
	}
	return out
}

func (e *handlerEnv) lastAudit() auditEvent {
	e.t.Helper()
	all := e.audits()
	if len(all) == 0 {
		e.t.Fatal("no audit event")
	}
	return all[len(all)-1]
}

func expectAudit(t *testing.T, got auditEvent, actor, target, status, org string, changes map[string]any) {
	t.Helper()
	if got.Action != "deleted" || got.Target.Type != "file" || got.Actor.ID != actor || got.Target.ID != target || got.Status != status || got.OrganizationID != org || !reflect.DeepEqual(got.Changes, changes) {
		t.Fatalf("audit %+v, want %s by %s on %s in %s with %v", got, status, actor, target, org, changes)
	}
}

func TestDelete(t *testing.T) {
	e := newHandlerEnv(t)
	owner, reader, manager := e.user(), e.user(), e.user()
	org, ws := e.organization(owner)
	e.member(org, reader, "member")
	e.grant(org, ws, reader, "read")
	e.member(org, manager, "member")
	e.grant(org, ws, manager, "manage")
	path := func(name string) string { return "/storage/" + ws.String() + "/public/" + name }
	e.put(ws.String()+"/public/logo.png", "image/png", "png")

	// Public files too need authentication to be deleted.
	expectJSON(t, e.do(http.MethodDelete, path("logo.png"), nil), http.StatusUnauthorized, notAuthenticatedBody, false)
	expectAudit(t, e.lastAudit(), "unknown", ws.String()+":public", "failure", org.String(), map[string]any{"workspaceId": ws.String(), "accessType": "public", "failureReason": "User not authenticated"})
	expectJSON(t, e.do(http.MethodDelete, path("logo.png"), asUser(reader)), http.StatusUnauthorized, unauthorizedBody, false)
	expectAudit(t, e.lastAudit(), "unknown", ws.String()+":public", "failure", org.String(), map[string]any{"workspaceId": ws.String(), "accessType": "public", "failureReason": "User not authorized to access workspace"})
	expectJSON(t, e.do(http.MethodDelete, path("logo.png"), asKey(e.apiKey(org, ws, "write"))), http.StatusUnauthorized, unauthorizedBody, false)
	if !e.exists(ws.String() + "/public/logo.png") {
		t.Fatal("a refused delete removed the file")
	}

	rec := e.do(http.MethodDelete, path("logo.png"), asUser(manager))
	expectJSON(t, rec, http.StatusOK, `{"data":"File deleted successfully"}`, false)
	if e.exists(ws.String() + "/public/logo.png") {
		t.Fatal("the file survived")
	}
	ev := e.lastAudit()
	expectAudit(t, ev, manager.String(), ws.String()+":public", "success", org.String(), map[string]any{"workspaceId": ws.String(), "accessType": "public"})
	if ev.APIURL != "http://app.example.com"+path("logo.png") {
		t.Errorf("apiUrl %q", ev.APIURL)
	}
	// S3 deletes are idempotent: a missing file is deleted successfully.
	expectJSON(t, e.do(http.MethodDelete, path("logo.png"), asUser(owner)), http.StatusOK, `{"data":"File deleted successfully"}`, false)

	expectJSON(t, e.do(http.MethodDelete, path("README"), asUser(owner)), http.StatusBadRequest,
		`{"code":"bad_request","message":"Fields are missing or incorrectly formatted","details":{"fileName":"File name must have an extension"}}`, true)
	// forma2 had not read the access type yet, so the target id says so.
	expectAudit(t, e.lastAudit(), "unknown", ws.String()+":undefined", "failure", org.String(), map[string]any{"workspaceId": ws.String(), "failureReason": "Parameter validation failed"})

	expectJSON(t, e.do(http.MethodDelete, "/storage/clunknownaaaaaaaaaaaaaaa1/public/a.png", asUser(owner)), http.StatusNotFound,
		`{"code":"not_found","message":"Workspace not found","details":{"resource_id":"clunknownaaaaaaaaaaaaaaa1","resource_type":"Workspace"}}`, false)
	expectAudit(t, e.lastAudit(), "unknown", "clunknownaaaaaaaaaaaaaaa1:undefined", "failure", "unknown", map[string]any{"workspaceId": "clunknownaaaaaaaaaaaaaaa1", "failureReason": "Workspace not found"})

	e.put("victim/private/secret.pdf", "application/pdf", "secret")
	rec = e.do(http.MethodDelete, "/storage/"+ws.String()+"/private/%2e%2e/%2e%2e/victim/private/secret.pdf", asUser(owner))
	expectJSON(t, rec, http.StatusBadRequest, `{"code":"bad_request","message":"Invalid input","details":{"fileName":"../../victim/private/secret.pdf"}}`, true)
	expectAudit(t, e.lastAudit(), owner.String(), ws.String()+":private", "failure", org.String(), map[string]any{"workspaceId": ws.String(), "accessType": "private", "failureReason": "invalid_input"})
	if !e.exists("victim/private/secret.pdf") {
		t.Fatal("a traversal deleted another tenant's file")
	}
}

func TestDeleteRateLimit(t *testing.T) {
	e := newHandlerEnv(t)
	owner := e.user()
	org, ws := e.organization(owner)
	path := "/storage/" + ws.String() + "/public/a.png"
	for i := range 5 {
		if rec := e.do(http.MethodDelete, path, asUser(owner)); rec.Code != http.StatusOK {
			t.Fatalf("delete %d: %d", i+1, rec.Code)
		}
	}
	before := len(e.audits())
	expectJSON(t, e.do(http.MethodDelete, path, asUser(owner)), http.StatusTooManyRequests,
		`{"code":"too_many_requests","message":"Maximum number of requests reached. Please try again later.","details":{}}`, false)
	if len(e.audits()) != before {
		t.Error("a rate-limited delete was audited")
	}
	// API keys were left to forma2's gateway.
	key := e.apiKey(org, ws, "manage")
	for i := range 7 {
		if rec := e.do(http.MethodDelete, path, asKey(key)); rec.Code != http.StatusOK {
			t.Fatalf("API key delete %d: %d", i+1, rec.Code)
		}
	}
	if ev := e.lastAudit(); ev.Actor.ID != "unknown" || ev.Status != "success" {
		t.Errorf("API key delete audited as %+v", ev)
	}
}
