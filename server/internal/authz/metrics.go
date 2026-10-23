package authz

import (
	"context"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// meters are forma2's authorization and AuthZed instruments (apps/web/lib/authorization/metrics.ts, apps/web/lib/authzed/metrics.ts), under their meters, names and buckets. Every attribute is a bounded value, never an identifier: these leave the deployment when an exporter is configured.
type meters struct {
	decisions        metric.Int64Counter
	decisionDuration metric.Float64Histogram
	checksPerRequest metric.Int64Histogram

	requestFailures      metric.Int64Counter
	requestRetries       metric.Int64Counter
	requestCircuitState  metric.Int64Gauge
	requestShortCircuits metric.Int64Counter

	outboxDelivery         metric.Int64Counter
	outboxDeliveryDuration metric.Float64Histogram
	revocationDelivery     metric.Float64Histogram
	outboxStatus           metric.Int64Gauge
	outboxOldestPendingAge metric.Float64Gauge

	reconciliationAudit    metric.Int64Counter
	reconciliationDrift    metric.Int64Counter
	reconciliationRepair   metric.Int64Counter
	reconciliationDuration metric.Float64Histogram
	reconciliationFailure  metric.Int64Counter
}

// Creating an instrument fails only on an invalid name or option, which these constants are not; a failure still yields a usable no-op instrument, so the errors are dropped.
func newMeters(mp metric.MeterProvider) *meters {
	m := &meters{}
	authorization := mp.Meter("forma.authzed.authorization")
	m.decisions, _ = authorization.Int64Counter("forma_authzed_authorization_decisions_total", metric.WithDescription("Authoritative SpiceDB authorization decisions by bounded outcome"))
	m.decisionDuration, _ = authorization.Float64Histogram("forma_authzed_authorization_decision_duration_seconds", metric.WithDescription("Duration of authoritative SpiceDB authorization operations"), metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.005, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 0.75, 1, 2.5, 5))
	// The 0.5 boundary separates a request that authorized nothing from one that authorized once.
	m.checksPerRequest, _ = authorization.Int64Histogram("forma_authzed_authorization_checks_per_request", metric.WithDescription("Number of central authorization decisions made while handling one request"), metric.WithUnit("{check}"),
		metric.WithExplicitBucketBoundaries(0.5, 1, 2, 3, 5, 8, 13, 21, 34, 55, 89, 144, 250, 350, 500, 750, 1000))

	authzed := mp.Meter("forma.authzed")
	m.requestFailures, _ = authzed.Int64Counter("forma_authzed_request_failures_total", metric.WithDescription("AuthZed requests that failed after exhausting retries"))
	m.requestRetries, _ = authzed.Int64Counter("forma_authzed_request_retries_total", metric.WithDescription("AuthZed requests retried after a retryable failure"))
	m.requestCircuitState, _ = authzed.Int64Gauge("forma_authzed_request_circuit_state", metric.WithDescription("Request-path AuthZed circuit breaker state, 1 for the state in force"), metric.WithUnit("{state}"))
	m.requestShortCircuits, _ = authzed.Int64Counter("forma_authzed_request_short_circuits_total", metric.WithDescription("AuthZed requests refused locally by an open request-path circuit"))
	m.outboxDelivery, _ = authzed.Int64Counter("forma_authzed_projection_outbox_delivery_total", metric.WithDescription("Authorization projection outbox events processed by outcome"))
	m.outboxDeliveryDuration, _ = authzed.Float64Histogram("forma_authzed_projection_outbox_delivery_duration_seconds", metric.WithDescription("Duration of an authorization projection outbox delivery batch"), metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30))
	m.revocationDelivery, _ = authzed.Float64Histogram("forma_authzed_projection_revocation_delivery_duration_seconds", metric.WithDescription("Time from a committed authorization revocation to successful SpiceDB delivery"), metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.1, 0.5, 1, 2.5, 5, 10, 15, 30, 45, 60, 120, 300))
	m.outboxStatus, _ = authzed.Int64Gauge("forma_authzed_projection_outbox_status", metric.WithDescription("Point-in-time authorization projection outbox counts by bounded state"), metric.WithUnit("{event}"))
	m.outboxOldestPendingAge, _ = authzed.Float64Gauge("forma_authzed_projection_outbox_oldest_pending_age_seconds", metric.WithDescription("Point-in-time age of the oldest pending authorization projection event"), metric.WithUnit("s"))
	m.reconciliationAudit, _ = authzed.Int64Counter("forma_authzed_reconciliation_audit_total", metric.WithDescription("Scheduled authorization relationship audits by outcome"))
	m.reconciliationDrift, _ = authzed.Int64Counter("forma_authzed_reconciliation_drift_total", metric.WithDescription("Attributable relationship differences observed by scheduled audits"))
	m.reconciliationRepair, _ = authzed.Int64Counter("forma_authzed_reconciliation_repair_total", metric.WithDescription("Attributable relationship repair results from scheduled reconciliation"))
	m.reconciliationDuration, _ = authzed.Float64Histogram("forma_authzed_reconciliation_duration_seconds", metric.WithDescription("Duration of one pass of the scheduled authorization reconciliation"), metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(1, 5, 10, 30, 60, 120, 300, 600, 1800, 3600))
	m.reconciliationFailure, _ = authzed.Int64Counter("forma_authzed_reconciliation_failure_total", metric.WithDescription("Scheduled authorization reconciliation unit failures by error code"))
	return m
}

