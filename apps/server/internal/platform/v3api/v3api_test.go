package v3api

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/server/internal/platform/ratelimit"
)

func serve(t *testing.T, handler gin.HandlerFunc, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Handle(method, "/api/v3/things", handler)
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestProblemResponses(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	rec := serve(t, func(c *gin.Context) {
		Fail(c, log, Unprocessable("Survey document failed validation", InvalidParam{Name: "blocks.0.id", Reason: "duplicated", Code: CodeDuplicateIdentifier, FirstUsedAt: "blocks.1.id"}))
	}, http.MethodGet, "/api/v3/things", "")
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	params := body["invalid_params"].([]any)
	if rec.Code != 422 || rec.Header().Get("Content-Type") != "application/problem+json" || rec.Header().Get("Cache-Control") != CacheNoStore ||
		body["title"] != "Unprocessable Content" || body["status"] != float64(422) || body["code"] != "unprocessable_content" || body["instance"] != "/api/v3/things" ||
		len(params) != 1 || params[0].(map[string]any)["firstUsedAt"] != "blocks.1.id" || params[0].(map[string]any)["missingId"] != nil {
		t.Fatalf("%d %v %s", rec.Code, rec.Header(), rec.Body.String())
	}

	rec = serve(t, func(c *gin.Context) { Fail(c, log, errors.New("disk on fire")) }, http.MethodGet, "/api/v3/things", "")
	if rec.Code != 500 || strings.Contains(rec.Body.String(), "fire") || !strings.Contains(rec.Body.String(), `"detail":"An unexpected error occurred."`) {
		t.Fatalf("internal error leaked: %s", rec.Body.String())
	}

	rec = serve(t, func(c *gin.Context) {
		Fail(c, log, TooManyRequests("Maximum number of requests reached. Please try again later.", 42))
	}, http.MethodGet, "/api/v3/things", "")
	if rec.Code != 429 || rec.Header().Get("Retry-After") != "42" {
		t.Fatalf("rate limit %d %v", rec.Code, rec.Header())
	}
}

func TestInputHelpers(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	read := func(c *gin.Context) {
		raw, err := ReadJSON(c)
		if err != nil {
			Fail(c, log, err)
			return
		}
		if err := AllowQuery(c, "lang"); err != nil {
			Fail(c, log, err)
			return
		}
		OK(c, raw)
	}
	if rec := serve(t, read, http.MethodPost, "/api/v3/things?lang=de", `{"a": 1}`); rec.Code != 200 || rec.Body.String() != `{"data":{"a":1}}` {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if rec := serve(t, read, http.MethodPost, "/api/v3/things", `{"a"`); rec.Code != 400 || !strings.Contains(rec.Body.String(), "Malformed JSON input") {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if rec := serve(t, read, http.MethodPost, "/api/v3/things?b=1&a=2", `{}`); rec.Code != 400 || !strings.Contains(rec.Body.String(), `Unrecognized keys: \"a\", \"b\"`) {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if rec := serve(t, read, http.MethodPost, "/api/v3/things", `"`+strings.Repeat("x", MaxBodyBytes)+`"`); rec.Code != 413 {
		t.Fatalf("%d", rec.Code)
	}
}

// stubSessions stands in for the session middleware: Optional records err with httpapi.Fail, as auth.Sessions does when the session store fails, or else signs in user when it is set.
type stubSessions struct {
	err  error
	user uuid.UUID
}

func (s stubSessions) Require() gin.HandlerFunc { return s.Optional() }

func (s stubSessions) Optional() gin.HandlerFunc {
	return func(c *gin.Context) {
		if s.err != nil {
			httpapi.Fail(c, s.err)
			return
		}
		if s.user != uuid.Nil {
			httpapi.SetActor(c, httpapi.Actor{UserID: s.user})
		}
		c.Next()
	}
}

func guarded(t *testing.T, sessions httpapi.SessionAuth, method string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	webApp, _ := url.Parse("https://app.example.com")
	cfg := config.Config{WebAppURL: webApp, RateLimitingDisabled: true}
	g := NewGuard(cfg, sessions, nil, ratelimit.New(cfg, nil, log), log)
	r := gin.New()
	r.Handle(method, "/api/v3/things", append(g.Handlers(SessionOrAPIKey), func(c *gin.Context) { OK(c, "done") })...)
	req := httptest.NewRequest(method, "/api/v3/things", nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// A session the store cannot read is a 500 problem, not gin's empty 200.
func TestGuardRendersSessionFailures(t *testing.T) {
	rec := guarded(t, stubSessions{err: errors.New("load session: connection refused")}, http.MethodGet, nil)
	if rec.Code != http.StatusInternalServerError || rec.Header().Get("Content-Type") != "application/problem+json" || !strings.Contains(rec.Body.String(), `"code":"internal_server_error"`) || strings.Contains(rec.Body.String(), "refused") {
		t.Fatalf("%d %s %s", rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
	}
	rec = guarded(t, stubSessions{err: apperr.NotAuthenticated("Not authenticated")}, http.MethodGet, nil)
	if rec.Code != http.StatusUnauthorized || rec.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
}

// A session-authenticated mutation passes the /api/app CSRF rule; a safe method does not need it.
func TestGuardChecksCSRFOnSessionMutations(t *testing.T) {
	user := stubSessions{user: uuid.New()}
	cases := []struct {
		name    string
		method  string
		headers map[string]string
		status  int
		detail  string
	}{
		{"a read from anywhere", http.MethodGet, nil, http.StatusOK, ""},
		{"a cross-site mutation", http.MethodPost, map[string]string{"Origin": "https://evil.example.com", httpapi.CSRFHeader: "XMLHttpRequest"}, http.StatusForbidden, "Cross-origin request rejected"},
		{"a mutation without an origin", http.MethodDelete, map[string]string{httpapi.CSRFHeader: "XMLHttpRequest"}, http.StatusForbidden, "Cross-origin request rejected"},
		{"a mutation without the header", http.MethodPatch, map[string]string{"Origin": "https://app.example.com"}, http.StatusForbidden, "Missing X-Requested-With header"},
		{"a mutation from the app", http.MethodPost, map[string]string{"Origin": "https://app.example.com", httpapi.CSRFHeader: "XMLHttpRequest"}, http.StatusOK, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := guarded(t, user, tc.method, tc.headers)
			if rec.Code != tc.status || (tc.detail != "" && !strings.Contains(rec.Body.String(), `"detail":"`+tc.detail+`"`)) {
				t.Fatalf("%d %s", rec.Code, rec.Body.String())
			}
		})
	}
}
