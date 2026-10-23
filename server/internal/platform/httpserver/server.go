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
	"regexp"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/gin-gonic/gin"

	"github.com/yldm-tech/forma/server/internal/platform/config"
)

const requestIDHeader = "X-Request-Id"

type Server struct {
	srv             *http.Server
	shutdownTimeout time.Duration
	log             *slog.Logger
}

// NewEngine builds the engine with the middleware stack; outer runs right after the request id, outside everything else. Feature handlers are mounted by the caller.
func NewEngine(log *slog.Logger, outer ...gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	// gin's own ClientIP is never consulted: httpapi.ClientIPResolver derives the address from TRUSTED_PROXY_HOP_COUNT, and trusting no proxy here keeps the two from disagreeing.
	if err := r.SetTrustedProxies(nil); err != nil {
		panic(fmt.Sprintf("disable trusted proxies: %v", err))
	}
	r.HandleMethodNotAllowed = true
	r.Use(requestID())
	// outer middleware (the telemetry) wraps the panic recovery, so it sees a request that panicked with the 500 the recovery answered.
	r.Use(outer...)
	r.Use(securityHeaders(), accessLog(log), recovery(log), reportServerErrors())
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

// SetRequestID makes id the request id of a request served in-process on behalf of another one, as MCP runs the v3 operations its tools call, so problems and logs carry the outer request's id.
func SetRequestID(c *gin.Context, id string) {
	c.Set(requestIDHeader, id)
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

// contentSecurityPolicy is forma2's production policy (apps/web/next.config.mjs), without the frame-ancestors directive, which depends on the path.
const contentSecurityPolicy = "default-src 'self'; script-src 'self' 'unsafe-inline' https:; style-src 'self' 'unsafe-inline' https:; img-src 'self' blob: data: https:; font-src 'self' data: https:; connect-src 'self' https: wss:; frame-src 'self' https://app.cal.com https:; media-src 'self' https:; object-src 'self' data: https:; base-uri 'self'; form-action 'self'"

// securityHeaders sets the headers forma2 sent on every response. Only the public survey pages (/s/..., /c/...) may be framed by any site, since customers embed them; everything else, the admin SPA first, may be framed only by itself, so another site cannot overlay it to steer a click (clickjacking). The referrer policy keeps a token-bearing URL such as /invite?token= from leaving the origin in a Referer header.
func securityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains; preload")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if p := c.Request.URL.Path; strings.HasPrefix(p, "/s/") || strings.HasPrefix(p, "/c/") {
			h.Set("Content-Security-Policy", contentSecurityPolicy+"; frame-ancestors *")
		} else {
			h.Set("X-Frame-Options", "SAMEORIGIN")
			h.Set("Content-Security-Policy", contentSecurityPolicy+"; frame-ancestors 'self'")
		}
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
		// Reported from inside the recovering goroutine, so the event's stack trace is the panic's.
		err, ok := recovered.(error)
		if !ok {
			err = fmt.Errorf("%v", recovered)
		}
		withErrorScope(c, http.StatusInternalServerError, func(hub *sentry.Hub) { hub.CaptureException(err) })
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

// apiVersionPattern reads the API version off a path, forma2's getApiVersionFromPath.
var apiVersionPattern = regexp.MustCompile(`^/api/(v[123])(?:/|$)`)

// reportServerErrors is forma2's API error reporter: a request answered with a 5xx is reported to Sentry, when Sentry is set up, with the error a handler recorded or, without one, forma2's synthetic "API <version> error, id: <request id>". Panics are reported by recovery instead, with their stack.
func reportServerErrors() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		status := c.Writer.Status()
		if status < http.StatusInternalServerError {
			return
		}
		withErrorScope(c, status, func(hub *sentry.Hub) {
			err := c.Errors.Last()
			if err != nil {
				hub.CaptureException(err.Err)
				return
			}
			version := apiVersion(c.Request.URL.Path)
			if version == "unknown" {
				hub.CaptureException(fmt.Errorf("API error, id: %s", RequestID(c)))
				return
			}
			hub.CaptureException(fmt.Errorf("API %s error, id: %s", strings.ToUpper(version), RequestID(c)))
		})
	}
}

// withErrorScope reports to Sentry, when it is set up, in a scope tagged as forma2 tagged API errors. The path is the route pattern, never the raw path, which can carry a token.
func withErrorScope(c *gin.Context, status int, report func(hub *sentry.Hub)) {
	hub := sentry.CurrentHub()
	if hub.Client() == nil {
		return
	}
	hub = hub.Clone()
	version := apiVersion(c.Request.URL.Path)
	hub.WithScope(func(scope *sentry.Scope) {
		scope.SetLevel(sentry.LevelError)
		scope.SetTags(map[string]string{"apiVersion": version, "correlationId": RequestID(c), "method": c.Request.Method, "path": routeOf(c)})
		scope.SetContext("apiRequest", sentry.Context{"apiVersion": version, "correlationId": RequestID(c), "method": c.Request.Method, "path": routeOf(c), "status": status})
		report(hub)
	})
}

func apiVersion(path string) string {
	if m := apiVersionPattern.FindStringSubmatch(path); m != nil {
		return m[1]
	}
	return "unknown"
}
