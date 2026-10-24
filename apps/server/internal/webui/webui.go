// Package webui serves the admin SPA that `pnpm --filter @forma/web build` writes into dist/. The SPA is built for config.AppBasePath (/app) and served only under it; the files of apps/web/public that surveys, emails and customer sites load keep their root URLs, the admin pages that lived at the root before answer with a permanent redirect to the same path under /app, and the root itself leads to the app.
package webui

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"path"
	"slices"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/yldm-tech/forma/apps/server/internal/platform/analytics"
	"github.com/yldm-tech/forma/apps/server/internal/platform/buildinfo"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpserver"
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

// surveyShell is the survey page bundle's entry in the build (apps/web/survey.html).
const surveyShell = "survey.html"

// RootPublicPaths are the first path segments of the build's files that are served at their root URL as well as under /app: the files of apps/web/public that something outside the SPA loads by a root path. Surveys and workspaces store /image-backgrounds and /animated-bgs URLs as their backgrounds, the survey runtime and emails load /smiley-icons and /star-icons, forma2's emails embed /logo-transparent.png, customer sites load the SDK from /js, browsers ask for /favicon.ico, and the organization invite template is a download link at /sample-csv. A new top-level entry in apps/web/public needs a decision here (TestRootPublicPathsCoverThePublicDirectory) and, when it is listed, in the ingress that routes these paths to the server.
var RootPublicPaths = []string{"animated-bgs", "favicon", "favicon.ico", "image-backgrounds", "js", "logo-transparent.png", "sample-csv", "smiley-icons", "star-icons"}

// LegacyAdminPaths are the first path segments of the admin pages that were served at the root, by forma2 and by this SPA before it moved under /app. Links to them were mailed, bookmarked and registered as return URLs, so each answers with a permanent redirect (308) to the same path and query under /app.
var LegacyAdminPaths = []string{"account", "auth", "billing-confirmation", "email-change-without-verification-success", "environments", "i", "invite", "organizations", "projects", "setup", "verify-email-change", "workspaces"}

// publicContentTypes are the types of the static files in apps/web/public that Go's built-in table does not know, set explicitly because the distroless image has no system MIME database to fall back on; forma2 sent text/csv for its CSV templates and image/svg+xml for every SVG.
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
	if rest, ok := strings.CutPrefix(p, config.AppBasePath); ok && (rest == "" || strings.HasPrefix(rest, "/")) {
		h.serveApp(c, rest)
		return
	}
	clean := path.Clean(p)
	first, _, _ := strings.Cut(strings.TrimPrefix(clean, "/"), "/")
	switch {
	case slices.Contains(LegacyAdminPaths, first):
		redirectToApp(c, http.StatusPermanentRedirect, c.Request.URL.EscapedPath())
	case clean == "/":
		// Temporary: a deployment that puts a website at the root later must not find browsers holding a cached redirect.
		redirectToApp(c, http.StatusFound, "/")
	case slices.Contains(RootPublicPaths, first):
		if !allowedMethod(c) {
			return
		}
		if !h.serveFile(c, strings.TrimPrefix(clean, "/")) {
			c.Status(http.StatusNotFound)
		}
	default:
		c.String(http.StatusNotFound, "404 page not found")
	}
}

// redirectToApp sends the request to target under the app's base path, the query kept.
func redirectToApp(c *gin.Context, status int, target string) {
	target = config.AppBasePath + target
	if q := c.Request.URL.RawQuery; q != "" {
		target += "?" + q
	}
	c.Header("Cache-Control", "no-store")
	c.Redirect(status, target)
}

func allowedMethod(c *gin.Context) bool {
	if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
		c.Status(http.StatusMethodNotAllowed)
		return false
	}
	return true
}

// serveApp answers a path under the app's base path, given without it: a file of the build, or index.html for a client route.
func (h *Handler) serveApp(c *gin.Context, rest string) {
	if !allowedMethod(c) {
		return
	}
	name := strings.TrimPrefix(path.Clean("/"+rest), "/")
	if name != "" && name != "index.html" {
		if h.serveFile(c, name) {
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
	// The SPA converts HEIC images with WebAssembly.
	httpserver.AllowWebAssembly(c)
	c.Data(http.StatusOK, "text/html; charset=utf-8", h.index)
}

// serveFile writes the build's file name with the headers its folder gets, and reports whether there was one. The survey page bundle's HTML is a template, never a file.
func (h *Handler) serveFile(c *gin.Context, name string) bool {
	if name == surveyShell {
		return false
	}
	info, err := fs.Stat(h.files, name)
	if err != nil || info.IsDir() {
		return false
	}
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
	return true
}
