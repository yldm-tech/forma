package app

import (
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
	"testing/fstest"

	"go.yaml.in/yaml/v3"

	"github.com/yldm-tech/forma/server/internal/actionclass"
	"github.com/yldm-tech/forma/server/internal/ai"
	"github.com/yldm-tech/forma/server/internal/analysis"
	"github.com/yldm-tech/forma/server/internal/apikey"
	"github.com/yldm-tech/forma/server/internal/auth"
	"github.com/yldm-tech/forma/server/internal/billing"
	"github.com/yldm-tech/forma/server/internal/clientapi"
	"github.com/yldm-tech/forma/server/internal/contact"
	"github.com/yldm-tech/forma/server/internal/health"
	"github.com/yldm-tech/forma/server/internal/integration"
	"github.com/yldm-tech/forma/server/internal/invite"
	"github.com/yldm-tech/forma/server/internal/linksurvey"
	"github.com/yldm-tech/forma/server/internal/mcp"
	"github.com/yldm-tech/forma/server/internal/mgmtapi"
	"github.com/yldm-tech/forma/server/internal/mgmtapi/mgmtv1"
	"github.com/yldm-tech/forma/server/internal/notification"
	"github.com/yldm-tech/forma/server/internal/oauthserver"
	"github.com/yldm-tech/forma/server/internal/org"
	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/platform/httpapi"
	surveyv3 "github.com/yldm-tech/forma/server/internal/platform/v3api"
	"github.com/yldm-tech/forma/server/internal/response"
	"github.com/yldm-tech/forma/server/internal/storage"
	"github.com/yldm-tech/forma/server/internal/survey"
	"github.com/yldm-tech/forma/server/internal/tag"
	"github.com/yldm-tech/forma/server/internal/team"
	"github.com/yldm-tech/forma/server/internal/v3api"
	"github.com/yldm-tech/forma/server/internal/webhook"
	"github.com/yldm-tech/forma/server/internal/webui"
	"github.com/yldm-tech/forma/server/internal/workflow"
	"github.com/yldm-tech/forma/server/internal/workspace"
)

// The v3 route parity check, forma2's scripts/check-v3-route-parity.mjs: every /api/v3 operation the production router serves is documented in the committed bundle, and every documented operation is served. Documenting an operation is what enrols it in the published reference and in the Schemathesis contract suite (docs/api-v3-reference/contract-tests), which drives exactly what the bundle lists; nothing else notices a route that ships without an entry, or an entry whose route never landed.

// v3Bundle is the committed v3 OpenAPI bundle, relative to this package.
const v3Bundle = "../../../docs/api-v3-reference/openapi.yml"

// documentedWithoutRoute lists documented operations that deliberately have no route yet, each with the reason. The list may only shrink: an entry that has become untrue fails the check.
var documentedWithoutRoute = map[string]string{
	"GET /api/v3/responses":                 "ENG-2959: forma2 agreed this contract before building it (ENG-2868) and never routed it",
	"POST /api/v3/responses":                "ENG-2959: forma2 agreed this contract before building it (ENG-2868) and never routed it",
	"GET /api/v3/responses/count":           "ENG-2959: forma2 agreed this contract before building it (ENG-2868) and never routed it",
	"POST /api/v3/responses/validate":       "ENG-2959: forma2 agreed this contract before building it (ENG-2868) and never routed it",
	"POST /api/v3/responses/batch-delete":   "ENG-2959: forma2 agreed this contract before building it (ENG-2868) and never routed it",
	"GET /api/v3/responses/{responseId}":    "ENG-2959: forma2 agreed this contract before building it (ENG-2868) and never routed it",
	"PATCH /api/v3/responses/{responseId}":  "ENG-2959: forma2 agreed this contract before building it (ENG-2868) and never routed it",
	"DELETE /api/v3/responses/{responseId}": "ENG-2959: forma2 agreed this contract before building it (ENG-2868) and never routed it",
}

// routeWithoutDocumentation lists served operations with no documented operation, recorded rather than hidden. The list may only shrink: document the operation, never add an entry.
var routeWithoutDocumentation = map[string]string{
	"POST /api/v3/surveys/templates": "undocumented since it shipped in forma2, where this gate found it; needs a paths file with x-excluded: true",
}

func TestV3RouteParity(t *testing.T) {
	served := servedV3Operations(t)
	documented := documentedV3Operations(t)
	undocumented, unrouted, stale := compareV3Operations(served, documented, routeWithoutDocumentation, documentedWithoutRoute)

	for _, op := range undocumented {
		t.Errorf("%s is served but not documented: add a file under docs/api-v3-reference/src/paths/, reference it from src/openapi.yml and run `pnpm api:v3:bundle` (an operation not ready for real data is documented with x-excluded: true)", op)
	}
	for _, op := range unrouted {
		t.Errorf("%s is documented but not served, so the contract suite fails on it: implement the route, remove the operation from the bundle, or record it in documentedWithoutRoute with the ticket that removes it", op)
	}
	for _, op := range stale {
		t.Errorf("%s is recorded as an exception that no longer applies: delete the entry", op)
	}
	if !t.Failed() {
		t.Logf("%d served /api/v3 operations, %d documented", len(served), len(documented))
	}
}

