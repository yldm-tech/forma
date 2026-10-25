// Package webui serves the admin SPA that `pnpm --filter @forma/web build` writes into dist/. The SPA is built for config.AppBasePath (/app) and served only under it; the files of apps/web/public that surveys, emails and customer sites load keep their root URLs, and the root itself leads to the app.
package webui

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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
	// hashes are the content hashes of the files under js/, by name: their ETag, and the version a page can pin with versionParam.
	hashes map[string]string
	// surveyLocales are the survey runtime's locale files (js/surveys-locales/<tag>.<hash>.json, written by the packages/surveys build) by language tag.
	surveyLocales map[string]string
}

// surveyShell is the survey page bundle's entry in the build (apps/web/survey.html).
const surveyShell = "survey.html"

// RootPublicPaths are the first path segments of the build's files that are served at their root URL as well as under /app: the files of apps/web/public that something outside the SPA loads by a root path. Surveys and workspaces store /image-backgrounds and /animated-bgs URLs as their backgrounds, the survey runtime and emails load /smiley-icons and /star-icons, emails embed /logo-transparent.png, customer sites load the SDK from /js, browsers ask for /favicon.ico, and the organization invite template is a download link at /sample-csv. A new top-level entry in apps/web/public needs a decision here (TestRootPublicPathsCoverThePublicDirectory) and, when it is listed, in the ingress that routes these paths to the server.
var RootPublicPaths = []string{"animated-bgs", "favicon", "favicon.ico", "image-backgrounds", "js", "logo-transparent.png", "sample-csv", "smiley-icons", "star-icons"}

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
	hashes, err := hashSDKFiles(files)
	if err != nil {
		return nil, err
	}
	return &Handler{files: files, index: index, survey: survey, hashes: hashes, surveyLocales: surveyLocaleFiles(hashes)}, nil
}

// sdkDir is the folder of the SDK and survey runtime bundles, served at /js.
const sdkDir = "js"

// surveyLocaleDir holds the survey runtime's translations other than English, which the runtime fetches from beside its own script; their names carry a hash of their content (packages/surveys/vite.config.mts).
const surveyLocaleDir = sdkDir + "/surveys-locales/"

// versionParam is the query parameter that pins a URL under /js to one content hash (VersionedPath). The name is not "v", which customers may already append to the embed snippets as a cache buster.
const versionParam = "hash"

