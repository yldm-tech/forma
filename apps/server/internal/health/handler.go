// Package health serves the liveness and readiness probes.
package health

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Pinger is the one capability readiness needs from the database.
type Pinger interface {
	PingContext(ctx context.Context) error
}

type Handler struct {
	db  Pinger
	log *slog.Logger
}

func NewHandler(db Pinger, log *slog.Logger) *Handler {
	return &Handler{db: db, log: log}
}

// NewDatabasePinger adapts the GORM pool to Pinger.
func NewDatabasePinger(db *gorm.DB) (Pinger, error) {
	return db.DB()
}

// Register mounts the probes at /health and /health/ready, the paths the Helm chart and external monitors probe.
func (h *Handler) Register(r gin.IRouter) {
	r.GET("/health", h.live)
	r.GET("/health/ready", h.ready)
}

// live answers "is this process serving HTTP". It must stay true during a dependency outage, or kubelet restarts every pod at once.
func (h *Handler) live(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// ready answers "can this pod serve a request that needs its data". Deliberately Postgres only: Valkey and SpiceDB outages degrade individual features, and draining every replica out of the Service on such a blip costs more than it saves.
func (h *Handler) ready(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()

	c.Header("Cache-Control", "no-store")
	if err := h.db.PingContext(ctx); err != nil {
		h.log.WarnContext(ctx, "readiness check failed", "dependency", "postgres", "err", err)
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "error", "main_database": false})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "main_database": true})
}