// instruments record through the global meter provider, which telemetry.Setup installs when an exporter is configured.
var instruments = newMeters(otel.GetMeterProvider())

// Authorization surfaces, forma2's TAuthorizationSurface: which kind of request a decision was made for. A decision made outside any is "unscoped".
const (
	SurfaceServerAction = "server_action"
	SurfacePage         = "page"
	SurfaceAPIV1        = "api_v1"
	SurfaceAPIV2        = "api_v2"
	SurfaceAPIV3        = "api_v3"
	SurfaceMCP          = "mcp"
	surfaceUnscoped     = "unscoped"
)

type surfaceKey struct{}

// surfaceScope is one request's surface and how many central authorization operations it made.
type surfaceScope struct {
	surface string
	checks  atomic.Int64
}

// WithSurface opens an authorization surface on ctx. A request has one surface: inside one already open, ctx is returned unchanged and the outer surface keeps counting, as MCP's in-process v3 operations count for the MCP request. record is the surface's checks-per-request observation; it is a no-op for a nested call.
func WithSurface(ctx context.Context, surface string) (context.Context, func(ctx context.Context)) {
	if _, ok := ctx.Value(surfaceKey{}).(*surfaceScope); ok {
		return ctx, func(context.Context) {}
	}
	scope := &surfaceScope{surface: surface}
	return context.WithValue(ctx, surfaceKey{}, scope), func(ctx context.Context) {
		instruments.checksPerRequest.Record(ctx, scope.checks.Load(), metric.WithAttributes(attribute.String("surface", surface)))
	}
}

func surfaceOf(ctx context.Context) string {
	if s, ok := ctx.Value(surfaceKey{}).(*surfaceScope); ok {
		return s.surface
	}
	return surfaceUnscoped
}

// checkIssued counts one central authorization operation. Repeated questions count each time, memoized or not: a page that asks the same thing twenty times has an N+1 worth seeing.
func checkIssued(ctx context.Context) {
	if s, ok := ctx.Value(surfaceKey{}).(*surfaceScope); ok {
		s.checks.Add(1)
	}
}

// Surface opens the authorization surface of every request by its path: the admin SPA's /api/app reads as forma2's pages and its mutations as its server actions, the management APIs and MCP as themselves. Requests that authorize nobody (the client API the SDK calls, the health check, public pages) open none, so they add no zero observations to the checks-per-request histogram.
func Surface() gin.HandlerFunc {
	return func(c *gin.Context) {
		surface := surfaceForRequest(c.Request.Method, c.Request.URL.Path)
		if surface == "" {
			c.Next()
			return
		}
		ctx, record := WithSurface(c.Request.Context(), surface)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
		record(ctx)
	}
}

func surfaceForRequest(method, path string) string {
	under := func(prefix string) bool { return path == prefix || strings.HasPrefix(path, prefix+"/") }
	switch {
	case under("/api/v1/client"), under("/api/v2/client"), path == "/api/v2/health":
		return ""
	case under("/api/app"):
		if method == "GET" || method == "HEAD" {
			return SurfacePage
		}
		return SurfaceServerAction
	case under("/api/v1"):
		return SurfaceAPIV1
	case under("/api/v2"):
		return SurfaceAPIV2
	case under("/api/v3"):
		return SurfaceAPIV3
	case under("/api/mcp"):
		return SurfaceMCP
	}
	return ""
}

