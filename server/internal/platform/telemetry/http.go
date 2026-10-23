package telemetry

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
)

// Middleware traces every request and records the HTTP server metrics, named after the matched route. The probes and the metrics endpoint are left out, as forma2's instrumentation ignored them: they run every few seconds and say nothing about the service.
func Middleware(serviceName string) gin.HandlerFunc {
	return otelgin.Middleware(serviceName, otelgin.WithFilter(traced))
}

func traced(r *http.Request) bool {
	p := r.URL.Path
	return p != "/health" && p != "/api/v2/health" && p != "/metrics" && !strings.HasPrefix(p, "/metrics/")
}
