package clientapi

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
	"testing/fstest"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/contact"
	"github.com/yldm-tech/forma/apps/server/internal/jsonschema"
	"github.com/yldm-tech/forma/apps/server/internal/platform/audit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpserver"
	"github.com/yldm-tech/forma/apps/server/internal/platform/jobs"
	"github.com/yldm-tech/forma/apps/server/internal/platform/pgtest"
	"github.com/yldm-tech/forma/apps/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/secret"
	"github.com/yldm-tech/forma/apps/server/internal/platform/valkey/valkeytest"
	"github.com/yldm-tech/forma/apps/server/internal/response"
	"github.com/yldm-tech/forma/apps/server/internal/storage"
	"github.com/yldm-tech/forma/apps/server/internal/webui"
	"github.com/yldm-tech/forma/apps/server/internal/workspace"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	os.Exit(m.Run())
}

const (
	webAppURL = "http://localhost:3999"
	// browserUA is the desktop Chrome the recorder identified as.
	browserUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36"
	// testPinTokenSecret signs PIN tokens; testTokenSecret signs email verification tokens.
	testPinTokenSecret = "better-auth-secret-for-client-api-tests"
	testTokenSecret    = "next-auth-secret-for-client-api-tests"
)

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

// clock is the time the rate limiter and the handler read; tests move it.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// nextWindow moves the clock to the start of the next rate-limit minute.
func (c *clock) nextWindow() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Truncate(time.Minute).Add(time.Minute)
}

// env is the client API on a fresh database and Valkey, mounted as the server mounts it, with rate limits on and a clock the test moves.
type env struct {
	t       *testing.T
	cfg     config.Config
	db      *gorm.DB
	cache   *redis.Client
	clock   *clock
	h       *Handler
	engine  *gin.Engine
	logs    *syncBuffer
	cipher  *secret.Cipher
	google  *httptest.Server
	captcha struct {
		mu     sync.Mutex
		answer string
		status int
		forms  []url.Values
	}
}

func newEnv(t *testing.T, configure ...func(*config.Config)) *env {
	t.Helper()
	t.Parallel()
	u, _ := url.Parse(webAppURL)
	s3, _ := url.Parse("http://localhost:9000")
	cfg := config.Config{
		WebAppURL: u, TrustedProxyHopCount: 1,
		EncryptionKey: bytes.Repeat([]byte{7}, 32), EncryptionKeyText: config.Secret(strings.Repeat("07", 32)), EncryptionKeyVersion: 1,
		LinkSurveyPinTokenSecret: testPinTokenSecret, LinkSurveyTokenSecret: testTokenSecret,
		S3: config.S3Config{AccessKey: "contractfixtureaccess", SecretKey: "contractfixturesecret", Region: "us-east-1", EndpointURL: s3, ForcePathStyle: true, BucketName: "forma-uploads"},
	}
	for _, f := range configure {
		f(&cfg)
	}
	e := &env{t: t, cfg: cfg, db: pgtest.GORM(t), cache: valkeytest.Client(t), logs: &syncBuffer{}, clock: &clock{now: time.Now().Truncate(time.Minute)}}
	log := slog.New(slog.NewJSONHandler(e.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	var err error
	if e.cipher, err = secret.NewCipher(cfg); err != nil {
		t.Fatal(err)
	}
	enqueuer, err := jobs.NewEnqueuer(log)
	if err != nil {
		t.Fatal(err)
	}
	limiter := ratelimit.New(cfg, e.cache, log).WithClock(e.clock.Now)
	responses := response.NewService(cfg, e.db, enqueuer, e.cipher, nil, limiter, audit.New(cfg, log), log)
	schemas, err := jsonschema.New()
	if err != nil {
		t.Fatal(err)
	}
	contacts := contact.NewService(contact.NewRepository(e.db), nil, limiter, audit.New(cfg, log), schemas, log)
	bucket, err := storage.NewBucket(cfg, log)
	if err != nil {
		t.Fatal(err)
	}

	e.google = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		e.captcha.mu.Lock()
		defer e.captcha.mu.Unlock()
		e.captcha.forms = append(e.captcha.forms, r.PostForm)
		if e.captcha.status != 0 {
			w.WriteHeader(e.captcha.status)
		}
		_, _ = io.WriteString(w, e.captcha.answer)
	}))
	t.Cleanup(e.google.Close)
	recaptcha := NewRecaptcha(cfg, log)
	recaptcha.endpoint = e.google.URL

	e.h = NewHandler(cfg, e.db, e.cache, limiter, workspace.NewRepository(e.db, log), responses, contacts, storage.NewService(bucket), recaptcha, e.cipher, log)
	e.h.now = e.clock.Now
	web, err := webui.NewFromFS(fstest.MapFS{"index.html": {Data: []byte("<!doctype html>")}, "js/surveys.umd.cjs": {Data: []byte("/* surveys */")}})
	if err != nil {
		t.Fatal(err)
	}
	e.engine = httpserver.NewEngine(cfg, log)
	e.engine.Use(httpapi.NewClientIPResolver(cfg, log).Middleware())
	e.h.Register(e.engine)
	web.Register(e.engine)
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

