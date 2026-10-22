package app

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/yldm-tech/forma/server/internal/actionclass"
	"github.com/yldm-tech/forma/server/internal/apikey"
	"github.com/yldm-tech/forma/server/internal/auth"
	"github.com/yldm-tech/forma/server/internal/contact"
	"github.com/yldm-tech/forma/server/internal/invite"
	"github.com/yldm-tech/forma/server/internal/org"
	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/server/internal/platform/httpserver"
	surveyv3 "github.com/yldm-tech/forma/server/internal/platform/v3api"
	"github.com/yldm-tech/forma/server/internal/survey"
	"github.com/yldm-tech/forma/server/internal/tag"
	"github.com/yldm-tech/forma/server/internal/team"
	"github.com/yldm-tech/forma/server/internal/v3api"
	"github.com/yldm-tech/forma/server/internal/workspace"
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

	r := httpserver.NewEngine(log)
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

	surveys := survey.NewHandler(nil, sessions, surveyv3.NewGuard(cfg, sessions, nil, nil, log), log)

	r := httpserver.NewEngine(log)
	v3 := v3api.NewMiddleware(cfg, nil, nil)
	actionClasses, tags := actionclass.NewHandler(nil, sessions, v3), tag.NewHandler(nil, sessions, v3, nil)
	mountAppAPI(r, p, auth.NewHandler(cfg, nil, sessions, nil), sso, org.NewHandler(nil, sessions), workspace.NewHandler(nil, sessions), team.NewHandler(nil, sessions), invite.NewHandler(nil, sessions), apikey.NewHandler(nil, sessions, nil), surveys, contact.NewHandler(nil, sessions), actionClasses, tags)
	surveys.RegisterV3(r)
	mountAuthenticatedV3API(r, p, sessions, nil, contact.NewV3Handler(nil))
	mountV3API(r, p, sessions, actionClasses, tags)
	sso.RegisterCallbacks(r)

	want := map[string]bool{
		"POST /api/app/invites/accept":                                         true,
		"POST /api/app/invites/inspect":                                        true,
		"POST /api/app/invites/:inviteId/link":                                 true,
		"POST /api/app/organizations/:organizationId/invites/bulk":             true,
		"POST /api/app/organizations/:organizationId/invites/:inviteId/resend": true,
		"GET /api/app/organizations/:organizationId/teams":                     true,
		"PUT /api/app/teams/:teamId":                                           true,
		"GET /api/app/workspaces/:workspaceId/teams":                           true,
		"GET /api/app/workspaces/:workspaceId":                                 true,
		"GET /api/app/organizations/:organizationId/workspaces":                true,
		"GET /api/app/organizations/:organizationId/members":                   true,
		"PATCH /api/app/organizations/:organizationId/api-keys/:apiKeyId":      true,
		"POST /api/app/auth/sso/sign-in":                                       true,
		"POST /api/app/workspaces/:workspaceId/contacts/import":                true,
		"GET /api/app/workspaces/:workspaceId/contacts/:contactId":             true,
		"PUT /api/app/surveys/:surveyId/segment":                               true,
		"POST /api/app/surveys/:surveyId/segment/reset":                        true,
		"GET /api/v3/surveys/:surveyId":                                        true,
		"POST /api/v3/surveys/templates":                                       true,
		"GET /api/v3/contact-attribute-keys":                                   true,
		"GET /api/auth/callback/:provider":                                     true,
		"POST /api/app/workspaces/:workspaceId/action-classes":                 true,
		"PATCH /api/app/action-classes/:actionClassId":                         true,
		"POST /api/app/workspaces/:workspaceId/tags":                           true,
		"DELETE /api/app/workspaces/:workspaceId/languages/:languageId":        true,
		"GET /api/app/workspaces/:workspaceId/app-connection":                  true,
		"GET /api/v3/action-classes":                                           true,
		"GET /api/v3/tags":                                                     true,
		"POST /api/v3/tags/:tagId/merge":                                       true,
	}
	for _, route := range r.Routes() {
		delete(want, route.Method+" "+route.Path)
	}
	if len(want) != 0 {
		t.Fatalf("routes not mounted: %v", want)
	}
}
