package authz

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// recordMetrics points the package's instruments at an in-memory reader for one test. Tests that call it must not run in parallel.
func recordMetrics(t *testing.T) *sdkmetric.ManualReader {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	previous := instruments
	instruments = newMeters(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
	t.Cleanup(func() { instruments = previous })
	return reader
}

type collected map[string]metricdata.Aggregation

func collectMetrics(t *testing.T, reader *sdkmetric.ManualReader) collected {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatal(err)
	}
	out := collected{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			out[m.Name] = m.Data
		}
	}
	return out
}

func matches(set attribute.Set, want map[string]string) bool {
	for k, v := range want {
		got, ok := set.Value(attribute.Key(k))
		if !ok || got.String() != v {
			return false
		}
	}
	return true
}

// sum adds up an int64 counter's data points whose attributes include want.
func (c collected) sum(name string, want map[string]string) int64 {
	var n int64
	if s, ok := c[name].(metricdata.Sum[int64]); ok {
		for _, dp := range s.DataPoints {
			if matches(dp.Attributes, want) {
				n += dp.Value
			}
		}
	}
	return n
}

func (c collected) observations(name string, want map[string]string) uint64 {
	var n uint64
	switch h := c[name].(type) {
	case metricdata.Histogram[float64]:
		for _, dp := range h.DataPoints {
			if matches(dp.Attributes, want) {
				n += dp.Count
			}
		}
	case metricdata.Histogram[int64]:
		for _, dp := range h.DataPoints {
			if matches(dp.Attributes, want) {
				n += dp.Count
			}
		}
	}
	return n
}

func (c collected) gauge(name string, want map[string]string) (int64, bool) {
	if g, ok := c[name].(metricdata.Gauge[int64]); ok {
		for _, dp := range g.DataPoints {
			if matches(dp.Attributes, want) {
				return dp.Value, true
			}
		}
	}
	return 0, false
}