func TestCompareV3Operations(t *testing.T) {
	served := []string{"GET /api/v3/a", "POST /api/v3/b", "GET /api/v3/hidden"}
	documented := []string{"GET /api/v3/a", "GET /api/v3/c", "GET /api/v3/planned"}
	undocumented, unrouted, stale := compareV3Operations(served, documented,
		map[string]string{"GET /api/v3/hidden": "", "GET /api/v3/a": "", "GET /api/v3/gone": ""},
		map[string]string{"GET /api/v3/planned": "", "POST /api/v3/b": ""})

	if want := []string{"POST /api/v3/b"}; !slices.Equal(undocumented, want) {
		t.Errorf("undocumented = %v, want %v", undocumented, want)
	}
	if want := []string{"GET /api/v3/c"}; !slices.Equal(unrouted, want) {
		t.Errorf("unrouted = %v, want %v", unrouted, want)
	}
	// "GET /api/v3/a" is now documented, "GET /api/v3/gone" is no longer served, and "POST /api/v3/b" is served after all.
	if want := []string{"GET /api/v3/a", "GET /api/v3/gone", "POST /api/v3/b"}; !slices.Equal(stale, want) {
		t.Errorf("stale = %v, want %v", stale, want)
	}
}

func TestOpenAPIPath(t *testing.T) {
	cases := map[string]string{
		"/api/v3/surveys":                    "/api/v3/surveys",
		"/api/v3/surveys/:surveyId":          "/api/v3/surveys/{surveyId}",
		"/api/v3/workflows/runs/:runId/logs": "/api/v3/workflows/runs/{runId}/logs",
		"/api/v3/files/*path":                "/api/v3/files/{path}",
	}
	for in, want := range cases {
		if got := openAPIPath(in); got != want {
			t.Errorf("openAPIPath(%q) = %q, want %q", in, got, want)
		}
	}
}

// compareV3Operations reports served operations nobody documented, documented operations nothing serves, and exceptions that no longer hold, each sorted.
func compareV3Operations(served, documented []string, allowUndocumented, allowUnrouted map[string]string) (undocumented, unrouted, stale []string) {
	for _, op := range served {
		if !slices.Contains(documented, op) {
			if _, ok := allowUndocumented[op]; !ok {
				undocumented = append(undocumented, op)
			}
		}
	}
	for _, op := range documented {
		if !slices.Contains(served, op) {
			if _, ok := allowUnrouted[op]; !ok {
				unrouted = append(unrouted, op)
			}
		}
	}
	for op := range allowUndocumented {
		if slices.Contains(documented, op) || !slices.Contains(served, op) {
			stale = append(stale, op)
		}
	}
	for op := range allowUnrouted {
		if slices.Contains(served, op) || !slices.Contains(documented, op) {
			stale = append(stale, op)
		}
	}
	sort.Strings(undocumented)
	sort.Strings(unrouted)
	sort.Strings(stale)
	return undocumented, unrouted, stale
}

var ginParam = regexp.MustCompile(`[:*]([^/]+)`)

// openAPIPath turns gin's `:name` and `*name` segments into OpenAPI's `{name}`.
func openAPIPath(path string) string {
	return ginParam.ReplaceAllString(path, "{$1}")
}

// servedV3Operations builds the production router (NewRouter, the graph `forma serve` mounts) with handlers that have no dependencies behind them, which is enough to register every route, and lists its /api/v3 operations. HEAD and OPTIONS are gin's and CORS's, never documented.
func servedV3Operations(t *testing.T) []string {
	t.Helper()
	webURL, _ := url.Parse("https://app.example.com")
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.Config{WebAppURL: webURL, SessionMaxAge: 86400}
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
	mcpHandler, err := mcp.NewHandler(cfg, nil, nil, nil, nil, nil, surveys, workflows, log)
	if err != nil {
		t.Fatal(err)
	}
	v1 := mgmtv1.New(cfg, nil, nil, nil, nil, nil, log)

	r := NewRouter(p, sessions, nil,
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
		analysis.NewHandler(nil, sessions),
		ai.NewHandler(nil, sessions),
		storage.NewHandler(cfg, nil, nil, sessions, nil, nil, nil, nil, log),
		linksurvey.NewHandler(cfg, nil, nil, web, log),
		clientapi.NewHandler(cfg, nil, nil, nil, nil, nil, nil, nil, nil, log),
		webhook.NewHandler(nil, sessions),
		webhook.NewV1Handler(nil, v1, log),
		mgmtapi.NewHandler(cfg, nil, nil, v1, nil, nil, nil, nil, nil, sessions, nil, nil, nil, nil, nil, nil, log),
		integration.NewHandler(nil, sessions),
		integration.NewOAuthHandler(nil, sessions, nil, log),
		notification.NewHandler(web),
		analysis.NewAttachmentsHandler(nil, nil, v3, nil, nil, log),
		workflows,
		oauth,
		oauthserver.NewAppHandler(oauth, sessions),
		mcpHandler,
		billing.NewHandler(nil, sessions),
		billing.NewWebhookHandler(nil, log),
		web,
	)

	var ops []string
	for _, route := range r.Routes() {
		if route.Method == http.MethodHead || route.Method == http.MethodOptions || !strings.HasPrefix(route.Path, "/api/v3/") {
			continue
		}
		op := route.Method + " " + openAPIPath(route.Path)
		if !slices.Contains(ops, op) {
			ops = append(ops, op)
		}
	}
	if len(ops) == 0 {
		t.Fatal("the router serves no /api/v3 operation; the parity check would prove nothing")
	}
	sort.Strings(ops)
	return ops
}

// documentedV3Operations lists the operations of the committed bundle.
func documentedV3Operations(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.FromSlash(v3Bundle))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Paths map[string]map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse %s: %v", v3Bundle, err)
	}
	var ops []string
	for path, item := range doc.Paths {
		for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
			if _, ok := item[strings.ToLower(method)]; ok {
				ops = append(ops, method+" "+path)
			}
		}
	}
	if len(ops) == 0 {
		t.Fatalf("%s documents no operation", v3Bundle)
	}
	sort.Strings(ops)
	return ops
}
