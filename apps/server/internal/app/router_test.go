package app

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"sort"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"

	"github.com/yldm-tech/forma/apps/server/internal/actionclass"
	"github.com/yldm-tech/forma/apps/server/internal/ai"
	"github.com/yldm-tech/forma/apps/server/internal/analysis"
	"github.com/yldm-tech/forma/apps/server/internal/apikey"
	"github.com/yldm-tech/forma/apps/server/internal/auth"
	"github.com/yldm-tech/forma/apps/server/internal/billing"
	"github.com/yldm-tech/forma/apps/server/internal/clientapi"
	"github.com/yldm-tech/forma/apps/server/internal/contact"
	"github.com/yldm-tech/forma/apps/server/internal/contact/contactsurvey"
	"github.com/yldm-tech/forma/apps/server/internal/health"
	"github.com/yldm-tech/forma/apps/server/internal/instance"
	"github.com/yldm-tech/forma/apps/server/internal/integration"
	"github.com/yldm-tech/forma/apps/server/internal/invite"
	"github.com/yldm-tech/forma/apps/server/internal/linksurvey"
	"github.com/yldm-tech/forma/apps/server/internal/mcp"
	"github.com/yldm-tech/forma/apps/server/internal/mgmtapi"
	"github.com/yldm-tech/forma/apps/server/internal/mgmtapi/mgmtv1"
	"github.com/yldm-tech/forma/apps/server/internal/notification"
	"github.com/yldm-tech/forma/apps/server/internal/oauthserver"
	"github.com/yldm-tech/forma/apps/server/internal/org"
	"github.com/yldm-tech/forma/apps/server/internal/plain"
	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpserver"
	surveyv3 "github.com/yldm-tech/forma/apps/server/internal/platform/v3api"
	"github.com/yldm-tech/forma/apps/server/internal/response"
	"github.com/yldm-tech/forma/apps/server/internal/sdkidentity"
	"github.com/yldm-tech/forma/apps/server/internal/storage"
	"github.com/yldm-tech/forma/apps/server/internal/survey"
	"github.com/yldm-tech/forma/apps/server/internal/survey/emailembed"
	"github.com/yldm-tech/forma/apps/server/internal/tag"
	"github.com/yldm-tech/forma/apps/server/internal/team"
	"github.com/yldm-tech/forma/apps/server/internal/unsplash"
	"github.com/yldm-tech/forma/apps/server/internal/v3api"
	"github.com/yldm-tech/forma/apps/server/internal/v3api/discovery"
	"github.com/yldm-tech/forma/apps/server/internal/webhook"
	"github.com/yldm-tech/forma/apps/server/internal/webui"
	"github.com/yldm-tech/forma/apps/server/internal/workflow"
	"github.com/yldm-tech/forma/apps/server/internal/workspace"
)

// echoModule stands in for a feature module: it reports the client address it saw and fails on demand.
type echoModule struct{}

func (echoModule) Register(r gin.IRouter) {
	r.POST("/echo", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ip": httpapi.ClientIP(c.Request.Context())})
	})
	r.GET("/missing", func(c *gin.Context) { httpapi.Fail(c, apperr.NotFound("Survey", "")) })
	r.GET("/broken", func(c *gin.Context) { httpapi.Fail(c, errors.New("database is on fire")) })
}

