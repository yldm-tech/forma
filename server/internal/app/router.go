// Package app assembles the processes from their parts; wire generates the constructors in wire_gen.go.
package app

import (
	"log/slog"

	"github.com/gin-gonic/gin"

	"github.com/yldm-tech/forma/server/internal/actionclass"
	"github.com/yldm-tech/forma/server/internal/ai"
	"github.com/yldm-tech/forma/server/internal/analysis"
	"github.com/yldm-tech/forma/server/internal/apikey"
	"github.com/yldm-tech/forma/server/internal/apiv3"
	"github.com/yldm-tech/forma/server/internal/auth"
	"github.com/yldm-tech/forma/server/internal/authz"
	"github.com/yldm-tech/forma/server/internal/billing"
	"github.com/yldm-tech/forma/server/internal/clientapi"
	"github.com/yldm-tech/forma/server/internal/contact"
	"github.com/yldm-tech/forma/server/internal/health"
	"github.com/yldm-tech/forma/server/internal/integration"
	"github.com/yldm-tech/forma/server/internal/invite"
	"github.com/yldm-tech/forma/server/internal/linksurvey"
	"github.com/yldm-tech/forma/server/internal/mcp"
	"github.com/yldm-tech/forma/server/internal/mgmtapi"
	"github.com/yldm-tech/forma/server/internal/notification"
	"github.com/yldm-tech/forma/server/internal/oauthserver"
	"github.com/yldm-tech/forma/server/internal/org"
	"github.com/yldm-tech/forma/server/internal/platform/audit"
	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/server/internal/platform/httpserver"
	"github.com/yldm-tech/forma/server/internal/platform/mailer"
	"github.com/yldm-tech/forma/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/server/internal/response"
	"github.com/yldm-tech/forma/server/internal/storage"
	"github.com/yldm-tech/forma/server/internal/survey"
	"github.com/yldm-tech/forma/server/internal/tag"
	"github.com/yldm-tech/forma/server/internal/team"
	"github.com/yldm-tech/forma/server/internal/v3api"
	"github.com/yldm-tech/forma/server/internal/webhook"
	"github.com/yldm-tech/forma/server/internal/webui"
	"github.com/yldm-tech/forma/server/internal/workflow"
	"github.com/yldm-tech/forma/server/internal/workspace"
)

// Platform is the shared infrastructure every feature module is built from: configuration, logging, the client address resolver, rate limits, mail, audit events and authorization. Modules take the parts they need as constructor arguments; NewRouter takes the bundle.
type Platform struct {
	Config   config.Config
	Log      *slog.Logger
	ClientIP *httpapi.ClientIPResolver
	Limiter  *ratelimit.Limiter
	Mailer   *mailer.Mailer
	Audit    *audit.Emitter
	Authz    *authz.Authorizer
}

// routeRegistrar is a feature module's HTTP handler.
type routeRegistrar interface {
	Register(r gin.IRouter)
}

// v3Registrar is a feature module's /api/v3 handler.
type v3Registrar interface {
	RegisterV3(r gin.IRouter)
}

