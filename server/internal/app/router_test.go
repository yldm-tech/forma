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

	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/server/internal/platform/httpserver"
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
