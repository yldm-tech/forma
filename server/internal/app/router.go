// Package app assembles the processes from their parts; wire generates the constructor in wire_gen.go.
package app

import (
	"log/slog"

	"github.com/gin-gonic/gin"

	"github.com/yldm-tech/forma/server/internal/health"
	"github.com/yldm-tech/forma/server/internal/platform/httpserver"
	"github.com/yldm-tech/forma/server/internal/webui"
)

// NewRouter mounts every feature on one engine. The SPA goes last because it owns NoRoute.
func NewRouter(log *slog.Logger, healthHandler *health.Handler, web *webui.Handler) *gin.Engine {
	r := httpserver.NewEngine(log)
	healthHandler.Register(r)
	web.Register(r)
	return r
}
