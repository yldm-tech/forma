package httpserver

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
)

func gateEngine(cfg config.Config) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(PublicDomainGate(cfg))
	r.NoRoute(func(c *gin.Context) { c.String(http.StatusOK, "served") })
	return r
}

func gateStatus(r http.Handler, host, path string) int {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Host = host
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec.Code
}

// forma2's endpoint-validator.test.ts and proxy behaviour.
func TestPublicDomainGate(t *testing.T) {
	cfg := config.Config{WebAppURL: &url.URL{Scheme: "https", Host: "app.example.com"}, PublicURL: &url.URL{Scheme: "https", Host: "surveys.example.com"}}
	r := gateEngine(cfg)
	cases := []struct {
		host, path string
		want       int
	}{
		{"surveys.example.com", "/s/abc", http.StatusOK},
		{"surveys.example.com", "/s/abc/pin", http.StatusOK},
		{"surveys.example.com", "/c/token", http.StatusOK},
		{"surveys.example.com", "/p/slug", http.StatusOK},
		{"surveys.example.com", "/api/v1/client/og", http.StatusOK},
		{"surveys.example.com", "/api/v2/client/ws/environment", http.StatusOK},
		{"surveys.example.com", "/health", http.StatusOK},
		{"surveys.example.com", "/storage/ws/public/logo.png", http.StatusOK},
		{"surveys.example.com", "/js/surveys.umd.js", http.StatusOK},
		{"surveys.example.com", "/app/assets/survey-1a2b.js", http.StatusOK},
		{"surveys.example.com", "/assets/survey-1a2b.js", http.StatusNotFound},
		{"surveys.example.com", "/app/", http.StatusNotFound},
		{"surveys.example.com", "/app/workspaces/abc", http.StatusNotFound},
		{"surveys.example.com", "/favicon.ico", http.StatusOK},
		{"surveys.example.com", "/animated-bgs/4K/1_4k.mp4", http.StatusOK},
		{"surveys.example.com", "/image-backgrounds/dogs.webp", http.StatusOK},
		{"surveys.example.com", "/favicon/apple-touch-icon.png", http.StatusOK},
		{"surveys.example.com", "/logo-transparent.png", http.StatusOK},
		{"surveys.example.com", "/star-icons/star.png", http.StatusOK},
		{"surveys.example.com", "/smiley-icons/tired-face.png", http.StatusOK},
		{"surveys.example.com", "/sample-csv/forma-organization-members-template.csv", http.StatusOK},
		{"surveys.example.com", "/image-backgroundsx/dogs.webp", http.StatusNotFound},
		{"surveys.example.com", "/logo-transparent.png/x", http.StatusNotFound},
		{"surveys.example.com", "/", http.StatusNotFound},
		{"surveys.example.com", "/s/", http.StatusNotFound},
		{"surveys.example.com", "/health/ready", http.StatusNotFound},
		{"surveys.example.com", "/api/app/auth/session", http.StatusNotFound},
		{"surveys.example.com", "/api/v3/surveys", http.StatusNotFound},
		{"surveys.example.com", "/storage/ws/private/file.pdf", http.StatusNotFound},
		{"surveys.example.com", "/workspaces/abc", http.StatusNotFound},
		{"app.example.com", "/workspaces/abc", http.StatusOK},
		{"app.example.com", "/app/workspaces/abc", http.StatusOK},
		{"app.example.com", "/app/assets/app-1a2b.js", http.StatusOK},
		{"app.example.com", "/api/app/auth/session", http.StatusOK},
		{"app.example.com", "/health/ready", http.StatusOK},
		{"app.example.com", "/storage/ws/private/file.pdf", http.StatusOK},
		{"app.example.com", "/js/forma.umd.cjs", http.StatusOK},
		{"app.example.com", "/image-backgrounds/dogs.webp", http.StatusOK},
		{"app.example.com", "/logo-transparent.png", http.StatusOK},
		{"app.example.com", "/s/abc", http.StatusNotFound},
		{"app.example.com", "/c/token", http.StatusNotFound},
		{"app.example.com", "/p/slug", http.StatusNotFound},
		{"app.example.com", "/api/v1/client/og", http.StatusNotFound},
		// An unknown host is treated as the admin domain.
		{"other.example.com", "/s/abc", http.StatusNotFound},
		{"other.example.com", "/workspaces", http.StatusOK},
	}
	for _, tc := range cases {
		if got := gateStatus(r, tc.host, tc.path); got != tc.want {
			t.Errorf("%s%s = %d, want %d", tc.host, tc.path, got, tc.want)
		}
	}
}

// Without PUBLIC_URL every route is served on every host.
func TestPublicDomainGateWithoutPublicURL(t *testing.T) {
	r := gateEngine(config.Config{WebAppURL: &url.URL{Scheme: "https", Host: "app.example.com"}})
	for _, path := range []string{"/s/abc", "/api/v1/client/og", "/workspaces", "/api/app/x"} {
		if got := gateStatus(r, "app.example.com", path); got != http.StatusOK {
			t.Errorf("%s = %d", path, got)
		}
	}
}
