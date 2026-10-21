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

	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), `"code":"internal_error"`) {
		t.Fatalf("got %d %q, want 500 internal_error", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "kaboom") {
		t.Fatalf("panic value leaked to the client: %q", rec.Body.String())
	}
	if !strings.Contains(logs.String(), "kaboom") {
		t.Fatalf("panic was not logged: %q", logs.String())
	}
}
