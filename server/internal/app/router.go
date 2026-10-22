// Package app assembles the processes from their parts; wire generates the constructors in wire_gen.go.
package app

import (
	"log/slog"

	"github.com/gin-gonic/gin"

	"github.com/yldm-tech/forma/server/internal/apikey"
	"github.com/yldm-tech/forma/server/internal/auth"
	"github.com/yldm-tech/forma/server/internal/authz"
	"github.com/yldm-tech/forma/server/internal/health"
	"github.com/yldm-tech/forma/server/internal/invite"
	"github.com/yldm-tech/forma/server/internal/org"
	"github.com/yldm-tech/forma/server/internal/platform/audit"
	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/server/internal/platform/httpserver"
	"github.com/yldm-tech/forma/server/internal/platform/mailer"
	"github.com/yldm-tech/forma/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/server/internal/survey"
	"github.com/yldm-tech/forma/server/internal/team"
	"github.com/yldm-tech/forma/server/internal/webui"
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

// NewRouter mounts every feature on one engine. The SPA goes last because it owns NoRoute.
func NewRouter(p *Platform, healthHandler *health.Handler, authHandler *auth.Handler, ssoHandler *auth.SSOHandler, orgHandler *org.Handler, workspaceHandler *workspace.Handler, teamHandler *team.Handler, inviteHandler *invite.Handler, apikeyHandler *apikey.Handler, surveyHandler *survey.Handler, web *webui.Handler) *gin.Engine {
	r := httpserver.NewEngine(p.Log)
	r.Use(p.ClientIP.Middleware())
	healthHandler.Register(r)
	mountAppAPI(r, p, authHandler, ssoHandler, orgHandler, workspaceHandler, teamHandler, inviteHandler, apikeyHandler, surveyHandler)
	// The v3 management API answers with RFC 9457 problems and authenticates by session or API key, outside /api/app and its envelope.
	surveyHandler.RegisterV3(r)
	// The identity providers' redirect URIs: browser navigations that end in a redirect, outside /api/app and its JSON envelope.
	ssoHandler.RegisterCallbacks(r)
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