// hashSDKFiles hashes every file under js/ once, at start: the embedded build never changes while the process runs. A build without js/ has none.
func hashSDKFiles(files fs.FS) (map[string]string, error) {
	hashes := map[string]string{}
	err := fs.WalkDir(files, sdkDir, func(name string, d fs.DirEntry, err error) error {
		if errors.Is(err, fs.ErrNotExist) && name == sdkDir {
			return fs.SkipAll
		}
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(files, name)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		hashes[name] = hex.EncodeToString(sum[:8])
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("hash %s: %w", sdkDir, err)
	}
	return hashes, nil
}

// surveyLocaleFiles picks the survey runtime's locale files out of the hashed names: the language tag is the file name up to its first dot.
func surveyLocaleFiles(hashes map[string]string) map[string]string {
	locales := map[string]string{}
	for name := range hashes {
		file, ok := strings.CutPrefix(name, surveyLocaleDir)
		if !ok || strings.Contains(file, "/") || path.Ext(file) != ".json" {
			continue
		}
		tag, _, _ := strings.Cut(file, ".")
		locales[tag] = "/" + name
	}
	return locales
}

// currentSurveyLocale is this build's locale file for the language of a locale file name the build does not have. The runtime names them by content hash (js/surveys-locales/<tag>.<hash>.json), so a runtime that a browser or CDN cached under its fixed name from an earlier build asks for hashes this build never had; answering those with 404 would render every language but English in English until that cache expired, up to the CDN's 30 days. False for any other name and for a language this build does not ship.
func (h *Handler) currentSurveyLocale(name string) (string, bool) {
	file, ok := strings.CutPrefix(name, surveyLocaleDir)
	if !ok || strings.Contains(file, "/") || path.Ext(file) != ".json" {
		return "", false
	}
	tag, _, _ := strings.Cut(file, ".")
	current, ok := h.surveyLocales[tag]
	return strings.TrimPrefix(current, "/"), ok
}

// VersionedPath is the root URL path of the build's file name under js/, pinned to its content: a response for it is cached as immutable, so a page that names it this way never revalidates it. A file the build does not have keeps its plain path.
func (h *Handler) VersionedPath(name string) string {
	hash, ok := h.hashes[name]
	if !ok {
		return "/" + name
	}
	return "/" + name + "?" + versionParam + "=" + hash
}

// SurveyLocalePaths are the root URL paths of the survey runtime's translations, by the language tag of each shipped bundle (de-DE, zh-Hant-TW). English is not among them: the runtime carries it. The map is shared; callers only read it.
func (h *Handler) SurveyLocalePaths() map[string]string {
	return h.surveyLocales
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
	case clean == "/":
		// Temporary: a deployment that puts a website at the root later must not find browsers holding a cached redirect.
		redirectToApp(c)
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

// redirectToApp sends a request for the root to the app's base path, the query kept.
func redirectToApp(c *gin.Context) {
	target := config.AppBasePath + "/"
	if q := c.Request.URL.RawQuery; q != "" {
		target += "?" + q
	}
	c.Header("Cache-Control", "no-store")
	c.Redirect(http.StatusFound, target)
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
	stale := false
	if err != nil || info.IsDir() {
		current, ok := h.currentSurveyLocale(name)
		if !ok {
			return false
		}
		name, stale = current, true
	}
	switch {
	// Vite fingerprints everything under assets/, so those files never change under the same name.
	case strings.HasPrefix(name, "assets/"):
		c.Header("Cache-Control", "public, max-age=31536000, immutable")
	// The SDK and survey runtime bundles keep fixed names because customer pages embed them, so browsers revalidate hourly while a CDN may hold them for 30 days. Same headers forma2 sent. The ETag, a hash of the content, lets that hourly revalidation end in a 304 instead of the whole bundle; it is weak because the server gzips the body. A URL pinned to the content (VersionedPath, the public survey pages) never needs revalidating, and the survey runtime's locale files are named by their content. A URL naming other content than this build's comes from another build: a pinned runtime URL on a page rendered by a server with another build during a deploy, or a locale file by the hash a runtime cached from an earlier build knows it by (currentSurveyLocale). It is answered with what this build has, never stored, so no cache keeps that content under the other version.
	case strings.HasPrefix(name, sdkDir+"/"):
		hash := h.hashes[name]
		switch version, pinned := c.GetQuery(versionParam); {
		case stale, pinned && version != hash:
			c.Header("Cache-Control", "no-store")
		case strings.HasPrefix(name, surveyLocaleDir), pinned:
			c.Header("Cache-Control", "public, max-age=31536000, immutable")
		default:
			c.Header("Cache-Control", "public, max-age=3600, s-maxage=2592000, stale-while-revalidate=3600, stale-if-error=86400")
		}
		c.Header("ETag", `W/"`+hash+`"`)
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Vary", "Accept-Encoding")
		// Go has no MIME type for .cjs and would sniff text/plain, which browsers refuse to execute. The charset is spelled as forma2 sent it.
		if ext := path.Ext(name); ext == ".js" || ext == ".cjs" {
			c.Header("Content-Type", "application/javascript; charset=UTF-8")
		}
	// The favicons keep fixed names (browsers request /favicon.ico on their own), so they must not be cached as immutable: forma2's "s-maxage=31536000, immutable" kept the old icon at the CDN after the 2026-10-25 rebrand. A day at most, served stale while it revalidates; index.html also links them with a ?v= query to switch at once. The folders below keep forma2's headers.
	case name == "favicon.ico" || strings.HasPrefix(name, "favicon/"):
		c.Header("Cache-Control", "public, max-age=86400, s-maxage=86400, stale-while-revalidate=86400")
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
