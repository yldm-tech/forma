// Package httpserver owns the gin engine, the middleware every request passes through, and graceful shutdown.
package httpserver

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/yldm-tech/forma/server/internal/platform/config"
)

const requestIDHeader = "X-Request-Id"

type Server struct {
	srv             *http.Server
	shutdownTimeout time.Duration
	log             *slog.Logger
}

// NewEngine builds the engine with the middleware stack. Feature handlers are mounted by the caller.
func NewEngine(log *slog.Logger) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	// gin's own ClientIP is never consulted: httpapi.ClientIPResolver derives the address from TRUSTED_PROXY_HOP_COUNT, and trusting no proxy here keeps the two from disagreeing.
	if err := r.SetTrustedProxies(nil); err != nil {
		panic(fmt.Sprintf("disable trusted proxies: %v", err))
	}
	r.HandleMethodNotAllowed = true
	r.Use(requestID(), accessLog(log), recovery(log))
	return r
}

func New(cfg config.Config, engine *gin.Engine, log *slog.Logger) *Server {
	return &Server{
		srv: &http.Server{
			Addr:              cfg.HTTPAddr,
			Handler:           engine,
			ReadHeaderTimeout: 10 * time.Second,
			IdleTimeout:       120 * time.Second,
		},
		shutdownTimeout: cfg.ShutdownTimeout,
		log:             log,
	}
}

// Run serves until ctx is cancelled, then drains in-flight requests for up to the shutdown timeout.
func (s *Server) Run(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		s.log.Info("http server listening", "addr", s.srv.Addr)
		if err := s.srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("http server: %w", err)
	case <-ctx.Done():
	}

	s.log.Info("http server shutting down", "timeout", s.shutdownTimeout)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), s.shutdownTimeout)
	defer cancel()
	if err := s.srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("http server shutdown: %w", err)
	}
	return <-errCh
}

// RequestID returns the id the requestID middleware assigned to this request.
func RequestID(c *gin.Context) string {
	return c.GetString(requestIDHeader)
}

// requestID keeps an inbound X-Request-Id (a gateway may already have set one) or mints one, and echoes it back.
func requestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(requestIDHeader)
		if id == "" || len(id) > 128 {
			var b [16]byte
			_, _ = rand.Read(b[:])
			id = hex.EncodeToString(b[:])
		}
		c.Set(requestIDHeader, id)
		c.Header(requestIDHeader, id)
		c.Next()
	}
}

func accessLog(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		// Probes run every few seconds per pod; logging them buries everything else.
		if c.Request.URL.Path == "/health" || c.Request.URL.Path == "/health/ready" {
			return
		}
		level := slog.LevelInfo
		if c.Writer.Status() >= http.StatusInternalServerError {
			level = slog.LevelError
		}
		log.LogAttrs(c.Request.Context(), level, "http request",
			slog.String("request_id", c.GetString(requestIDHeader)),
			slog.String("method", c.Request.Method),
			slog.String("route", routeOf(c)),
			slog.Int("status", c.Writer.Status()),
			slog.Duration("duration", time.Since(start)),
		)
	}
}

func recovery(log *slog.Logger) gin.HandlerFunc {
	return gin.CustomRecoveryWithWriter(nil, func(c *gin.Context, recovered any) {
		log.ErrorContext(c.Request.Context(), "panic serving request",
			"request_id", c.GetString(requestIDHeader),
			"route", routeOf(c),
			"panic", recovered,
		)
		// Same envelope and wording as an unexpected error returned by a handler (see httpapi.ErrorHandler).
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"code": "internal_server_error", "message": "Something went wrong while executing the operation."})
	})
}

// routeOf returns the matched route pattern. Unmatched requests (the SPA fallback, 404s) are logged by a fixed label rather than their raw path, which can carry tokens such as a personal survey link.
func routeOf(c *gin.Context) string {
	if r := c.FullPath(); r != "" {
		return r
	}
	return "(unmatched)"
}
