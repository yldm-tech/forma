package linksurvey

import (
	"encoding/json"
	"html"
	"log/slog"
	"maps"
	"net/http"
	"slices"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpserver"
	"github.com/yldm-tech/forma/apps/server/internal/platform/mailer"
	"github.com/yldm-tech/forma/apps/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/userinput"
	"github.com/yldm-tech/forma/apps/server/internal/webui"
)

// Headers forma2's Next.js sent with the survey pages and the client API, beyond the security headers every response gets (httpserver).
const (
	pageCacheControl = "private, no-cache, no-store, max-age=0, must-revalidate"
	pageVary         = "rsc, next-router-state-tree, next-router-prefetch, next-router-segment-prefetch, Accept-Encoding"
	routeVary        = "rsc, next-router-state-tree, next-router-prefetch, next-router-segment-prefetch"
	ogCacheControl   = "public, s-maxage=600, max-age=1800, stale-while-revalidate=600, stale-if-error=600"
	// The CORS headers next.config.mjs set on every /api/(v1|v2)/client response; they mask whatever a route set itself.
	clientAllowMethods = "GET,OPTIONS,PATCH,DELETE,POST,PUT"
	clientAllowHeaders = "X-CSRF-Token, X-Requested-With, Accept, Accept-Version, Content-Length, Content-MD5, Content-Type, Date, X-Api-Version, Cache-Control"
)

// surveysScript is the respondent runtime the survey page renders with, a file of the web build; the app loads the .js copy because CDNs do not cache .cjs.
const surveysScript = "js/surveys.umd.js"

// Handler serves the public survey pages, their gate endpoints and the OG image.
type Handler struct {
	cfg     config.Config
	svc     *Service
	limiter *ratelimit.Limiter
	shell   shell
	log     *slog.Logger
	// scriptPath is the runtime's URL path pinned to the build's content, so a respondent's browser and the CDN keep it until a deploy changes it instead of revalidating it hourly (webui.VersionedPath).
	scriptPath string
	// localePaths are the runtime's translation files by language tag (webui.SurveyLocalePaths).
	localePaths map[string]string
	// policy is the pages' Content-Security-Policy (pagePolicy).
	policy string
}

func NewHandler(cfg config.Config, svc *Service, limiter *ratelimit.Limiter, web *webui.Handler, log *slog.Logger) *Handler {
	return &Handler{cfg: cfg, svc: svc, limiter: limiter, shell: newShell(web.SurveyShell()), log: log, scriptPath: web.VersionedPath(surveysScript), localePaths: web.SurveyLocalePaths(), policy: pagePolicy(cfg)}
}

// Script sources a survey page loads beside the page bundle: Cal.com's embed for a scheduling question, and Google reCAPTCHA (api.js, which loads its release from gstatic) where RECAPTCHA_* lets a survey turn on spam protection. Paths are part of the sources, so the rest of www.google.com stays out.
const calEmbedScript = "https://cal.com/embed.js"

var recaptchaScripts = []string{"https://www.google.com/recaptcha/", "https://www.gstatic.com/recaptcha/"}

// pagePolicy is the survey pages' Content-Security-Policy (httpserver.DocumentPolicy): the page bundle from its own origin, the respondent runtime from the public domain the page names it on (PUBLIC_URL, else WEBAPP_URL), and the scripts above. A page that runs custom head scripts keeps the policy every response gets (page.CustomScripts).
func pagePolicy(cfg config.Config) string {
	var sources []string
	domain := cfg.WebAppURL
	if cfg.PublicURL != nil {
		domain = cfg.PublicURL
	}
	if domain != nil && domain.Host != "" {
		sources = append(sources, domain.Scheme+"://"+domain.Host)
	}
	sources = append(sources, calEmbedScript)
	if cfg.RecaptchaConfigured() {
		sources = append(sources, recaptchaScripts...)
	}
	return httpserver.DocumentPolicy(cfg, sources...)
}

