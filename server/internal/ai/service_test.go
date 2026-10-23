package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/server/internal/authz"
	"github.com/yldm-tech/forma/server/internal/authz/authztest"
	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/platform/audit"
	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/server/internal/platform/httpserver"
	"github.com/yldm-tech/forma/server/internal/platform/pgtest"
	"github.com/yldm-tech/forma/server/internal/platform/ratelimit"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	os.Exit(m.Run())
}

const webApp = "http://app.example.com"

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

// lines are the JSON log lines with the given message.
func (b *syncBuffer) lines(t *testing.T, msg string) []map[string]any {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []map[string]any
	for _, line := range strings.Split(b.buf.String(), "\n") {
		var v map[string]any
		if json.Unmarshal([]byte(line), &v) == nil && v["msg"] == msg {
			out = append(out, v)
		}
	}
	return out
}

// fakeProvider is an OpenAI-compatible chat completions endpoint whose answer each test decides.
type fakeProvider struct {
	*httptest.Server
	mu     sync.Mutex
	bodies []map[string]any
	answer func(body map[string]any) (status int, content string)
}

func newFakeProvider(t *testing.T) *fakeProvider {
	t.Helper()
	f := &fakeProvider{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		f.mu.Lock()
		f.bodies = append(f.bodies, body)
		answer := f.answer
		f.mu.Unlock()
		status, content := answer(body)
		if status != http.StatusOK {
			w.Header().Set("Retry-After", "17")
			writeJSON(w, status, map[string]any{"error": map[string]any{"message": "rate limited"}})
			return
		}
		writeJSON(w, http.StatusOK, chatCompletion(content, "stop"))
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeProvider) requests() []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]map[string]any(nil), f.bodies...)
}

type serviceEnv struct {
	t        *testing.T
	db       *gorm.DB
	authz    *authztest.Env
	svc      *Service
	examples *ExampleResponses
	engine   *gin.Engine
	logs     *syncBuffer
	provider *fakeProvider
}

