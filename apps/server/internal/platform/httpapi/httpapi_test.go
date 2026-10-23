package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/platform/config"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	os.Exit(m.Run())
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) Envelope {
	t.Helper()
	var e Envelope
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
		t.Fatalf("body %q is not an envelope: %v", rec.Body.String(), err)
	}
	return e
}

func TestErrorHandler(t *testing.T) {
	var logs bytes.Buffer
	r := gin.New()
	r.Use(ErrorHandler(slog.New(slog.NewTextHandler(&logs, nil))))
	r.GET("/expected", func(c *gin.Context) { Fail(c, fmt.Errorf("wrapped: %w", apperr.NotFound("Survey", "abc"))) })
	r.GET("/limited", func(c *gin.Context) { Fail(c, apperr.TooManyRequests(42)) })
	r.GET("/unexpected", func(c *gin.Context) { Fail(c, errors.New("pq: connection reset by peer")) })
	r.GET("/ok", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	cases := []struct {
		path       string
		status     int
		code       string
		message    string
		retryAfter string
	}{
		{"/expected", http.StatusNotFound, apperr.CodeNotFound, "Survey with ID abc not found", ""},
		{"/limited", http.StatusTooManyRequests, apperr.CodeTooManyRequests, apperr.TooManyRequestsMessage, "42"},
		{"/unexpected", http.StatusInternalServerError, apperr.CodeInternal, apperr.InternalMessage, ""},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
			e := decode(t, rec)
			if rec.Code != tc.status || e.Code != tc.code || e.Message != tc.message || rec.Header().Get("Retry-After") != tc.retryAfter {
				t.Fatalf("got %d %+v Retry-After %q", rec.Code, e, rec.Header().Get("Retry-After"))
			}
		})
	}
	if strings.Contains(logs.String(), "Survey") || !strings.Contains(logs.String(), "connection reset") {
		t.Fatalf("only the unexpected error should be logged:\n%s", logs.String())
	}

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ok", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("a successful handler was rewritten to %d", rec.Code)
	}
}

func csrfEngine(publicURL string) *gin.Engine {
	web, _ := url.Parse("https://app.example.com")
	cfg := config.Config{WebAppURL: web}
	if publicURL != "" {
		cfg.PublicURL, _ = url.Parse(publicURL)
	}
	r := gin.New()
	r.Use(CSRF(cfg))
	r.Any("/x", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	return r
}

func TestCSRF(t *testing.T) {
	cases := []struct {
		name   string
		method string
		origin string
		header bool
		public string
		want   int
	}{
		{"safe method needs nothing", http.MethodGet, "", false, "", http.StatusNoContent},
		{"head needs nothing", http.MethodHead, "https://evil.example", false, "", http.StatusNoContent},
		{"same origin with header", http.MethodPost, "https://app.example.com", true, "", http.StatusNoContent},
		{"origin compared case-insensitively", http.MethodDelete, "HTTPS://APP.EXAMPLE.COM", true, "", http.StatusNoContent},
		{"public origin allowed", http.MethodPatch, "https://surveys.example.com", true, "https://surveys.example.com/", http.StatusNoContent},
		{"missing origin", http.MethodPost, "", true, "", http.StatusForbidden},
		{"null origin", http.MethodPost, "null", true, "", http.StatusForbidden},
		{"foreign origin", http.MethodPost, "https://evil.example", true, "", http.StatusForbidden},
		{"scheme mismatch", http.MethodPut, "http://app.example.com", true, "", http.StatusForbidden},
		{"port mismatch", http.MethodPost, "https://app.example.com:8443", true, "", http.StatusForbidden},
		{"public origin without PUBLIC_URL", http.MethodPost, "https://surveys.example.com", true, "", http.StatusForbidden},
		{"same origin without header", http.MethodPost, "https://app.example.com", false, "", http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, "/x", nil)
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			if tc.header {
				req.Header.Set(CSRFHeader, "XMLHttpRequest")
			}
			rec := httptest.NewRecorder()
			csrfEngine(tc.public).ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d (%s)", rec.Code, tc.want, rec.Body.String())
			}
			if tc.want == http.StatusForbidden && decode(t, rec).Code != CodeCSRF {
				t.Fatalf("body = %s, want code %s", rec.Body.String(), CodeCSRF)
			}
		})
	}
}

func TestOrigin(t *testing.T) {
	cases := map[string]string{
		"http://localhost:5173/":       "http://localhost:5173",
		"https://App.Example.com:443/": "https://app.example.com",
		"http://example.com:80":        "http://example.com",
		"https://[::1]:8443":           "https://[::1]:8443",
	}
	for in, want := range cases {
		u, _ := url.Parse(in)
		if got := origin(u); got != want {
			t.Errorf("origin(%q) = %q, want %q", in, got, want)
		}
	}
}