// Register mounts the pages and the OG image. The PIN and email verification endpoints live under the survey's own path, where forma2's server actions were posted, so the public domain serves them with the page.
func (h *Handler) Register(r gin.IRouter) {
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		r.Handle(method, "/s/:surveyId", h.surveyPage)
		r.Handle(method, "/c/:token", h.contactPage)
		r.Handle(method, "/p/:slug", h.prettyURL)
		r.Handle(method, "/api/v1/client/og", h.ogImage)
		for _, page := range []string{"/s/:surveyId", "/c/:token", "/p/:slug"} {
			r.Handle(method, page+"/*rest", h.subpath)
		}
	}
	r.OPTIONS("/api/v1/client/og", h.ogOptions)
	gates := r.Group("/s/:surveyId", httpapi.ErrorHandler(h.log), httpapi.CSRF(h.cfg))
	gates.POST("/pin", h.validatePin)
	gates.POST("/pin/resume", h.resumePin)
	gates.POST("/verify-email", h.sendVerificationEmail)
}

func (h *Handler) surveyPage(c *gin.Context) {
	q := query{values: c.Request.URL.Query(), raw: c.Request.URL.RawQuery}
	p, err := h.svc.surveyPage(c.Request.Context(), c.Param("surveyId"), q, c.GetHeader("Accept-Language"))
	h.writePage(c, p, err, linkViewport)
}

func (h *Handler) contactPage(c *gin.Context) {
	q := query{values: c.Request.URL.Query(), raw: c.Request.URL.RawQuery}
	p, err := h.svc.contactPage(c.Request.Context(), c.Param("token"), q, c.GetHeader("Accept-Language"))
	h.writePage(c, p, err, rootViewport)
}

// prettyURL redirects to the survey's /s page with Next's redirect status, 307; an unknown slug is the survey not-found page.
func (h *Handler) prettyURL(c *gin.Context) {
	target, ok, err := h.svc.prettyURL(c.Request.Context(), c.Param("slug"), c.Request.URL.RawQuery)
	if err != nil {
		h.writePage(c, page{}, err, rootViewport)
		return
	}
	if !ok {
		h.writePage(c, page{Status: http.StatusNotFound, Head: notFoundHead(rootViewport), State: pageState{View: viewNotFound, Locale: localeOf(c)}}, nil, rootViewport)
		return
	}
	c.Header("Cache-Control", pageCacheControl)
	c.Redirect(http.StatusTemporaryRedirect, target)
}

// subpath answers a path below a survey page. forma2 had no route there, so Next answered its not-found page with 404, and with a 308 to the page itself for a trailing slash. Without this route the admin SPA's fallback would answer with its own shell and 200, frameable by any site under /s and /c.
func (h *Handler) subpath(c *gin.Context) {
	if c.Param("rest") == "/" {
		target := strings.TrimSuffix(c.Request.URL.EscapedPath(), "/")
		if c.Request.URL.RawQuery != "" {
			target += "?" + c.Request.URL.RawQuery
		}
		c.Redirect(http.StatusPermanentRedirect, target)
		return
	}
	h.writePage(c, page{Status: http.StatusNotFound, Head: notFoundHead(rootViewport), State: pageState{View: viewNotFound, Locale: localeOf(c)}}, nil, rootViewport)
}

func localeOf(c *gin.Context) string {
	return mailer.MatchLocale(c.GetHeader("Accept-Language"))
}