// newServiceEnv is the AI service on a fresh database with real authorization, its routes mounted as the server mounts them, and an AI provider the test answers for. configure edits the AI configuration; nil leaves the provider configured.
func newServiceEnv(t *testing.T, configure func(*config.AIConfig)) *serviceEnv {
	t.Helper()
	t.Parallel()
	provider := newFakeProvider(t)
	u, _ := url.Parse(webApp)
	cfg := config.Config{WebAppURL: u, RateLimitingDisabled: true, AuditLogEnabled: true, TrustedProxyHopCount: 1, AI: config.AIConfig{Provider: "openai-compatible", Model: "qwen", ModelTranslation: "qwen-small", OpenAICompatibleBaseURL: provider.URL, OpenAICompatibleSupportsStructuredOutputs: "1"}}
	if configure != nil {
		configure(&cfg.AI)
	}
	db := pgtest.GORM(t)
	e := &serviceEnv{t: t, db: db, authz: authztest.New(t, cfg, db), logs: &syncBuffer{}, provider: provider}
	log := slog.New(slog.NewJSONHandler(e.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	zero := 0
	client := NewClient(cfg, log)
	client.endpoints = endpoints{retries: &zero}
	e.svc = NewService(client, NewRepository(db), e.authz.Authorizer, log)
	e.examples = NewExampleResponses(e.svc, db, ratelimit.New(cfg, nil, log), audit.New(cfg, log), log)
	e.engine = httpserver.NewEngine(log)
	group := e.engine.Group("/api/app", httpapi.ErrorHandler(log), httpapi.CSRF(cfg), authz.DecisionMemo())
	NewHandler(e.svc, e.examples, headerSessions{}).Register(group)
	return e
}

func (e *serviceEnv) insert(query string, args ...any) uuid.UUID {
	e.t.Helper()
	var id uuid.UUID
	if err := e.db.Raw(query, args...).Row().Scan(&id); err != nil {
		e.t.Fatalf("insert %q: %v", query, err)
	}
	return id
}

func (e *serviceEnv) exec(query string, args ...any) {
	e.t.Helper()
	if err := e.db.Exec(query, args...).Error; err != nil {
		e.t.Fatalf("exec %q: %v", query, err)
	}
}

func (e *serviceEnv) user() uuid.UUID {
	return e.insert(`INSERT INTO users (name, email) VALUES ('Ada', ?) RETURNING id`, uuid.NewString()+"@example.com")
}

// organization creates an organization owned by owner, with AI smart tools as given and one workspace.
func (e *serviceEnv) organization(owner uuid.UUID, smartTools bool) (org, ws uuid.UUID) {
	e.t.Helper()
	org = e.insert(`INSERT INTO organizations (name, is_ai_smart_tools_enabled) VALUES ('Acme', ?) RETURNING id`, smartTools)
	e.member(org, owner, "owner")
	ws = e.insert(`INSERT INTO workspaces (organization_id, name) VALUES (?, 'Main') RETURNING id`, org)
	return org, ws
}

func (e *serviceEnv) member(org, user uuid.UUID, role string) {
	e.exec(`INSERT INTO memberships (user_id, organization_id, role, accepted) VALUES (?, ?, ?, true)`, user, org, role)
}

func (e *serviceEnv) grant(org, ws, user uuid.UUID, permission string) {
	team := e.insert(`INSERT INTO teams (organization_id, name) VALUES (?, ?) RETURNING id`, org, "Team "+uuid.NewString())
	e.exec(`INSERT INTO team_members (team_id, user_id, role) VALUES (?, ?, 'contributor')`, team, user)
	e.exec(`INSERT INTO workspace_teams (workspace_id, team_id, permission) VALUES (?, ?, ?)`, ws, team, permission)
}

func (e *serviceEnv) do(user uuid.UUID, method, path string, body any) *httptest.ResponseRecorder {
	e.t.Helper()
	e.authz.Deliver(e.t)
	var r io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		r = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, r)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", webApp)
	req.Header.Set(httpapi.CSRFHeader, "XMLHttpRequest")
	req.Header.Set(testUserHeader, user.String())
	rec := httptest.NewRecorder()
	e.engine.ServeHTTP(rec, req)
	return rec
}

func (e *serviceEnv) ok(rec *httptest.ResponseRecorder, out any) {
	e.t.Helper()
	if rec.Code != http.StatusOK {
		e.t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	// Decoded into a zero value, so a key the answer omits does not keep an earlier answer's value.
	v := reflect.ValueOf(out).Elem()
	v.Set(reflect.Zero(v.Type()))
	if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
		e.t.Fatal(err)
	}
}

func (e *serviceEnv) fails(rec *httptest.ResponseRecorder, status int, code string) httpapi.Envelope {
	e.t.Helper()
	var env httpapi.Envelope
	_ = json.Unmarshal(rec.Body.Bytes(), &env)
	if rec.Code != status || env.Code != code {
		e.t.Fatalf("want %d %s, got %d: %s", status, code, rec.Code, rec.Body)
	}
	return env
}

// expectCode asserts an expected failure with the status and code, and returns a copy of it.
func expectCode(t *testing.T, err error, status int, code string) apperr.Error {
	t.Helper()
	e, ok := apperr.As(err)
	if !ok || e.Status != status || e.Code != code {
		t.Fatalf("want %d %s, got %v", status, code, err)
	}
	return *e
}

// forma2's service.test.ts: the organization's AI configuration and the order assertOrganizationAIConfigured fails in.
func TestServiceAssert(t *testing.T) {
	e := newServiceEnv(t, nil)
	owner := e.user()
	on, _ := e.organization(owner, true)
	off, _ := e.organization(owner, false)
	ctx := t.Context()

	cfg, err := e.svc.Assert(ctx, on)
	if err != nil || !reflect.DeepEqual(*cfg, OrganizationConfig{OrganizationID: on, IsAISmartToolsEnabled: true, IsAISmartToolsEntitled: true, IsInstanceConfigured: true}) {
		t.Fatalf("%+v %v", cfg, err)
	}
	e2 := expectCode(t, func() error { _, err := e.svc.Assert(ctx, off); return err }(), http.StatusForbidden, CodeSmartToolsDisabled)
	if e2.Message != "AI smart tools are disabled for this organization." {
		t.Fatalf("message %q", e2.Message)
	}
	expectCode(t, func() error { _, err := e.svc.Assert(ctx, uuid.New()); return err }(), http.StatusNotFound, apperr.CodeNotFound)
	_, err = e.svc.GenerateObject(ctx, off, FeatureSurveyGeneration, testObjectRequest())
	expectCode(t, err, http.StatusForbidden, CodeSmartToolsDisabled)
	if len(e.provider.requests()) != 0 {
		t.Fatal("a refused organization reached the provider")
	}
}

func TestServiceAssertOnUnconfiguredInstance(t *testing.T) {
	e := newServiceEnv(t, func(c *config.AIConfig) { c.Model = "" })
	org, _ := e.organization(e.user(), true)
	_, err := e.svc.Assert(t.Context(), org)
	expectCode(t, err, http.StatusForbidden, CodeInstanceNotConfigured)
	off, _ := e.organization(e.user(), false)
	cfg, err := e.svc.OrganizationConfig(t.Context(), off)
	if err != nil || cfg.UnavailableReason() != ReasonNotEnabled {
		t.Fatalf("%+v %v", cfg, err)
	}
	cfg.IsAISmartToolsEnabled = true
	if cfg.UnavailableReason() != ReasonInstanceNotConfigured {
		t.Fatal(cfg.UnavailableReason())
	}
	cfg.IsAISmartToolsEntitled = false
	if cfg.UnavailableReason() != ReasonNotInPlan {
		t.Fatal(cfg.UnavailableReason())
	}
}

// Usage is recorded once per completed generation, under the feature and the model it actually ran on; a translation runs on AI_MODEL_TRANSLATION.
func TestServiceRecordsUsageAndRoutesFeatures(t *testing.T) {
	e := newServiceEnv(t, nil)
	reader := sdkmetric.NewManualReader()
	e.svc.usage = newUsageMeters(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
	org, _ := e.organization(e.user(), true)
	e.provider.answer = func(map[string]any) (int, string) { return http.StatusOK, `{"name": "Onboarding", "range": null}` }
	if _, err := e.svc.GenerateObject(t.Context(), org, FeatureSurveyGeneration, testObjectRequest()); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.GenerateObject(t.Context(), org, FeatureTranslation, testObjectRequest()); err != nil {
		t.Fatal(err)
	}
	e.provider.answer = func(map[string]any) (int, string) { return http.StatusOK, "Hello" }
	if _, err := e.svc.GenerateText(t.Context(), org, "", Request{Prompt: "Hi"}); err != nil {
		t.Fatal(err)
	}
	reqs := e.provider.requests()
	if reqs[0]["model"] != "qwen" || reqs[1]["model"] != "qwen-small" || reqs[2]["model"] != "qwen" {
		t.Fatalf("models %v %v %v", reqs[0]["model"], reqs[1]["model"], reqs[2]["model"])
	}
	usage := e.logs.lines(t, "AI generation usage")
	if len(usage) != 3 {
		t.Fatalf("usage lines %v", usage)
	}
	want := []map[string]any{
		{"organizationId": org.String(), "feature": "ai_survey_generation", "model": "qwen", "inputTokens": float64(12), "outputTokens": float64(7), "reasoningTokens": float64(3)},
		{"organizationId": org.String(), "feature": "ai_translation", "model": "qwen-small"},
		{"organizationId": org.String(), "feature": "unknown", "model": "qwen"},
	}
	for i, w := range want {
		for k, v := range w {
			if usage[i][k] != v {
				t.Errorf("usage %d %s = %v, want %v", i, k, usage[i][k], v)
			}
		}
	}
	for _, line := range usage {
		if _, ok := line["prompt"]; ok {
			t.Fatal("a usage line carries the prompt")
		}
	}

	// The same facts as forma2's counters, by feature and model and never by organization.
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(t.Context(), &rm); err != nil {
		t.Fatal(err)
	}
	got := map[string]int64{}
	for _, sm := range rm.ScopeMetrics {
		if sm.Scope.Name != "forma.ai" {
			t.Errorf("scope %q, want forma.ai", sm.Scope.Name)
		}
		for _, m := range sm.Metrics {
			for _, dp := range m.Data.(metricdata.Sum[int64]).DataPoints {
				if _, ok := dp.Attributes.Value("organizationId"); ok {
					t.Errorf("%s carries the organization", m.Name)
				}
				feature, _ := dp.Attributes.Value("feature")
				model, _ := dp.Attributes.Value("model")
				kind, _ := dp.Attributes.Value("kind")
				got[m.Name+"|"+feature.AsString()+"|"+model.AsString()+"|"+kind.AsString()] += dp.Value
			}
		}
	}
	for key, n := range map[string]int64{
		"forma_ai_generations_total|ai_survey_generation|qwen|":      1,
		"forma_ai_generations_total|ai_translation|qwen-small|":      1,
		"forma_ai_generations_total|unknown|qwen|":                   1,
		"forma_ai_tokens_total|ai_survey_generation|qwen|input":      12,
		"forma_ai_tokens_total|ai_survey_generation|qwen|output":     7,
		"forma_ai_reasoning_tokens_total|ai_survey_generation|qwen|": 3,
	} {
		if got[key] != n {
			t.Errorf("%s = %d, want %d (all: %v)", key, got[key], n, got)
		}
	}
}

// A provider 429 becomes ai_quota_exceeded with its retry-after, logged with the provider status; a failed generation records no usage.
func TestServiceMapsProviderQuota(t *testing.T) {
	e := newServiceEnv(t, nil)
	org, _ := e.organization(e.user(), true)
	e.provider.answer = func(map[string]any) (int, string) { return http.StatusTooManyRequests, "" }
	_, err := e.svc.GenerateObject(t.Context(), org, FeatureSurveyGeneration, testObjectRequest())
	quota := expectCode(t, err, http.StatusTooManyRequests, CodeQuotaExceeded)
	if quota.RetryAfter != 17 || quota.Message != QuotaExceededMessage {
		t.Fatalf("%+v", quota)
	}
	failures := e.logs.lines(t, "Failed to generate organization AI object")
	if len(failures) != 1 || failures[0]["statusCode"] != float64(429) || failures[0]["isQuotaExhausted"] != true || failures[0]["isRetryable"] != true || failures[0]["organizationId"] != org.String() {
		t.Fatalf("failure log %v", failures)
	}
	if len(e.logs.lines(t, "AI generation usage")) != 0 {
		t.Fatal("a failed generation recorded usage")
	}
	e.provider.answer = func(map[string]any) (int, string) { return http.StatusBadRequest, "" }
	_, err = e.svc.GenerateText(t.Context(), org, "", Request{Prompt: "Hi"})
	if _, ok := apperr.As(err); ok || ClassifyProviderError(err, e.svc.now()).StatusCode != http.StatusBadRequest {
		t.Fatalf("a non-quota failure was not passed through: %v", err)
	}
}

// A generation its caller cancelled is not an incident: no error line, no usage.
func TestServiceAbortIsNotLogged(t *testing.T) {
	e := newServiceEnv(t, nil)
	org, _ := e.organization(e.user(), true)
	ctx, cancel := context.WithCancel(t.Context())
	e.provider.answer = func(map[string]any) (int, string) {
		cancel()
		return http.StatusOK, `{"name": "x", "range": null}`
	}
	_, err := e.svc.StreamObject(ctx, org, FeatureSurveyGeneration, testObjectRequest(), func(any) {})
	if !IsAbort(ctx, err) {
		t.Fatalf("%v", err)
	}
	if lines := e.logs.lines(t, "Failed to stream organization AI object"); len(lines) != 0 {
		t.Fatalf("an abort was logged: %v", lines)
	}
}

// translator answers translation requests: each item's text with a prefix, and an empty string for the ids blank names.
func translator(prefix string, blank ...string) func(map[string]any) (int, string) {
	return func(body map[string]any) (int, string) {
		var items []translationItem
		_ = json.Unmarshal([]byte(jsonPath(body, "messages.1.content").(string)), &items)
		out := map[string]string{}
		for _, item := range items {
			out[item.ID] = prefix + item.Text
			if slicesContains(blank, item.ID) {
				out[item.ID] = ""
			}
		}
		b, _ := json.Marshal(out)
		return http.StatusOK, string(b)
	}
}

func slicesContains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

func fields(n int) []TranslationField {
	out := make([]TranslationField, n)
	for i := range out {
		out[i] = TranslationField{Path: fmt.Sprintf("blocks.%d.headline", i), DefaultText: fmt.Sprintf("Question %d", i)}
	}
	return out
}

// forma2's translate-fields.test.ts.
func TestTranslateFields(t *testing.T) {
	e := newServiceEnv(t, nil)
	org, _ := e.organization(e.user(), true)
	ctx := t.Context()

	e.provider.answer = translator("DE ")
	in := []TranslationField{
		{Path: "welcomeCard.headline", DefaultText: "Welcome", IsRichText: false},
		{Path: "blocks.0.elements.0.subheader", DefaultText: "", IsRichText: true},
		{Path: "blocks.0.elements.0.headline", DefaultText: "<p>Hi {{name}}</p>", IsRichText: true},
	}
	out, err := e.svc.TranslateFields(ctx, org, in, "English", "German")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(out, &TranslationResult{Translations: map[string]string{"welcomeCard.headline": "DE Welcome", "blocks.0.elements.0.subheader": "", "blocks.0.elements.0.headline": "DE <p>Hi {{name}}</p>"}, FailedPaths: []string{}}) {
		t.Fatalf("%+v", out)
	}
	body := e.provider.requests()[0]
	// Paths never reach the model: items carry index ids, and the schema names exactly those.
	expectBody(t, body, map[string]any{
		"model":              "qwen-small",
		"temperature":        float64(0),
		"max_tokens":         float64(1024),
		"messages.1.content": `[{"id":"t0","text":"Welcome","richText":false},{"id":"t1","text":"<p>Hi {{name}}</p>","richText":true}]`,
		"response_format.json_schema.schema.required":             []any{"t0", "t1"},
		"response_format.json_schema.schema.additionalProperties": false,
		"response_format.json_schema.name":                        "response",
	})
	if system := jsonPath(body, "messages.0.content").(string); !strings.HasPrefix(system, "You are a professional translator for survey content. Translate each item from English to German.\nRules:\n") || !strings.Contains(system, "{{variable}}") {
		t.Fatalf("system prompt %q", system)
	}
	if strings.Contains(fmt.Sprint(body), "welcomeCard") {
		t.Fatal("a path reached the model")
	}

	// Nothing to translate makes no call.
	before := len(e.provider.requests())
	if out, err := e.svc.TranslateFields(ctx, org, []TranslationField{{Path: "a", DefaultText: ""}}, "English", "German"); err != nil || out.Translations["a"] != "" {
		t.Fatalf("%+v %v", out, err)
	}
	if out, err := e.svc.TranslateFields(ctx, org, nil, "English", "German"); err != nil || len(out.Translations) != 0 {
		t.Fatalf("%+v %v", out, err)
	}
	if len(e.provider.requests()) != before {
		t.Fatal("an empty batch called the model")
	}

	// 60 fields are two sequential chunks of 51 and 9, each with its own budget; an item that comes back empty is a failed path.
	before = len(e.provider.requests())
	e.provider.answer = translator("FR ", "t3")
	out, err = e.svc.TranslateFields(ctx, org, fields(60), "English", "French")
	if err != nil {
		t.Fatal(err)
	}
	reqs := e.provider.requests()[before:]
	if len(reqs) != 2 || reqs[0]["max_tokens"] != float64(8160) || reqs[1]["max_tokens"] != float64(1440) {
		t.Fatalf("chunks %d %v %v", len(reqs), reqs[0]["max_tokens"], reqs[1]["max_tokens"])
	}
	if len(out.Translations) != 58 || !reflect.DeepEqual(out.FailedPaths, []string{"blocks.3.headline", "blocks.54.headline"}) {
		t.Fatalf("%d translations, failed %v", len(out.Translations), out.FailedPaths)
	}

	// A chunk that fails costs only its own paths.
	calls := 0
	e.provider.answer = func(body map[string]any) (int, string) {
		calls++
		if calls == 1 {
			return http.StatusTooManyRequests, ""
		}
		return translator("IT ")(body)
	}
	out, err = e.svc.TranslateFields(ctx, org, fields(60), "English", "Italian")
	if err != nil || len(out.FailedPaths) != 51 || len(out.Translations) != 9 {
		t.Fatalf("%d translations, %d failed, %v", len(out.Translations), len(out.FailedPaths), err)
	}

	// An answer missing an id does not match the chunk's schema, so the chunk fails as a whole. Nothing translated at all is a failure: the first chunk's error, else forma2's incomplete result.
	e.provider.answer = func(map[string]any) (int, string) { return http.StatusOK, `{"t0": "Ciao"}` }
	out, err = e.svc.TranslateFields(ctx, org, fields(3), "English", "Italian")
	var noObject *NoObjectError
	if !errors.As(err, &noObject) {
		t.Fatalf("%+v %v", out, err)
	}
	e.provider.answer = func(map[string]any) (int, string) { return http.StatusTooManyRequests, "" }
	_, err = e.svc.TranslateFields(ctx, org, fields(2), "English", "Italian")
	expectCode(t, err, http.StatusTooManyRequests, CodeQuotaExceeded)
	e.provider.answer = translator("", "t0", "t1")
	if _, err = e.svc.TranslateFields(ctx, org, fields(2), "English", "Italian"); !errors.Is(err, errIncompleteTranslation) {
		t.Fatalf("%v", err)
	}
}

// The SPA's routes: availability for the settings toggle, the survey list and the editor, and the editor's translation, each behind its permission.
func TestAvailabilityAndTranslationRoutes(t *testing.T) {
	e := newServiceEnv(t, nil)
	owner, reader, outsider := e.user(), e.user(), e.user()
	org, ws := e.organization(owner, false)
	e.member(org, reader, "member")
	e.grant(org, ws, reader, "read")
	survey := e.insert(`INSERT INTO surveys (workspace_id, name) VALUES (?, 'Onboarding') RETURNING id`, ws)

	var orgAI OrganizationAvailability
	e.ok(e.do(reader, http.MethodGet, "/api/app/organizations/"+org.String()+"/ai", nil), &orgAI)
	if !reflect.DeepEqual(orgAI, OrganizationAvailability{OrganizationConfig: OrganizationConfig{OrganizationID: org, IsAISmartToolsEntitled: true, IsInstanceConfigured: true}, Availability: Availability{Reason: ReasonNotEnabled}}) {
		t.Fatalf("%+v", orgAI)
	}
	e.fails(e.do(outsider, http.MethodGet, "/api/app/organizations/"+org.String()+"/ai", nil), http.StatusForbidden, apperr.CodeForbidden)

	var a Availability
	e.ok(e.do(owner, http.MethodGet, "/api/app/workspaces/"+ws.String()+"/ai", nil), &a)
	if a != (Availability{Reason: ReasonNotEnabled}) {
		t.Fatalf("%+v", a)
	}
	e.ok(e.do(reader, http.MethodGet, "/api/app/workspaces/"+ws.String()+"/ai", nil), &a)
	if a != (Availability{Reason: ReasonReadOnly}) {
		t.Fatalf("%+v", a)
	}
	e.fails(e.do(outsider, http.MethodGet, "/api/app/workspaces/"+ws.String()+"/ai", nil), http.StatusForbidden, apperr.CodeForbidden)
	e.fails(e.do(owner, http.MethodGet, "/api/app/workspaces/"+uuid.NewString()+"/ai", nil), http.StatusNotFound, apperr.CodeNotFound)

	availability := "/api/app/surveys/" + survey.String() + "/ai/translation-availability"
	e.ok(e.do(reader, http.MethodGet, availability, nil), &a)
	if a != (Availability{Reason: ReasonNotEnabled}) {
		t.Fatalf("%+v", a)
	}
	e.fails(e.do(outsider, http.MethodGet, availability, nil), http.StatusForbidden, apperr.CodeForbidden)
	e.fails(e.do(owner, http.MethodGet, "/api/app/surveys/"+uuid.NewString()+"/ai/translation-availability", nil), http.StatusNotFound, apperr.CodeNotFound)

	translate := "/api/app/workspaces/" + ws.String() + "/ai/translations"
	body := map[string]any{"fields": []any{map[string]any{"path": "blocks.0.elements.0.headline", "defaultText": "How was it?", "isRichText": false}}, "sourceLanguage": "English", "targetLanguage": "Deutsch"}
	e.fails(e.do(owner, http.MethodPost, translate, body), http.StatusForbidden, CodeSmartToolsDisabled)
	e.exec(`UPDATE organizations SET is_ai_smart_tools_enabled = true WHERE id = ?`, org)
	e.ok(e.do(owner, http.MethodGet, availability, nil), &a)
	if a != (Availability{Available: true}) {
		t.Fatalf("%+v", a)
	}
	e.fails(e.do(reader, http.MethodPost, translate, body), http.StatusForbidden, apperr.CodeForbidden)
	e.fails(e.do(owner, http.MethodPost, translate, map[string]any{"fields": []any{}, "sourceLanguage": "English", "targetLanguage": "Deutsch"}), http.StatusBadRequest, apperr.CodeValidation)
	e.fails(e.do(owner, http.MethodPost, translate, map[string]any{"fields": body["fields"], "sourceLanguage": "", "targetLanguage": "Deutsch"}), http.StatusBadRequest, apperr.CodeValidation)

	e.provider.answer = translator("DE ")
	var out TranslationResult
	e.ok(e.do(owner, http.MethodPost, translate, body), &out)
	if !reflect.DeepEqual(out, TranslationResult{Translations: map[string]string{"blocks.0.elements.0.headline": "DE How was it?"}, FailedPaths: []string{}}) {
		t.Fatalf("%+v", out)
	}
	e.provider.answer = func(map[string]any) (int, string) { return http.StatusTooManyRequests, "" }
	env := e.fails(e.do(owner, http.MethodPost, translate, body), http.StatusTooManyRequests, CodeQuotaExceeded)
	if env.Details["retryAfter"] != float64(17) {
		t.Fatalf("%+v", env)
	}
}

// The operator's query parameters can carry an API key (AI_OPENAI_COMPATIBLE_QUERY_PARAMS_JSON is a secret); a request that never reached the gateway fails with an error that does not name them, in the log or to the caller.
func TestTransportFailureDoesNotLogQueryParameters(t *testing.T) {
	e := newServiceEnv(t, func(c *config.AIConfig) { c.OpenAICompatibleQueryParamsJSON = `{"api-key": "sk-query-secret"}` })
	org, _ := e.organization(e.user(), true)
	e.provider.Close()
	_, err := e.svc.GenerateText(t.Context(), org, "", Request{Prompt: "Hi"})
	if err == nil || strings.Contains(err.Error(), "sk-query-secret") {
		t.Fatalf("generation error %v", err)
	}
	out, err := e.svc.TranslateFields(t.Context(), org, fields(1), "en", "de")
	if err == nil && len(out.FailedPaths) == 0 {
		t.Fatalf("translation through an unreachable gateway succeeded: %+v", out)
	}
	if err != nil && strings.Contains(err.Error(), "sk-query-secret") {
		t.Fatalf("translation error %v", err)
	}
	if len(e.logs.lines(t, "Failed to generate organization AI text")) != 1 || len(e.logs.lines(t, "AI translation chunk failed")) != 1 {
		t.Fatalf("the failures were not logged: %s", e.logs.String())
	}
	if strings.Contains(e.logs.String(), "sk-query-secret") {
		t.Fatalf("the log carries the query secret: %s", e.logs.String())
	}
}