// recordDecision is one authoritative decision: allow, deny, or an operational error with its code.
func recordDecision(ctx context.Context, action string, actorType ActorType, resourceType string, started time.Time, allowed bool, err error) {
	outcome, code := "deny", "none"
	switch {
	case err != nil:
		outcome, code = "operational_error", string(CodeInternal)
		if e, ok := AsError(err); ok {
			code = string(e.Code)
		}
	case allowed:
		outcome = "allow"
	}
	base := []attribute.KeyValue{
		attribute.String("action", action),
		attribute.String("actor_type", string(actorType)),
		attribute.String("outcome", outcome),
		attribute.String("resource_type", resourceType),
		attribute.String("surface", surfaceOf(ctx)),
	}
	instruments.decisions.Add(ctx, 1, metric.WithAttributes(append(base, attribute.String("error_code", code))...))
	instruments.decisionDuration.Record(ctx, max(0, time.Since(started).Seconds()), metric.WithAttributes(base...))
}

func recordRequestFailure(ctx context.Context, e *Error) {
	instruments.requestFailures.Add(ctx, 1, metric.WithAttributes(attribute.String("code", string(e.Code)), attribute.String("operation", e.Operation), attribute.Bool("retryable", e.Retryable)))
}

func recordRequestRetry(ctx context.Context, e *Error) {
	instruments.requestRetries.Add(ctx, 1, metric.WithAttributes(attribute.String("code", string(e.Code)), attribute.String("operation", e.Operation)))
}

func recordShortCircuit(ctx context.Context, code Code, operation string) {
	instruments.requestShortCircuits.Add(ctx, 1, metric.WithAttributes(attribute.String("code", string(code)), attribute.String("operation", operation)))
}

// recordCircuitState writes the breaker's state set: 1 for the state in force, 0 for the others. It is written on transitions only, so a process whose circuit never moved exports no series rather than a reassuring zero.
func recordCircuitState(state circuitState) {
	for _, candidate := range []circuitState{circuitClosed, circuitHalfOpen, circuitOpen} {
		v := int64(0)
		if candidate == state {
			v = 1
		}
		instruments.requestCircuitState.Record(context.Background(), v, metric.WithAttributes(attribute.String("state", candidate.String())))
	}
}

func recordOutboxDelivery(ctx context.Context, count int, duration time.Duration, status string) {
	attrs := metric.WithAttributes(attribute.String("status", status))
	instruments.outboxDelivery.Add(ctx, int64(count), attrs)
	instruments.outboxDeliveryDuration.Record(ctx, duration.Seconds(), attrs)
}

func recordRevocationDelivery(ctx context.Context, age time.Duration) {
	instruments.revocationDelivery.Record(ctx, max(0, age.Seconds()))
}

func recordOutboxStatus(ctx context.Context, s OutboxStatus) {
	for state, n := range map[string]int64{"pending": s.Pending, "dead_lettered": s.DeadLettered, "revocation_warning": s.RevocationsPastWarning, "revocation_critical": s.RevocationsPastCritical} {
		instruments.outboxStatus.Record(ctx, n, metric.WithAttributes(attribute.String("state", state)))
	}
	age := 0.0
	if s.OldestPendingAgeSeconds != nil {
		age = *s.OldestPendingAgeSeconds
	}
	instruments.outboxOldestPendingAge.Record(ctx, age)
}

func recordReconciliationAudit(ctx context.Context, status string, drift, failures int64) {
	instruments.reconciliationAudit.Add(ctx, 1, metric.WithAttributes(attribute.String("status", status)))
	if drift > 0 {
		instruments.reconciliationDrift.Add(ctx, drift, metric.WithAttributes(attribute.String("kind", "attributable")))
	}
	if failures > 0 {
		instruments.reconciliationDrift.Add(ctx, failures, metric.WithAttributes(attribute.String("kind", "failure")))
	}
}

func recordReconciliationRepair(ctx context.Context, repaired, failed int64) {
	if repaired > 0 {
		instruments.reconciliationRepair.Add(ctx, repaired, metric.WithAttributes(attribute.String("status", "repaired")))
	}
	if failed > 0 {
		instruments.reconciliationRepair.Add(ctx, failed, metric.WithAttributes(attribute.String("status", "failed")))
	}
}

// unreportedFailureCode books the failures a pass counted beyond the codes it listed (the list is capped), so the failure counter still sums to the pass's failed count.
const unreportedFailureCode = "unreported"

func recordReconciliationPass(ctx context.Context, pass, status string, duration time.Duration, failed int64, codes []Code) {
	instruments.reconciliationDuration.Record(ctx, max(0, duration.Seconds()), metric.WithAttributes(attribute.String("pass", pass), attribute.String("status", status)))
	byCode := map[string]int64{}
	for _, c := range codes {
		byCode[string(c)]++
	}
	if unreported := failed - int64(len(codes)); unreported > 0 {
		byCode[unreportedFailureCode] += unreported
	}
	for code, n := range byCode {
		instruments.reconciliationFailure.Add(ctx, n, metric.WithAttributes(attribute.String("code", code), attribute.String("pass", pass)))
	}
}