// writePage renders a page into the survey page shell. An unexpected failure is logged here and rendered as the page bundle's error screen.
func (h *Handler) writePage(c *gin.Context, p page, err error, viewport string) {
	if err != nil {
		h.log.ErrorContext(c.Request.Context(), "render survey page", "request_id", httpserver.RequestID(c), "route", c.FullPath(), "err", err)
		p = page{Status: http.StatusInternalServerError, Head: head{Title: rootTitle, Viewport: viewport, Metas: []metaTag{{Name: "description", Content: rootDescription}}}, State: pageState{View: viewError, Locale: localeOf(c)}}
	}
	extra := ""
	if p.State.View == viewSurvey {
		// forma2 rendered the runtime's script tag into the document, so the browser fetches and runs it alongside the page bundle instead of after it.
		src := html.EscapeString(p.State.Survey.PublicDomain + h.scriptPath)
		// data-forma-survey-page tells the runtime it runs on this page, the one document where it may set zod jitless for the whole page (packages/surveys/src/lib/zod-jitless.ts): the runtime can run before the page bundle that sets it, and the policy here refuses the eval probe zod would otherwise make.
		extra = `<link rel="preload" href="` + src + `" as="script" fetchpriority="high"><script async src="` + src + `" data-forma-survey-page></script>`
		// The runtime carries only English and fetches the survey's language once it runs; fetching it alongside the runtime saves that round trip. crossorigin matches the runtime's fetch, a CORS request with same-origin credentials, so the browser reuses this response.
		if locale, ok := runtimeLocalePath(h.localePaths, p.RuntimeLanguage); ok {
			extra += `<link rel="preload" href="` + html.EscapeString(p.State.Survey.PublicDomain+locale) + `" as="fetch" crossorigin="anonymous">`
		}
	}
	doc, err := h.shell.render(p.State.Locale, p.Head.render()+extra, p.State)
	if err != nil {
		h.log.ErrorContext(c.Request.Context(), "encode survey page state", "err", err)
		c.Status(http.StatusInternalServerError)
		return
	}
	if !p.CustomScripts {
		httpserver.SetDocumentPolicy(c, h.policy)
	}
	c.Header("Cache-Control", pageCacheControl)
	c.Header("Vary", pageVary)
	c.Data(p.Status, "text/html; charset=utf-8", doc)
}

type pinRequest struct {
	Pin *string `json:"pin" binding:"required"`
}

// validatePin is forma2's validateSurveyPinAction, limited per client address.
func (h *Handler) validatePin(c *gin.Context) {
	var req pinRequest
	if err := httpapi.BindJSON(c, &req); err != nil {
		httpapi.Fail(c, err)
		return
	}
	if err := h.limiter.ApplyIP(c.Request.Context(), ratelimit.ActionValidateSurveyPin); err != nil {
		httpapi.Fail(c, err)
		return
	}
	res, err := h.svc.validatePin(c.Request.Context(), c.Param("surveyId"), *req.Pin)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, res)
}

type pinResumeRequest struct {
	PinAuthToken string `json:"pinAuthToken" binding:"required"`
}

// resumePin opens the PIN gate again with a PIN token the page kept (Service.resumePin). Not rate limited: the token is signed, so guessing one is not cheaper here than anywhere else, and a respondent reloading the page must not spend the PIN attempts.
func (h *Handler) resumePin(c *gin.Context) {
	var req pinResumeRequest
	if err := httpapi.BindJSON(c, &req); err != nil {
		httpapi.Fail(c, err)
		return
	}
	res, err := h.svc.resumePin(c.Request.Context(), c.Param("surveyId"), req.PinAuthToken)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, res)
}

// sendVerificationEmail is forma2's sendLinkSurveyEmailAction, limited per client address. The address is a single one (zod's email format refuses the commas, semicolons and angle brackets of an address list) and the locale one forma2 knew.
func (h *Handler) sendVerificationEmail(c *gin.Context) {
	var in emailInput
	if err := httpapi.BindJSON(c, &in); err != nil {
		httpapi.Fail(c, err)
		return
	}
	var fields []apperr.FieldError
	if !userinput.ValidEmailFormat(in.Email) || userinput.UTF16Len(in.Email) > userinput.EmailMaxLength {
		fields = append(fields, apperr.FieldError{Field: "email", Rule: "email"})
	}
	if !slices.Contains(userLocales, in.Locale) {
		fields = append(fields, apperr.FieldError{Field: "locale", Rule: "oneof", Param: strings.Join(userLocales, " ")})
	}
	if len(fields) > 0 {
		httpapi.Fail(c, apperr.Validation(fields))
		return
	}
	if err := h.limiter.ApplyIP(c.Request.Context(), ratelimit.ActionSendLinkSurvey); err != nil {
		httpapi.Fail(c, err)
		return
	}
	if err := h.svc.sendVerificationEmail(c.Request.Context(), c.Param("surveyId"), in); err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// clientAPIError is the client API's error body.
type clientAPIError struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details"`
}