// NewRouter mounts every feature on one engine. The SPA goes last because it owns NoRoute.
func NewRouter(p *Platform, sessions httpapi.SessionAuth, apiKeys *apikey.Authenticator, healthHandler *health.Handler, authHandler *auth.Handler, ssoHandler *auth.SSOHandler, orgHandler *org.Handler, workspaceHandler *workspace.Handler, teamHandler *team.Handler, inviteHandler *invite.Handler, apikeyHandler *apikey.Handler, surveyHandler *survey.Handler, contactHandler *contact.Handler, contactV3Handler *contact.V3Handler, actionClassHandler *actionclass.Handler, tagHandler *tag.Handler, responseHandler *response.Handler, analysisHandler *analysis.Handler, aiHandler *ai.Handler, storageHandler *storage.Handler, linkSurveyHandler *linksurvey.Handler, clientAPIHandler *clientapi.Handler, webhookHandler *webhook.Handler, webhookV1Handler *webhook.V1Handler, managementHandler *mgmtapi.Handler, integrationHandler *integration.Handler, integrationOAuthHandler *integration.OAuthHandler, notificationHandler *notification.Handler, attachmentsHandler *analysis.AttachmentsHandler, workflowHandler *workflow.Handler, oauthHandler *oauthserver.Handler, oauthAppHandler *oauthserver.AppHandler, mcpHandler *mcp.Handler, billingHandler *billing.Handler, stripeWebhookHandler *billing.WebhookHandler, web *webui.Handler) *gin.Engine {
	r := httpserver.NewEngine(p.Log)
	r.Use(httpserver.PublicDomainGate(p.Config), p.ClientIP.Middleware())
	healthHandler.Register(r)
	// The public survey pages and their OG image, served on PUBLIC_URL when it is set.
	linkSurveyHandler.Register(r)
	// The client API the SDK and the survey runtime call: forma2's paths, CORS and answers, outside /api/app.
	clientAPIHandler.Register(r)
	// The webhook subscription API Zapier, Make, n8n and Activepieces call with an API key: forma2's paths and v1 envelope, outside /api/app.
	webhookV1Handler.Register(r)
	// The management APIs integrations and customers' backends call with an API key, /api/v1/management and /api/v2: forma2's paths and envelopes, outside /api/app.
	managementHandler.Register(r)
	// The Stripe webhook at the path registered in Stripe's dashboard, authenticated by its signature: forma2's path and answers, outside /api/app.
	stripeWebhookHandler.Register(r)
	mountAppAPI(r, p, authHandler, ssoHandler, orgHandler, workspaceHandler, teamHandler, inviteHandler, apikeyHandler, surveyHandler, contactHandler, actionClassHandler, tagHandler, responseHandler, analysisHandler, aiHandler, webhookHandler, integrationHandler, workflowHandler, oauthAppHandler, billingHandler)
	// The v3 management API answers with RFC 9457 problems and authenticates by session or API key, outside /api/app and its envelope. Its modules were built on three middleware stacks, each mounted on its own /api/v3 group: the guard of the survey and workflow modules, the contact module's group-wide authentication, and the per-route v3api middleware of action classes and tags.
	surveyHandler.RegisterV3(r)
	workflowHandler.RegisterV3(r)
	mountAuthenticatedV3API(r, p, sessions, apiKeys, contactV3Handler)
	mountV3API(r, p, sessions, actionClassHandler, tagHandler)
	// forma2's internal routes outside /api/v3 that follow its conventions without its contract: the attachment ZIP export.
	mountInternalAPI(r, p, sessions, attachmentsHandler)
	// The identity providers' redirect URIs: browser navigations that end in a redirect, outside /api/app and its JSON envelope.
	ssoHandler.RegisterCallbacks(r)
	// The MCP server at forma2's /api/mcp, and the OAuth authorization server MCP clients discover through /.well-known and sign in through under /api/auth/oauth2: forma2's paths and answers, outside /api/app.
	mcpHandler.Register(r)
	oauthHandler.Register(r)
	// The integrations' OAuth authorize routes and the callbacks registered with Google, Airtable, Notion and Slack: forma2's paths and v1 envelope, outside /api/app.
	integrationOAuthHandler.Register(r)
	// Stored files, at the URLs surveys, responses and emails embed: forma2's paths and answers, outside /api/app.
	storageHandler.Register(r)
	// The unsubscribe and view-responses links of the response-finished email, which emails already sent carry.
	notificationHandler.Register(r)
	web.Register(r)
	return r
}

// mountAppAPI mounts /api/app, the session-authenticated API only the admin SPA calls. Every route under it answers errors with the JSON envelope, every mutation must pass the CSRF check, and each request evaluates a repeated authorization question once. Feature modules' handlers are passed here as they land.
func mountAppAPI(r *gin.Engine, p *Platform, handlers ...routeRegistrar) {
	group := r.Group("/api/app", httpapi.ErrorHandler(p.Log), httpapi.CSRF(p.Config), authz.DecisionMemo())
	for _, h := range handlers {
		h.Register(group)
	}
}

// mountAuthenticatedV3API mounts /api/v3 routes whose handlers rely on the group to authenticate every request. Every route under it answers failures as RFC 9457 problems, authenticates by session or API key, and charges forma2's api.v3 rate limit to the caller.
func mountAuthenticatedV3API(r *gin.Engine, p *Platform, sessions httpapi.SessionAuth, apiKeys *apikey.Authenticator, handlers ...routeRegistrar) {
	middleware := append([]gin.HandlerFunc{apiv3.ErrorHandler(p.Log), authz.DecisionMemo()}, apiv3.Authenticate(sessions, apiKeys, p.Limiter)...)
	group := r.Group("/api/v3", middleware...)
	for _, h := range handlers {
		h.Register(group)
	}
}

// mountV3API mounts /api/v3, the management API specified in docs/api-v3-reference. Errors are RFC 9457 problems, a signed-in browser's session is read for every route (each route then decides with v3api.Middleware.Auth whether it also accepts an API key), and each request evaluates a repeated authorization question once.
func mountV3API(r *gin.Engine, p *Platform, sessions httpapi.SessionAuth, handlers ...v3Registrar) {
	group := r.Group("/api/v3", v3api.ErrorHandler(p.Log), authz.DecisionMemo(), sessions.Optional())
	for _, h := range handlers {
		h.RegisterV3(group)
	}
}

// mountInternalAPI mounts the /api routes forma2 kept under (internal): v3's problems and per-route authentication, no published contract.
func mountInternalAPI(r *gin.Engine, p *Platform, sessions httpapi.SessionAuth, handlers ...routeRegistrar) {
	group := r.Group("/api", v3api.ErrorHandler(p.Log), authz.DecisionMemo(), sessions.Optional())
	for _, h := range handlers {
		h.Register(group)
	}
}
