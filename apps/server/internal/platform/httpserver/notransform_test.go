package httpserver

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"

	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
)

// Every HTML response says no-transform after the caching its handler chose, so Cloudflare does not inject its Web Analytics beacon; nothing else does, so Cloudflare still compresses it.
func TestNoTransformOnlyOnHTML(t *testing.T) {
	r := NewEngine(config.Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	html := func(cacheControl string) gin.HandlerFunc {
		return func(c *gin.Context) {
			if cacheControl != "" {
				c.Header("Cache-Control", cacheControl)
			}
			c.Data(http.StatusOK, "text/html; charset=utf-8", []byte("<!doctype html>"))
		}
	}
	r.GET("/page", html("private, no-cache, no-store, max-age=0, must-revalidate"))
	r.GET("/bare", html(""))
	r.GET("/marked", html("no-cache, No-Transform"))
	r.GET("/missing", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Data(http.StatusNotFound, "text/html; charset=utf-8", []byte("<!doctype html><title>Not found</title>"))
	})
	r.GET("/redirect", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Redirect(http.StatusFound, "/page")
	})
	r.GET("/file.html", func(c *gin.Context) {
		c.Header("Cache-Control", "no-cache")
		c.FileFromFS("/file.html", http.FS(fstest.MapFS{"file.html": {Data: []byte("<!doctype html>")}}))
	})
	r.GET("/app.js", func(c *gin.Context) {
		c.Header("Cache-Control", "public, max-age=31536000, immutable")
		c.Data(http.StatusOK, "application/javascript; charset=UTF-8", []byte("console.log(1)"))
	})
	r.GET("/api", func(c *gin.Context) {
		c.Header("Cache-Control", "private, no-store")
		c.JSON(http.StatusOK, gin.H{"data": "ok"})
	})
	r.GET("/text", func(c *gin.Context) { c.String(http.StatusNotFound, "404 page not found") })

	for path, want := range map[string]string{
		"/page":      "private, no-cache, no-store, max-age=0, must-revalidate, no-transform",
		"/bare":      "no-transform",
		"/marked":    "no-cache, No-Transform",
		"/missing":   "no-store, no-transform",
		"/redirect":  "no-store, no-transform",
		"/file.html": "no-cache, no-transform",
		"/app.js":    "public, max-age=31536000, immutable",
		"/api":       "private, no-store",
		"/text":      "",
	} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if got := rec.Header().Get("Cache-Control"); got != want {
			t.Errorf("%s: Cache-Control = %q, want %q", path, got, want)
		}
	}
}
