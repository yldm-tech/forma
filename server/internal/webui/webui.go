// Package webui serves the admin SPA that `pnpm --filter @forma/web build` writes into dist/.
package webui

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/yldm-tech/forma/server/internal/platform/analytics"
	"github.com/yldm-tech/forma/server/internal/platform/buildinfo"
	"github.com/yldm-tech/forma/server/internal/platform/config"
)

//go:embed all:dist
var embedded embed.FS

// Handler serves files from the SPA build, falling back to index.html so client-side routes survive a reload.
type Handler struct {
	files fs.FS
	index []byte
	// survey is the survey page bundle's HTML, which the public survey pages fill in; it is never served as a file.
	survey []byte
}

// surveyShell is the survey page bundle's entry in the build (web/survey.html).
const surveyShell = "survey.html"

// publicContentTypes are the types of the static files in web/public that Go's built-in table does not know, set explicitly because the distroless image has no system MIME database to fall back on; forma2 sent text/csv for its CSV templates and image/svg+xml for every SVG.
var publicContentTypes = map[string]string{
	".mp4":         "video/mp4",
	".ico":         "image/x-icon",
	".webmanifest": "application/manifest+json",
	".csv":         "text/csv",
	".svg":         "image/svg+xml",
}

// New uses the build embedded at compile time, with the browser's runtime configuration written into index.html. A binary compiled without a web build still starts; it answers every page with 404 and keeps the API working.
func New(cfg config.Config) (*Handler, error) {
	sub, err := fs.Sub(embedded, "dist")
	if err != nil {
		return nil, err
	}
	h, err := NewFromFS(sub)
	if err != nil {
		return nil, err
	}
	h.index = withRuntimeConfig(h.index, cfg)
	return h, nil
}

// sentryClientConfig is forma2's TSentryClientRuntimeConfig, read by the SPA from window.__formaSentryClientConfig.
type sentryClientConfig struct {
	DSN         string `json:"dsn"`
	Release     string `json:"release,omitempty"`
	Environment string `json:"environment,omitempty"`
}

// postHogClientConfig is what the SPA starts posthog-js with, from window.__formaPostHogConfig: the project key, the /ingest proxy as the API host and PostHog's EU app for links.
type postHogClientConfig struct {
	Key     string `json:"key"`
	APIHost string `json:"apiHost"`
	UIHost  string `json:"uiHost"`
}

// withRuntimeConfig writes the server-side settings the browser bundle needs into the page, as forma2's root layout did, so one build serves every deployment. Only what is configured is written: without SENTRY_DSN no DSN reaches the browser, without POSTHOG_KEY no key. JSON encoding escapes <, > and &, so no value can close the script element.
func withRuntimeConfig(index []byte, cfg config.Config) []byte {
	var script bytes.Buffer
	if dsn := cfg.Telemetry.SentryDSN; dsn != "" {
		c := sentryClientConfig{DSN: dsn, Environment: cfg.Telemetry.SentryEnvironment}
		if buildinfo.Released() {
			c.Release = buildinfo.Version
		}
		b, _ := json.Marshal(c)
		script.WriteString("window.__formaSentryClientConfig=" + string(b) + ";")
	}
	if key := cfg.Telemetry.PostHogKey; key != "" {
		b, _ := json.Marshal(postHogClientConfig{Key: key, APIHost: analytics.IngestPath, UIHost: analytics.Host})
		script.WriteString("window.__formaPostHogConfig=" + string(b) + ";")
	}
	head := bytes.Index(index, []byte("</head>"))
	if script.Len() == 0 || head < 0 {
		return index
	}
	out := make([]byte, 0, len(index)+script.Len()+64)
	out = append(out, index[:head]...)
	out = append(out, `<script id="forma-runtime-config">`...)
	out = append(out, script.Bytes()...)
	out = append(out, "</script>"...)
	return append(out, index[head:]...)
}

func NewFromFS(files fs.FS) (*Handler, error) {
	index, err := fs.ReadFile(files, "index.html")
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	survey, err := fs.ReadFile(files, surveyShell)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	return &Handler{files: files, index: index, survey: survey}, nil
}

// SurveyShell is the survey page bundle's HTML, or nil when the binary was built without the web UI.
func (h *Handler) SurveyShell() []byte {
	return h.survey
}

// Register installs the handler for every route nothing else matched.
func (h *Handler) Register(r *gin.Engine) {
	r.NoRoute(h.serve)
}

// ServePage answers a client route whose path another handler also mounts, as NoRoute would have.
func (h *Handler) ServePage(c *gin.Context) {
	h.serve(c)
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
	if name == surveyShell {
		c.Status(http.StatusNotFound)
		return
	}
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
				// Go has no MIME type for .cjs and would sniff text/plain, which browsers refuse to execute. The charset is spelled as forma2 sent it.
				if ext := path.Ext(name); ext == ".js" || ext == ".cjs" {
					c.Header("Content-Type", "application/javascript; charset=UTF-8")
				}
			// The favicons rarely change. Same headers forma2 sent (apps/web/next.config.mjs), as are the folders below.
			case name == "favicon.ico" || strings.HasPrefix(name, "favicon/"):
				c.Header("Cache-Control", "public, max-age=2592000, s-maxage=31536000, immutable")
				c.Header("Access-Control-Allow-Origin", "*")
			// Surveys and workspaces store these paths as their background; emails and customer pages load them cross-origin.
			case strings.HasPrefix(name, "image-backgrounds/"):
				c.Header("Cache-Control", "public, max-age=86400, s-maxage=2592000, stale-while-revalidate=86400")
				c.Header("Access-Control-Allow-Origin", "*")
				c.Header("Vary", "Accept-Encoding")
			case strings.HasPrefix(name, "animated-bgs/"):
				c.Header("Cache-Control", "public, max-age=604800, s-maxage=31536000, immutable")
				c.Header("Access-Control-Allow-Origin", "*")
			case strings.HasPrefix(name, "sample-csv/"):
				c.Header("Cache-Control", "public, max-age=3600, s-maxage=86400, stale-while-revalidate=3600")
				c.Header("Access-Control-Allow-Origin", "*")
			default:
				c.Header("Cache-Control", "no-cache")
			}
			if ct, ok := publicContentTypes[path.Ext(name)]; ok {
				c.Header("Content-Type", ct)
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