func TestAppAPIConventions(t *testing.T) {
	webURL, _ := url.Parse("https://app.example.com")
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.Config{WebAppURL: webURL, TrustedProxyHopCount: 1}
	p := &Platform{Config: cfg, Log: log, ClientIP: httpapi.NewClientIPResolver(cfg, log)}

	r := httpserver.NewEngine(cfg, log)
	r.Use(p.ClientIP.Middleware())
	mountAppAPI(r, p, echoModule{})

	cases := []struct {
		name   string
		method string
		path   string
		origin string
		want   int
		body   string
	}{
		{"mutation from the app origin", http.MethodPost, "/api/app/echo", "https://app.example.com", http.StatusOK, `"ip":"203.0.113.7"`},
		{"cross-site mutation", http.MethodPost, "/api/app/echo", "https://evil.example", http.StatusForbidden, `"code":"csrf_rejected"`},
		{"expected error", http.MethodGet, "/api/app/missing", "", http.StatusNotFound, `"message":"Survey not found"`},
		{"unexpected error", http.MethodGet, "/api/app/broken", "", http.StatusInternalServerError, `"code":"internal_server_error"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.Header.Set("X-Forwarded-For", "203.0.113.7")
			req.Header.Set(httpapi.CSRFHeader, "XMLHttpRequest")
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != tc.want || !strings.Contains(rec.Body.String(), tc.body) {
				t.Fatalf("got %d %s, want %d containing %s", rec.Code, rec.Body.String(), tc.want, tc.body)
			}
			if strings.Contains(rec.Body.String(), "on fire") {
				t.Fatal("an internal error reached the client")
			}
		})
	}
}

// Gin panics when two routes collide, so mounting every feature module on one engine proves their paths coexist, including a static segment beside a parameter (/invites/accept beside /invites/:inviteId).
func TestFeatureRoutesCoexist(t *testing.T) {
	webURL, _ := url.Parse("https://app.example.com")
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.Config{WebAppURL: webURL, SessionMaxAge: 86400}
	p := &Platform{Config: cfg, Log: log, ClientIP: httpapi.NewClientIPResolver(cfg, log)}
	sessions := auth.NewSessions(cfg, nil, log)

	sso := auth.NewSSOHandler(cfg, auth.NewSSO(cfg, nil, log))

	surveys := survey.NewHandler(nil, sessions, surveyv3.NewGuard(cfg, sessions, nil, nil, log), nil, log)

	workflows := workflow.NewHandler(cfg, nil, surveyv3.NewGuard(cfg, sessions, nil, nil, log), sessions, nil, log)

	r := httpserver.NewEngine(cfg, log)
	v3 := v3api.NewMiddleware(cfg, nil, nil)
	actionClasses, tags := actionclass.NewHandler(nil, sessions, v3), tag.NewHandler(nil, sessions, v3, nil)
	mountAppAPI(r, p, auth.NewHandler(cfg, nil, sessions, nil), sso, org.NewHandler(nil, sessions), workspace.NewHandler(nil, sessions), team.NewHandler(nil, sessions), invite.NewHandler(nil, sessions), apikey.NewHandler(nil, sessions, nil), surveys, contact.NewHandler(nil, sessions), actionClasses, tags, ai.NewHandler(nil, nil, sessions), workflows, billing.NewHandler(nil, sessions), plain.NewHandler(cfg, nil, sessions), sdkidentity.NewHandler(nil, sessions))
	billing.NewWebhookHandler(nil, log).Register(r)
	surveys.RegisterV3(r)
	workflows.RegisterV3(r)
	mountAuthenticatedV3API(r, p, sessions, nil, contact.NewV3Handler(nil))
	discoveryHandler, err := discovery.NewHandler(cfg, nil, nil, v3)
	if err != nil {
		t.Fatal(err)
	}
	mountV3API(r, p, sessions, actionClasses, tags, discoveryHandler)
	sso.RegisterCallbacks(r)
	web, err := webui.NewFromFS(fstest.MapFS{})
	if err != nil {
		t.Fatal(err)
	}
	linksurvey.NewHandler(cfg, nil, nil, web, log).Register(r)
	clientapi.NewHandler(cfg, nil, nil, nil, nil, nil, nil, nil, nil, nil, log).Register(r)

	want := map[string]bool{
		"GET /s/:surveyId":                                                            true,
		"HEAD /s/:surveyId":                                                           true,
		"POST /s/:surveyId/pin":                                                       true,
		"POST /s/:surveyId/verify-email":                                              true,
		"GET /c/:token":                                                               true,
		"GET /p/:slug":                                                                true,
		"GET /api/v1/client/og":                                                       true,
		"OPTIONS /api/v1/client/og":                                                   true,
		"GET /api/v1/client/:workspaceId/environment":                                 true,
		"POST /api/v2/client/:workspaceId/responses":                                  true,
		"PUT /api/v2/client/:workspaceId/responses/:responseId":                       true,
		"GET /api/v1/client/:workspaceId/displays/:displayId/response":                true,
		"POST /api/app/invites/accept":                                                true,
		"POST /api/app/invites/inspect":                                               true,
		"POST /api/app/invites/:inviteId/link":                                        true,
		"POST /api/app/organizations/:organizationId/invites/bulk":                    true,
		"POST /api/app/organizations/:organizationId/invites/:inviteId/resend":        true,
		"GET /api/app/organizations/:organizationId/teams":                            true,
		"PUT /api/app/teams/:teamId":                                                  true,
		"GET /api/app/workspaces/:workspaceId/teams":                                  true,
		"GET /api/app/workspaces/:workspaceId":                                        true,
		"GET /api/app/workspaces/:workspaceId/ai":                                     true,
		"POST /api/app/workspaces/:workspaceId/ai/translations":                       true,
		"GET /api/app/organizations/:organizationId/ai":                               true,
		"GET /api/app/surveys/:surveyId/ai/translation-availability":                  true,
		"POST /api/app/surveys/:surveyId/ai/example-responses":                        true,
		"GET /api/app/plain/session":                                                  true,
		"POST /api/app/surveys/generate/stream":                                       true,
		"GET /api/app/organizations/:organizationId/workspaces":                       true,
		"GET /api/app/organizations/:organizationId/members":                          true,
		"PATCH /api/app/organizations/:organizationId/api-keys/:apiKeyId":             true,
		"POST /api/app/auth/sso/sign-in":                                              true,
		"POST /api/app/workspaces/:workspaceId/contacts/import":                       true,
		"POST /api/app/workspaces/:workspaceId/contacts/export":                       true,
		"GET /api/app/workspaces/:workspaceId/contacts/:contactId":                    true,
		"PUT /api/app/surveys/:surveyId/segment":                                      true,
		"POST /api/app/surveys/:surveyId/segment/reset":                               true,
		"GET /api/v3/surveys/:surveyId":                                               true,
		"POST /api/v3/surveys/templates":                                              true,
		"POST /api/v3/surveys/generate":                                               true,
		"GET /api/v3/contact-attribute-keys":                                          true,
		"GET /api/auth/callback/:provider":                                            true,
		"POST /api/auth/saml/callback":                                                true,
		"GET /api/auth/saml/metadata":                                                 true,
		"POST /api/app/workspaces/:workspaceId/action-classes":                        true,
		"PATCH /api/app/action-classes/:actionClassId":                                true,
		"POST /api/app/workspaces/:workspaceId/tags":                                  true,
		"DELETE /api/app/workspaces/:workspaceId/languages/:languageId":               true,
		"GET /api/app/workspaces/:workspaceId/app-connection":                         true,
		"PATCH /api/app/workspaces/:workspaceId/sdk-identity":                         true,
		"POST /api/app/workspaces/:workspaceId/sdk-identity/secret":                   true,
		"GET /api/v3/action-classes":                                                  true,
		"GET /api/v3/me":                                                              true,
		"GET /api/v3/openapi.json":                                                    true,
		"GET /api/v3/tags":                                                            true,
		"POST /api/v3/tags/:tagId/merge":                                              true,
		"GET /api/v3/workflows":                                                       true,
		"GET /api/v3/workflows/runs":                                                  true,
		"GET /api/v3/workflows/runs/:runId":                                           true,
		"PATCH /api/v3/workflows/:workflowId":                                         true,
		"POST /api/v3/workflows/:workflowId/enable":                                   true,
		"POST /api/v3/workflows/:workflowId/test":                                     true,
		"GET /api/app/workspaces/:workspaceId/workflows/authoring-context":            true,
		"GET /api/app/organizations/:organizationId/billing":                          true,
		"POST /api/app/organizations/:organizationId/billing/setup-checkout/finalize": true,
		"POST /api/billing/stripe-webhook":                                            true,
	}
	for _, route := range r.Routes() {
		delete(want, route.Method+" "+route.Path)
	}
	if len(want) != 0 {
		t.Fatalf("routes not mounted: %v", want)
	}
}

// RootPathPrefixes are the first path segments, besides /app, that a request must keep to reach this server when a reverse proxy shares the host with something else (forma.yldm.ai sends every other path to the marketing website): every route the server mounts outside /app and the files of apps/web/public served at their root URL. The ingress in yldm-platform lists exactly these, plus /app.
//
// Each entry is a whole path segment, matched as the path itself and everything under it (/s and /s/...), never as a character prefix: a prefix rule for /s or /p would also take the website's /security or /pricing. A proxy whose prefix match is character-wise, such as Traefik's Ingress pathType Prefix, must route each entry as Exact /x plus Prefix /x/ (the single files favicon.ico and logo-transparent.png need only Exact).
var RootPathPrefixes = []string{
	".well-known",          // OAuth and OpenID discovery documents for MCP clients
	"animated-bgs",         // survey backgrounds (videos) stored by root URL
	"api",                  // every API: /api/app, the client API, management v1/v2, v3, MCP, OAuth, SSO and integration callbacks, Stripe
	"c",                    // personal link surveys
	"favicon",              // favicons
	"favicon.ico",          // favicon
	"health",               // liveness and readiness probes
	"image-backgrounds",    // survey backgrounds (images) stored by root URL
	"ingest",               // first-party PostHog proxy
	"js",                   // the SDK and survey runtime bundles customer sites embed
	"logo-transparent.png", // the logo emails embed
	"p",                    // pretty-URL surveys
	"s",                    // public link surveys
	"sample-csv",           // the invite template download
	"smiley-icons",         // rating icons emails and surveys load
	"star-icons",           // rating icons emails and surveys load
	"storage",              // stored files
}

// The production router answers exactly RootPathPrefixes outside /app, so the list an ingress is configured with cannot fall behind a new route, a new public file or a removed one.
func TestRootPathPrefixes(t *testing.T) {
	webURL, _ := url.Parse("https://app.example.com")
	cfg := config.Config{WebAppURL: webURL, SessionMaxAge: 86400}
	cfg.Telemetry.PostHogKey = "phc_test"
	r := newProductionRouter(t, cfg)

	served := map[string]bool{}
	for _, route := range r.Routes() {
		first, _, _ := strings.Cut(strings.TrimPrefix(route.Path, "/"), "/")
		served[first] = true
	}
	for _, first := range webui.RootPublicPaths {
		served[first] = true
	}
	if !served["app"] {
		t.Fatal("no route under /app; the router no longer mounts the email links there")
	}
	delete(served, "app")
	var got []string
	for first := range served {
		got = append(got, first)
	}
	sort.Strings(got)
	want := slices.Sorted(slices.Values(RootPathPrefixes))
	if !slices.Equal(got, want) {
		t.Fatalf("root path prefixes = %q, want %q; update RootPathPrefixes and the ingress together", got, want)
	}

	// Through the whole router, with every middleware in front: the root leads to the app, and a path outside the list, an admin page's path without /app included, is not the app.
	for _, path := range []string{"/pricing", "/workspaces/probe", "/auth/login"} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s = %d, want 404", path, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/app/" {
		t.Errorf("/ = %d %s, want 302 to /app/", rec.Code, rec.Header().Get("Location"))
	}
}

// newProductionRouter builds the production router (NewRouter, the graph `forma serve` mounts) with handlers that have no dependencies behind them, which is enough to register every route.
func newProductionRouter(t *testing.T, cfg config.Config) *gin.Engine {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	p := &Platform{Config: cfg, Log: log, ClientIP: httpapi.NewClientIPResolver(cfg, log)}
	sessions := auth.NewSessions(cfg, nil, log)
	guard := surveyv3.NewGuard(cfg, sessions, nil, nil, log)
	v3 := v3api.NewMiddleware(cfg, nil, nil)
	web, err := webui.NewFromFS(fstest.MapFS{})
	if err != nil {
		t.Fatal(err)
	}
	surveys := survey.NewHandler(nil, sessions, guard, nil, log)
	workflows := workflow.NewHandler(cfg, nil, guard, sessions, nil, log)
	oauth, err := oauthserver.NewHandler(cfg, nil, nil, sessions, nil, log)
	if err != nil {
		t.Fatal(err)
	}
	mcpHandler, err := mcp.NewHandler(cfg, nil, nil, nil, nil, nil, surveys, workflows, nil, log)
	if err != nil {
		t.Fatal(err)
	}
	v1 := mgmtv1.New(cfg, nil, nil, nil, nil, nil, log)
	discoveryHandler, err := discovery.NewHandler(cfg, nil, nil, v3)
	if err != nil {
		t.Fatal(err)
	}

	return NewRouter(p, sessions, nil,
		health.NewHandler(nil, log),
		auth.NewHandler(cfg, nil, sessions, nil),
		auth.NewSSOHandler(cfg, auth.NewSSO(cfg, nil, log)),
		org.NewHandler(nil, sessions),
		workspace.NewHandler(nil, sessions),
		team.NewHandler(nil, sessions),
		invite.NewHandler(nil, sessions),
		apikey.NewHandler(nil, sessions, nil),
		surveys,
		contact.NewHandler(nil, sessions),
		contact.NewV3Handler(nil),
		actionclass.NewHandler(nil, sessions, v3),
		tag.NewHandler(nil, sessions, v3, nil),
		response.NewHandler(nil, sessions),
		analysis.NewHandler(nil, sessions, nil),
		ai.NewHandler(nil, nil, sessions),
		storage.NewHandler(cfg, nil, nil, sessions, nil, nil, nil, nil, log),
		linksurvey.NewHandler(cfg, nil, nil, web, log),
		clientapi.NewHandler(cfg, nil, nil, nil, nil, nil, nil, nil, nil, nil, log),
		webhook.NewHandler(nil, sessions),
		webhook.NewV1Handler(nil, v1, log),
		mgmtapi.NewHandler(cfg, nil, nil, v1, nil, nil, nil, nil, nil, sessions, nil, nil, nil, nil, nil, nil, log),
		integration.NewHandler(nil, sessions),
		integration.NewOAuthHandler(nil, sessions, nil, log),
		notification.NewHandler(),
		analysis.NewAttachmentsHandler(nil, nil, v3, nil, nil, log),
		workflows,
		oauth,
		oauthserver.NewAppHandler(oauth, sessions),
		mcpHandler,
		billing.NewHandler(nil, sessions),
		billing.NewWebhookHandler(nil, log),
		instance.NewHandler(nil, nil, sessions),
		web,
		unsplash.NewHandler(nil, sessions),
		contactsurvey.NewHandler(nil, sessions),
		emailembed.NewHandler(nil, sessions),
		plain.NewHandler(cfg, nil, sessions),
		sdkidentity.NewHandler(nil, sessions),
		discoveryHandler,
	)
}

// v3SessionOnlyRoutes are the /api/v3 routes that refuse API keys, which the published OpenAPI document leaves out: the tag routes the responses page calls, and creating a survey from a template, which the SPA's template gallery calls.
var v3SessionOnlyRoutes = []string{
	"POST /api/v3/surveys/templates",
	"GET /api/v3/tags",
	"PATCH /api/v3/tags/{tagId}",
	"DELETE /api/v3/tags/{tagId}",
	"POST /api/v3/tags/{tagId}/merge",
}

// The OpenAPI document at /api/v3/openapi.json describes exactly the /api/v3 routes the production router mounts that an API key can call: a new route fails here until its module's V3OpenAPI describes it, and a route the document names must exist.
func TestOpenAPIDocumentCoversV3Routes(t *testing.T) {
	webURL, _ := url.Parse("https://app.example.com")
	cfg := config.Config{WebAppURL: webURL, SessionMaxAge: 86400}
	r := newProductionRouter(t, cfg)

	raw, err := discovery.Document(webURL.String())
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	documented := map[string]bool{}
	for path, item := range doc.Paths {
		for method := range item {
			documented[strings.ToUpper(method)+" "+path] = true
		}
	}

	mounted := map[string]bool{}
	for _, route := range r.Routes() {
		if !strings.HasPrefix(route.Path, "/api/v3/") {
			continue
		}
		segments := strings.Split(route.Path, "/")
		for i, s := range segments {
			if strings.HasPrefix(s, ":") {
				segments[i] = "{" + s[1:] + "}"
			}
		}
		mounted[route.Method+" "+strings.Join(segments, "/")] = true
	}
	for _, route := range v3SessionOnlyRoutes {
		if !mounted[route] {
			t.Errorf("%s is listed as session-only but not mounted", route)
		}
		if documented[route] {
			t.Errorf("%s is session-only but documented", route)
		}
		delete(mounted, route)
	}
	var missing, extra []string
	for route := range mounted {
		if !documented[route] {
			missing = append(missing, route)
		}
	}
	for route := range documented {
		if !mounted[route] {
			extra = append(extra, route)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) > 0 || len(extra) > 0 {
		t.Fatalf("v3 routes missing from the OpenAPI document: %q; documented but not mounted: %q", missing, extra)
	}

	// The session-only routes do refuse a request that carries only an API key, before any key is looked up.
	for _, route := range v3SessionOnlyRoutes {
		method, path, _ := strings.Cut(route, " ")
		path = strings.ReplaceAll(path, "{tagId}", "0197c1d2-0000-7000-8000-000000000001")
		req := httptest.NewRequest(method, path, strings.NewReader("{}"))
		req.Header.Set("X-Api-Key", "fbk_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "Session required") {
			t.Errorf("%s with an API key = %d %s, want 401 Session required", route, rec.Code, rec.Body.String())
		}
	}

	// The document itself needs no credentials and may be read from any origin.
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v3/openapi.json", nil))
	if rec.Code != http.StatusOK || rec.Header().Get("Access-Control-Allow-Origin") != "*" || rec.Body.String() != string(raw) {
		t.Fatalf("GET /api/v3/openapi.json = %d %v", rec.Code, rec.Header())
	}
}
