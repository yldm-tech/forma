// Package webui serves the admin SPA that `pnpm --filter @forma/web build` writes into dist/.
package webui

import (
	"embed"
	"errors"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/gin-gonic/gin"
)

//go:embed all:dist
var embedded embed.FS

// Handler serves files from the SPA build, falling back to index.html so client-side routes survive a reload.
type Handler struct {
	files fs.FS
	index []byte
}

// New uses the build embedded at compile time. A binary compiled without a web build still starts; it answers every page with 404 and keeps the API working.
func New() (*Handler, error) {
	sub, err := fs.Sub(embedded, "dist")
	if err != nil {
		return nil, err
	}
	return NewFromFS(sub)
}

func NewFromFS(files fs.FS) (*Handler, error) {
	index, err := fs.ReadFile(files, "index.html")
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	return &Handler{files: files, index: index}, nil
}

// Register installs the handler for every route nothing else matched.
func (h *Handler) Register(r *gin.Engine) {
	r.NoRoute(h.serve)
}

func (h *Handler) serve(c *gin.Context) {
	p := c.Request.URL.Path
	// API and probe paths never fall back to the SPA: a client calling a missing endpoint must get a 404, not an HTML page with status 200.
	if strings.HasPrefix(p, "/api/") || p == "/api" || strings.HasPrefix(p, "/health") {
		c.JSON(http.StatusNotFound, gin.H{"code": "not_found", "message": "Route not found"})
		return
	}
	if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
		c.Status(http.StatusMethodNotAllowed)
		return
	}

	name := strings.TrimPrefix(path.Clean(p), "/")
	if name != "" && name != "index.html" {
		if info, err := fs.Stat(h.files, name); err == nil && !info.IsDir() {
			switch {
			// Vite fingerprints everything under assets/, so those files never change under the same name.
			case strings.HasPrefix(name, "assets/"):
				c.Header("Cache-Control", "public, max-age=31536000, immutable")
			// The SDK and survey runtime bundles keep fixed names because customer pages embed them, so browsers revalidate hourly while a CDN may hold them for 30 days. Same headers forma2 sent.
			case strings.HasPrefix(name, "js/"):
				c.Header("Cache-Control", "public, max-age=3600, s-maxage=2592000, stale-while-revalidate=3600, stale-if-error=86400")
				c.Header("Access-Control-Allow-Origin", "*")
				c.Header("Vary", "Accept-Encoding")
				// Go has no MIME type for .cjs and would sniff text/plain, which browsers refuse to execute.
				if ext := path.Ext(name); ext == ".js" || ext == ".cjs" {
					c.Header("Content-Type", "application/javascript; charset=utf-8")
				}
			default:
				c.Header("Cache-Control", "no-cache")
			}
			c.FileFromFS("/"+name, http.FS(h.files))
			return
		}
		// A missing fingerprinted asset is a stale deploy, not a client route; returning index.html would make the browser parse HTML as JavaScript.
		if path.Ext(name) != "" {
			c.Status(http.StatusNotFound)
			return
		}
	}

	if h.index == nil {
		c.String(http.StatusNotFound, "web UI is not built into this binary")
		return
	}
	c.Header("Cache-Control", "no-cache")
	c.Data(http.StatusOK, "text/html; charset=utf-8", h.index)
}