type signupRequest struct {
	Name     string `json:"name" binding:"required,max=50"`
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=8"`
	Owner    *struct {
		Age int `json:"age" binding:"gte=18"`
	} `json:"owner"`
}

func bindRecorder(t *testing.T, contentType, body string) (*signupRequest, error) {
	t.Helper()
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", contentType)
	var dst signupRequest
	return &dst, BindJSON(c, &dst)
}

func TestBindJSON(t *testing.T) {
	dst, err := bindRecorder(t, "application/json; charset=utf-8", `{"name":"Ada","email":"ada@example.com","password":"longenough","extra":true}`)
	if err != nil || dst.Email != "ada@example.com" {
		t.Fatalf("valid body: %+v, %v", dst, err)
	}

	cases := []struct {
		name        string
		contentType string
		body        string
		code        string
		fields      []string
		status      int
	}{
		{"wrong content type", "text/plain", `{}`, apperr.CodeBadRequest, nil, http.StatusBadRequest},
		{"malformed json", "application/json", `{"name":`, apperr.CodeBadRequest, nil, http.StatusBadRequest},
		{"trailing data", "application/json", `{"name":"a","email":"a@b.co","password":"12345678"} {}`, apperr.CodeBadRequest, nil, http.StatusBadRequest},
		{"wrong type", "application/json", `{"name":1}`, apperr.CodeValidation, []string{"name"}, http.StatusBadRequest},
		{"failed constraints", "application/json", `{"name":"","email":"nope","password":"short","owner":{"age":3}}`, apperr.CodeValidation, []string{"name", "email", "password", "owner.age"}, http.StatusBadRequest},
		{"too large", "application/json", `{"name":"` + strings.Repeat("a", int(MaxBodyBytes)) + `"}`, apperr.CodeRequestTooLarge, nil, http.StatusRequestEntityTooLarge},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := bindRecorder(t, tc.contentType, tc.body)
			e, ok := apperr.As(err)
			if !ok || e.Code != tc.code || e.Status != tc.status {
				t.Fatalf("err = %v, want %s (%d)", err, tc.code, tc.status)
			}
			if tc.fields == nil {
				return
			}
			got := e.Details["fields"].([]apperr.FieldError)
			var names []string
			for _, f := range got {
				names = append(names, f.Field)
			}
			if strings.Join(names, ",") != strings.Join(tc.fields, ",") {
				t.Fatalf("fields = %+v, want %v", got, tc.fields)
			}
		})
	}
}

func TestBindQueryAndParamUUID(t *testing.T) {
	type query struct {
		Limit int `form:"limit" json:"limit" binding:"min=1,max=100"`
	}
	id := uuid.New()
	r := gin.New()
	r.Use(ErrorHandler(slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))))
	r.GET("/items/:id", func(c *gin.Context) {
		got, err := ParamUUID(c, "id")
		if err != nil {
			Fail(c, err)
			return
		}
		var q query
		if err := BindQuery(c, &q); err != nil {
			Fail(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"id": got, "limit": q.Limit})
	})

	cases := []struct {
		path   string
		status int
		field  string
	}{
		{"/items/" + id.String() + "?limit=10", http.StatusOK, ""},
		{"/items/not-a-uuid?limit=10", http.StatusBadRequest, "id"},
		{"/items/" + id.String() + "?limit=500", http.StatusBadRequest, "limit"},
		{"/items/" + id.String() + "?limit=many", http.StatusBadRequest, ""},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if rec.Code != tc.status {
			t.Fatalf("%s: status %d, want %d (%s)", tc.path, rec.Code, tc.status, rec.Body.String())
		}
		if tc.field != "" && !strings.Contains(rec.Body.String(), `"field":"`+tc.field+`"`) {
			t.Fatalf("%s: body %s does not name field %q", tc.path, rec.Body.String(), tc.field)
		}
	}
}

func TestActorContext(t *testing.T) {
	r := gin.New()
	actor := Actor{UserID: uuid.New(), SessionID: uuid.New()}
	var got Actor
	var ok bool
	r.GET("/", func(c *gin.Context) {
		if _, before := ActorFrom(c.Request.Context()); before {
			t.Error("actor present before SetActor")
		}
		SetActor(c, actor)
		got, ok = ActorFrom(c.Request.Context())
	})
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if !ok || got != actor {
		t.Fatalf("ActorFrom = %+v, %v; want %+v", got, ok, actor)
	}
}