// workspace creates an organization with billing and a workspace seeded as the workspace module seeds one: the default attribute keys and an en-US language.
func (e *env) workspace(columns ...any) uuid.UUID {
	e.t.Helper()
	org := e.insert(`INSERT INTO organizations (name) VALUES (?) RETURNING id`, "Org "+uuid.NewString()[:8])
	e.exec(`INSERT INTO organization_billing (organization_id, limits) VALUES (?, '{}'::jsonb)`, org)
	ws := e.insert(`INSERT INTO workspaces (organization_id, name) VALUES (?, ?) RETURNING id`, org, "Workspace "+uuid.NewString()[:8])
	for i := 0; i+1 < len(columns); i += 2 {
		cast := ""
		if columns[i] == "styling" {
			cast = "::jsonb"
		}
		e.exec(`UPDATE workspaces SET `+columns[i].(string)+` = ?`+cast+` WHERE id = ?`, columns[i+1], ws)
	}
	for _, k := range [][2]string{{"userId", "User Id"}, {"email", "Email"}, {"firstName", "First Name"}, {"lastName", "Last Name"}, {"language", "Language"}} {
		e.exec(`INSERT INTO contact_attribute_keys (workspace_id, key, name, type, is_unique) VALUES (?, ?, ?, 'default', ?)`, ws, k[0], k[1], k[0] == "userId" || k[0] == "email")
	}
	e.language(ws, "en-US", nil)
	return ws
}

func (e *env) language(ws uuid.UUID, code string, alias *string) uuid.UUID {
	e.t.Helper()
	var id uuid.UUID
	if err := e.db.Raw(`SELECT id FROM languages WHERE workspace_id = ? AND code = ?`, ws, code).Row().Scan(&id); err == nil {
		return id
	}
	return e.insert(`INSERT INTO languages (workspace_id, code, alias) VALUES (?, ?, ?) RETURNING id`, ws, code, alias)
}

// survey creates a survey of ws with the given columns as name/value pairs over a link survey in progress with one optional open text question q1.
func (e *env) survey(ws uuid.UUID, columns ...any) uuid.UUID {
	e.t.Helper()
	id := e.insert(`INSERT INTO surveys (workspace_id, name, type, status, blocks) VALUES (?, 'Survey', 'link', 'inProgress', ?::jsonb) RETURNING id`, ws,
		`[{"id":"b1","name":"Block 1","elements":[{"id":"q1","type":"openText","headline":{"default":"Q1"},"required":false,"inputType":"text"}]}]`)
	for i := 0; i+1 < len(columns); i += 2 {
		col := columns[i].(string)
		cast := ""
		switch col {
		case "welcome_card", "blocks", "endings", "hidden_fields", "variables", "styling", "single_use", "recaptcha", "workspace_overwrites":
			cast = "::jsonb"
		}
		e.exec(`UPDATE surveys SET `+col+` = ?`+cast+` WHERE id = ?`, columns[i+1], id)
	}
	return id
}

// surveyLanguage links a workspace language to a survey.
func (e *env) surveyLanguage(sv, language uuid.UUID, isDefault bool) {
	e.exec(`INSERT INTO survey_languages (survey_id, language_id, is_default, enabled) VALUES (?, ?, ?, true)`, sv, language, isDefault)
}

// segment creates a segment of ws with filters and attaches it to the surveys given.
func (e *env) segment(ws uuid.UUID, title, filters string, surveys ...uuid.UUID) uuid.UUID {
	e.t.Helper()
	id := e.insert(`INSERT INTO segments (workspace_id, title, is_private, filters) VALUES (?, ?, true, ?::jsonb) RETURNING id`, ws, title, filters)
	for _, s := range surveys {
		e.exec(`UPDATE surveys SET segment_id = ? WHERE id = ?`, id, s)
	}
	return id
}

// ingestedField links an ingested Embedded Data field to a survey.
func (e *env) ingestedField(ws, sv uuid.UUID, key string) {
	e.t.Helper()
	def := e.insert(`INSERT INTO embedded_data (workspace_id, survey_id, name, source, data_type) VALUES (?, ?, ?, 'ingested', 'string') RETURNING id`, ws, sv, key)
	position := e.count(`SELECT count(*) FROM survey_embedded_data WHERE survey_id = ?`, sv)
	e.exec(`INSERT INTO survey_embedded_data (workspace_id, survey_id, embedded_data_id, storage_key, position) VALUES (?, ?, ?, ?, ?)`, ws, sv, def, key, position)
}

// result is an answered request.
type result struct {
	Status int
	Header http.Header
	Body   []byte
}

func (r result) json(t *testing.T) map[string]any {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal(r.Body, &v); err != nil {
		t.Fatalf("body %s: %v", r.Body, err)
	}
	return v
}

// data is the body's "data" member.
func (r result) data(t *testing.T) map[string]any {
	t.Helper()
	d, ok := r.json(t)["data"].(map[string]any)
	if !ok {
		t.Fatalf("no data object in %d %s", r.Status, r.Body)
	}
	return d
}

// errorBody asserts a forma2 error answer.
func (r result) expectError(t *testing.T, status int, code, message string) map[string]any {
	t.Helper()
	if r.Status != status {
		t.Fatalf("status %d, want %d: %s", r.Status, status, r.Body)
	}
	b := r.json(t)
	if b["code"] != code || b["message"] != message {
		t.Fatalf("error %v %q, want %s %q", b["code"], b["message"], code, message)
	}
	d, _ := b["details"].(map[string]any)
	return d
}

// do sends a request; body is JSON-encoded unless it is a string, which is sent as it is.
func (e *env) do(method, path string, body any, headers ...string) result {
	e.t.Helper()
	var r io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		r = strings.NewReader(b)
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			e.t.Fatal(err)
		}
		r = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, r)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("User-Agent", browserUA)
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	rec := httptest.NewRecorder()
	e.engine.ServeHTTP(rec, req)
	return result{Status: rec.Code, Header: rec.Header(), Body: rec.Body.Bytes()}
}

// recaptchaAnswers makes the stand-in for Google answer with body and status.
func (e *env) recaptchaAnswers(status int, body string) {
	e.captcha.mu.Lock()
	defer e.captcha.mu.Unlock()
	e.captcha.status, e.captcha.answer = status, body
}

func ptr[T any](v T) *T { return &v }
