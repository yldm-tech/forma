package httpserver

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRequestID(t *testing.T) {
	r := NewEngine(slog.New(slog.NewTextHandler(io.Discard, nil)))
	r.GET("/ping", func(c *gin.Context) { c.String(http.StatusOK, "pong") })

	t.Run("mints an id when none is sent", func(t *testing.T) {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ping", nil))
		if got := rec.Header().Get(requestIDHeader); len(got) != 32 {
			t.Fatalf("X-Request-Id = %q, want a 32-char hex id", got)
		}
	})

	t.Run("keeps an inbound id", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/ping", nil)
		req.Header.Set(requestIDHeader, "gateway-123")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if got := rec.Header().Get(requestIDHeader); got != "gateway-123" {
			t.Fatalf("X-Request-Id = %q, want gateway-123", got)
		}
	})

	t.Run("replaces an oversized inbound id", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/ping", nil)
		req.Header.Set(requestIDHeader, strings.Repeat("x", 129))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if got := rec.Header().Get(requestIDHeader); len(got) != 32 {
			t.Fatalf("X-Request-Id = %q, want a minted id", got)
		}
	})
}

func TestRecoveryReturnsJSONAndLogs(t *testing.T) {
	var logs bytes.Buffer
	r := NewEngine(slog.New(slog.NewJSONHandler(&logs, nil)))
	r.GET("/boom", func(*gin.Context) { panic("kaboom") })

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/boom", nil))

	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), `"code":"internal_server_error"`) {
		t.Fatalf("got %d %q, want 500 internal_server_error", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "kaboom") {
		t.Fatalf("panic value leaked to the client: %q", rec.Body.String())
	}
	if !strings.Contains(logs.String(), "kaboom") {
		t.Fatalf("panic was not logged: %q", logs.String())
	}
}

// forma2's next.config.mjs headers(): every response carries the security headers, and only the public survey pages may be framed by another site.
func TestSecurityHeaders(t *testing.T) {
	r := NewEngine(slog.New(slog.NewTextHandler(io.Discard, nil)))
	r.GET("/api/app/me", func(c *gin.Context) { c.String(http.StatusOK, "{}") })
	r.NoRoute(func(c *gin.Context) { c.String(http.StatusOK, "<!DOCTYPE html>") })

	common := map[string]string{
		"X-Content-Type-Options":    "nosniff",
		"Strict-Transport-Security": "max-age=63072000; includeSubDomains; preload",
		"Referrer-Policy":           "strict-origin-when-cross-origin",
		"Permissions-Policy":        "camera=(), microphone=(), geolocation=()",
	}
	cases := []struct {
		path           string
		frameOptions   string
		frameAncestors string
	}{
		{"/api/app/me", "SAMEORIGIN", "frame-ancestors 'self'"},
		{"/invite?token=secret", "SAMEORIGIN", "frame-ancestors 'self'"},
		{"/organizations/1/settings/members", "SAMEORIGIN", "frame-ancestors 'self'"},
		{"/s", "SAMEORIGIN", "frame-ancestors 'self'"},
		{"/survey/1", "SAMEORIGIN", "frame-ancestors 'self'"},
		{"/s/abc", "", "frame-ancestors *"},
		{"/c/abc", "", "frame-ancestors *"},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
			for name, want := range common {
				if got := rec.Header().Get(name); got != want {
					t.Errorf("%s = %q, want %q", name, got, want)
				}
			}
			if got := rec.Header().Get("X-Frame-Options"); got != tc.frameOptions {
				t.Errorf("X-Frame-Options = %q, want %q", got, tc.frameOptions)
			}
			csp := rec.Header().Get("Content-Security-Policy")
			if !strings.HasPrefix(csp, "default-src 'self'; ") || !strings.HasSuffix(csp, "; "+tc.frameAncestors) {
				t.Errorf("Content-Security-Policy = %q, want forma2's policy ending in %q", csp, tc.frameAncestors)
			}
		})
	}
}