func clientCORS(c *gin.Context) {
	c.Header("Access-Control-Allow-Credentials", "true")
	c.Header("Access-Control-Allow-Origin", "*")
	c.Header("Access-Control-Allow-Methods", clientAllowMethods)
	c.Header("Access-Control-Allow-Headers", clientAllowHeaders)
	c.Header("Vary", routeVary)
}

// ogImage is forma2's GET /api/v1/client/og. It is unauthenticated and every distinct query string rasterises a new image, so the client API's per-address limit is charged first.
func (h *Handler) ogImage(c *gin.Context) {
	clientCORS(c)
	res, err := h.limiter.Check(c.Request.Context(), ratelimit.APIClient, ratelimit.IPIdentifier(c.Request.Context()), 1)
	if err != nil || !res.Allowed {
		body := clientAPIError{Code: apperr.CodeTooManyRequests, Message: apperr.TooManyRequestsMessage, Details: map[string]any{}}
		status := http.StatusTooManyRequests
		if err != nil {
			h.log.ErrorContext(c.Request.Context(), "og image rate limit", "err", err)
			body = clientAPIError{Code: apperr.CodeInternal, Message: "Something went wrong. Please try again.", Details: map[string]any{}}
			status = http.StatusInternalServerError
		}
		writeClientJSON(c, status, body)
		return
	}
	img, err := renderOgImage(parseOgParams(c.Request.URL.Query()))
	if err != nil {
		h.log.ErrorContext(c.Request.Context(), "render og image", "err", err)
		writeClientJSON(c, http.StatusInternalServerError, clientAPIError{Code: apperr.CodeInternal, Message: "Something went wrong. Please try again.", Details: map[string]any{}})
		return
	}
	c.Header("Cache-Control", ogCacheControl)
	c.Data(http.StatusOK, "image/png", img)
}

// ogOptions is Next's automatic answer to OPTIONS on a route that exports only GET.
func (h *Handler) ogOptions(c *gin.Context) {
	clientCORS(c)
	c.Header("Allow", "GET, HEAD, OPTIONS")
	c.Status(http.StatusNoContent)
}

// writeClientJSON writes a client API body the way forma2's responses did: application/json without a charset, never cached.
func writeClientJSON(c *gin.Context, status int, body any) {
	b, err := json.Marshal(body)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	c.Header("Cache-Control", "private, no-store")
	c.Data(status, "application/json", b)
}

// runtimeLocalePath is the translation file the respondent runtime fetches for a survey language, chosen as i18next's getBestMatchFromCodes chooses the bundle the runtime switches to (packages/surveys/src/lib/i18n.config.ts): the bundle of that tag, case aside, else the first bundle, in tag order, of the same primary language (de-AT reads de-DE). None for English, which the runtime carries, or for a language it has no translations for.
func runtimeLocalePath(paths map[string]string, code string) (string, bool) {
	tags := slices.Sorted(maps.Keys(paths))
	for _, tag := range tags {
		if strings.EqualFold(tag, code) {
			return paths[tag], true
		}
	}
	primary, _, _ := strings.Cut(strings.ToLower(code), "-")
	if primary == "en" {
		return "", false
	}
	for _, tag := range tags {
		if strings.HasPrefix(strings.ToLower(tag), primary+"-") {
			return paths[tag], true
		}
	}
	return "", false
}
