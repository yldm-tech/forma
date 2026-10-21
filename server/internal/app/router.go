// Package app assembles the processes from their parts; wire generates the constructors in wire_gen.go.
package app

import (
	"log/slog"

	"github.com/gin-gonic/gin"

	"github.com/yldm-tech/forma/server/internal/health"
	"github.com/yldm-tech/forma/server/internal/platform/audit"
	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/server/internal/platform/httpserver"
	"github.com/yldm-tech/forma/server/internal/platform/mailer"
	"github.com/yldm-tech/forma/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/server/internal/webui"
)

// Platform is the shared infrastructure every feature module is built from: configuration, logging, the client address resolver, rate limits, mail and audit events. Modules take the parts they need as constructor arguments; NewRouter takes the bundle.
type Platform struct {
	Config   config.Config
	Log      *slog.Logger
	ClientIP *httpapi.ClientIPResolver
	Limiter  *ratelimit.Limiter
	Mailer   *mailer.Mailer
	Audit    *audit.Emitter
}

// routeRegistrar is a feature module's HTTP handler.
type routeRegistrar interface {
	Register(r gin.IRouter)
}

// NewRouter mounts every feature on one engine. The SPA goes last because it owns NoRoute.
func NewRouter(p *Platform, healthHandler *health.Handler, web *webui.Handler) *gin.Engine {
	r := httpserver.NewEngine(p.Log)
	r.Use(p.ClientIP.Middleware())
	healthHandler.Register(r)
	mountAppAPI(r, p)
	web.Register(r)
	return r
}

// mountAppAPI mounts /api/app, the session-authenticated API only the admin SPA calls. Every route under it answers errors with the JSON envelope, and every mutation must pass the CSRF check. Feature modules' handlers are passed here as they land.
func mountAppAPI(r *gin.Engine, p *Platform, handlers ...routeRegistrar) {
	group := r.Group("/api/app", httpapi.ErrorHandler(p.Log), httpapi.CSRF(p.Config))
	for _, h := range handlers {
		h.Register(group)
	}
}