// TestDecisionsAreRecordedPerSurfaceAndCountedPerRequest drives real decisions through a request with the surface middleware: one allow, a memoized repeat (counted as a check, evaluated once), a deny and a lookup on the v3 surface, and a decision outside any surface.
func TestDecisionsAreRecordedPerSurfaceAndCountedPerRequest(t *testing.T) {
	reader := recordMetrics(t)
	e := newTestEnv(t)
	org, owner, stranger := e.org(), e.user(), e.user()
	e.member(org, owner, "owner")
	e.drain()

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(Surface(), DecisionMemo())
	r.GET("/api/v3/surveys", func(c *gin.Context) {
		ctx := c.Request.Context()
		for _, actor := range []Actor{User(owner), User(owner), User(stranger)} {
			if _, err := e.authorizer.Can(ctx, actor, OrganizationRead, Organization(org)); err != nil {
				t.Errorf("Can: %v", err)
			}
		}
		if _, err := e.authorizer.LookupWorkspaceIDs(ctx, User(stranger), true); err != nil {
			t.Errorf("LookupWorkspaceIDs: %v", err)
		}
		c.Status(http.StatusOK)
	})
	r.GET("/s/:surveyId", func(c *gin.Context) { c.Status(http.StatusOK) })
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v3/surveys", nil))
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/s/survey", nil))
	if _, err := e.authorizer.Can(e.ctx, User(owner), OrganizationRead, Organization(org)); err != nil {
		t.Fatal(err)
	}

	got := collectMetrics(t, reader)
	decisions := "forma_authzed_authorization_decisions_total"
	base := map[string]string{"action": "organization.read", "actor_type": "user", "resource_type": "organization", "error_code": "none"}
	with := func(extra map[string]string) map[string]string {
		m := map[string]string{}
		for k, v := range base {
			m[k] = v
		}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	if n := got.sum(decisions, with(map[string]string{"outcome": "allow", "surface": "api_v3"})); n != 1 {
		t.Errorf("api_v3 allows = %d, want 1 (the repeat is memoized)", n)
	}
	if n := got.sum(decisions, with(map[string]string{"outcome": "deny", "surface": "api_v3"})); n != 1 {
		t.Errorf("api_v3 denials = %d, want 1", n)
	}
	if n := got.sum(decisions, with(map[string]string{"outcome": "allow", "surface": "unscoped"})); n != 1 {
		t.Errorf("unscoped allows = %d, want 1", n)
	}
	if n := got.sum(decisions, map[string]string{"action": "workspace.write", "resource_type": "workspace", "outcome": "deny", "surface": "api_v3"}); n != 1 {
		t.Errorf("an empty lookup was recorded %d times as a denial, want 1", n)
	}
	if n := got.observations("forma_authzed_authorization_decision_duration_seconds", map[string]string{"surface": "api_v3"}); n != 3 {
		t.Errorf("api_v3 decision durations = %d, want 3", n)
	}
	checks, ok := got["forma_authzed_authorization_checks_per_request"].(metricdata.Histogram[int64])
	if !ok || len(checks.DataPoints) != 1 {
		t.Fatalf("checks per request = %#v, want one observation, for the v3 request only", got["forma_authzed_authorization_checks_per_request"])
	}
	dp := checks.DataPoints[0]
	if dp.Count != 1 || dp.Sum != 4 || !matches(dp.Attributes, map[string]string{"surface": "api_v3"}) || dp.Bounds[0] != 0.5 {
		t.Errorf("checks per request = count %d, sum %d, %v, bounds %v; want one request with 4 checks on the 0.5-first buckets", dp.Count, dp.Sum, dp.Attributes, dp.Bounds)
	}
}

// TestOperationalErrorsAndDeliveryAreRecorded: a stale revocation fails a check as an operational error with its code, and draining the outbox records the delivery, the revocation's delivery latency and the outbox status gauges.
func TestOperationalErrorsAndDeliveryAreRecorded(t *testing.T) {
	reader := recordMetrics(t)
	e := newTestEnv(t)
	org, owner, removed := e.org(), e.user(), e.user()
	e.member(org, owner, "owner")
	e.member(org, removed, "manager")
	e.drain()
	e.exec(`DELETE FROM memberships WHERE organization_id = ? AND user_id = ?`, org, removed)
	e.exec(`UPDATE authz_projection_outbox SET created_at = now() - interval '61 seconds' WHERE processed_at IS NULL`)
	e.authorizer.fresh = newFreshnessGuard(e.db)
	if _, err := e.authorizer.Can(e.ctx, User(owner), OrganizationRead, Organization(org)); err == nil {
		t.Fatal("a stale revocation did not fail the check")
	}
	e.drain()

	got := collectMetrics(t, reader)
	if n := got.sum("forma_authzed_authorization_decisions_total", map[string]string{"outcome": "operational_error", "error_code": string(CodeProjectionStale)}); n != 1 {
		t.Errorf("operational errors = %d, want 1 with authzed_projection_stale", n)
	}
	if n := got.sum("forma_authzed_projection_outbox_delivery_total", map[string]string{"status": "delivered"}); n < 3 {
		t.Errorf("delivered events = %d, want the memberships and the removal", n)
	}
	if n := got.observations("forma_authzed_projection_revocation_delivery_duration_seconds", nil); n < 1 {
		t.Error("the revocation's delivery latency was not recorded")
	}
	if v, ok := got.gauge("forma_authzed_projection_outbox_status", map[string]string{"state": "pending"}); !ok || v != 0 {
		t.Errorf("pending gauge = %d (%v), want 0 after draining", v, ok)
	}
	if _, ok := got.gauge("forma_authzed_projection_outbox_status", map[string]string{"state": "revocation_critical"}); !ok {
		t.Error("the revocation_critical state was not written")
	}
}

func TestReconciliationPassesAreRecorded(t *testing.T) {
	reader := recordMetrics(t)
	e := newTestEnv(t)
	org, owner := e.org(), e.user()
	e.member(org, owner, "owner")
	// Undelivered, so the audit finds the membership missing in SpiceDB and repairs it.
	if err := NewAudit(e.db, e.client, discardLog()).Run(e.ctx); err != nil {
		t.Fatal(err)
	}

	got := collectMetrics(t, reader)
	for _, pass := range []string{"dry_run", "apply", "confirm"} {
		if n := got.observations("forma_authzed_reconciliation_duration_seconds", map[string]string{"pass": pass}); n != 1 {
			t.Errorf("%s pass durations = %d, want 1", pass, n)
		}
	}
	if n := got.sum("forma_authzed_reconciliation_audit_total", map[string]string{"status": StatusReconciled}); n != 1 {
		t.Errorf("reconciled audits = %d, want 1", n)
	}
	if n := got.sum("forma_authzed_reconciliation_drift_total", map[string]string{"kind": "attributable"}); n < 1 {
		t.Errorf("attributable drift = %d, want the missing membership", n)
	}
	if n := got.sum("forma_authzed_reconciliation_repair_total", map[string]string{"status": "repaired"}); n < 1 {
		t.Errorf("repairs = %d, want the missing membership", n)
	}
}

func TestReconciliationFailuresBookUnreportedCodes(t *testing.T) {
	reader := recordMetrics(t)
	recordReconciliationPass(context.Background(), "apply", StatusFailed, 0, 5, []Code{CodeTimeout, CodeTimeout, CodeUnavailable})
	got := collectMetrics(t, reader)
	for code, want := range map[string]int64{string(CodeTimeout): 2, string(CodeUnavailable): 1, unreportedFailureCode: 2} {
		if n := got.sum("forma_authzed_reconciliation_failure_total", map[string]string{"code": code, "pass": "apply"}); n != want {
			t.Errorf("failures with code %s = %d, want %d", code, n, want)
		}
	}
}

// TestRequestPathMetrics: retries, an exhausted request, the circuit's state set on each transition, and short circuits.
func TestRequestPathMetrics(t *testing.T) {
	reader := recordMetrics(t)
	clock := newFakeClock()
	r, _ := testRunner(RequestProfile, discardLog())
	r.breaker = newBreaker(clock.now)
	for range circuitFailureThreshold {
		_ = r.do(context.Background(), "check_permission", func(context.Context) error { return status.Error(codes.Unavailable, "") })
	}
	_ = r.do(context.Background(), "check_permission", func(context.Context) error { return nil })

	got := collectMetrics(t, reader)
	op := map[string]string{"code": string(CodeUnavailable), "operation": "check_permission"}
	if n := got.sum("forma_authzed_request_retries_total", op); n != 2*circuitFailureThreshold {
		t.Errorf("retries = %d, want two per request", n)
	}
	if n := got.sum("forma_authzed_request_failures_total", map[string]string{"code": string(CodeUnavailable), "retryable": "true"}); n != circuitFailureThreshold {
		t.Errorf("failures = %d, want %d", n, circuitFailureThreshold)
	}
	if n := got.sum("forma_authzed_request_short_circuits_total", op); n != 1 {
		t.Errorf("short circuits = %d, want 1", n)
	}
	for state, want := range map[string]int64{"open": 1, "closed": 0, "half_open": 0} {
		if v, ok := got.gauge("forma_authzed_request_circuit_state", map[string]string{"state": state}); !ok || v != want {
			t.Errorf("circuit state %s = %d (%v), want %d", state, v, ok, want)
		}
	}
}

func TestSurfaceForRequest(t *testing.T) {
	for _, tc := range []struct{ method, path, want string }{
		{http.MethodGet, "/api/app/surveys/1", SurfacePage},
		{http.MethodPost, "/api/app/surveys", SurfaceServerAction},
		{http.MethodGet, "/api/v1/management/surveys", SurfaceAPIV1},
		{http.MethodGet, "/api/v2/me", SurfaceAPIV2},
		{http.MethodDelete, "/api/v3/surveys/1", SurfaceAPIV3},
		{http.MethodPost, "/api/mcp", SurfaceMCP},
		{http.MethodGet, "/api/v1/client/ws/environment", ""},
		{http.MethodPost, "/api/v2/client/ws/responses", ""},
		{http.MethodGet, "/api/v2/health", ""},
		{http.MethodGet, "/api/apps", ""},
		{http.MethodGet, "/s/abc", ""},
	} {
		if got := surfaceForRequest(tc.method, tc.path); got != tc.want {
			t.Errorf("surfaceForRequest(%s %s) = %q, want %q", tc.method, tc.path, got, tc.want)
		}
	}
}

func TestANestedSurfaceKeepsTheOuterOne(t *testing.T) {
	outer, record := WithSurface(context.Background(), SurfaceMCP)
	inner, recordInner := WithSurface(outer, SurfaceAPIV3)
	checkIssued(inner)
	if surfaceOf(inner) != SurfaceMCP {
		t.Errorf("nested surface = %q, want mcp", surfaceOf(inner))
	}
	reader := recordMetrics(t)
	recordInner(inner)
	record(outer)
	checks, _ := collectMetrics(t, reader)["forma_authzed_authorization_checks_per_request"].(metricdata.Histogram[int64])
	if len(checks.DataPoints) != 1 || checks.DataPoints[0].Count != 1 || checks.DataPoints[0].Sum != 1 {
		t.Errorf("checks per request = %+v, want one mcp observation of the nested check", checks.DataPoints)
	}
}
