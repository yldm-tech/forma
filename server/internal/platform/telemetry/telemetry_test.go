package telemetry

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getsentry/sentry-go"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/yldm-tech/forma/server/internal/platform/config"
)

func discardLog() *slog.Logger { return slog.New(slog.DiscardHandler) }

func TestSetupDoesNothingWithoutAnExporter(t *testing.T) {
	tel := setup(t.Context(), config.TelemetryConfig{ServiceName: "forma", PrometheusPort: 9464, TracesSampler: "always_on"}, discardLog(), "127.0.0.1:0")
	if tel.meterProvider != nil || tel.tracerProvider != nil || tel.metricsServer != nil {
		t.Fatalf("telemetry started without PROMETHEUS_ENABLED or an OTLP endpoint: %+v", tel)
	}
	tel.Shutdown(t.Context())
}

func TestSamplerSelection(t *testing.T) {
	for _, tc := range []struct {
		name, arg, wantName, wantDescription string
	}{
		{"always_on", "", "always_on", "AlwaysOnSampler"},
		{"always_off", "", "always_off", "AlwaysOffSampler"},
		{"traceidratio", "0.25", "traceidratio", "TraceIDRatioBased{0.25}"},
		{"traceidratio", "", "traceidratio", "TraceIDRatioBased{1}"},
		{"traceidratio", "NaN", "traceidratio", "TraceIDRatioBased{1}"},
		{"traceidratio", "not a number", "traceidratio", "TraceIDRatioBased{1}"},
		{"parentbased_traceidratio", "0.5", "parentbased_traceidratio", "ParentBased{root:TraceIDRatioBased{0.5}"},
		{"parentbased_always_on", "", "parentbased_always_on", "ParentBased{root:AlwaysOnSampler"},
		{"parentbased_always_off", "", "parentbased_always_off", "ParentBased{root:AlwaysOffSampler"},
		{"jaeger_remote", "", "always_on", "AlwaysOnSampler"},
	} {
		sampler, name := newSampler(tc.name, tc.arg, discardLog())
		if name != tc.wantName || !strings.HasPrefix(sampler.Description(), tc.wantDescription) {
			t.Errorf("newSampler(%q, %q) = %s (%s), want %s (%s...)", tc.name, tc.arg, sampler.Description(), name, tc.wantDescription, tc.wantName)
		}
	}
}

// TestPrometheusServesForma2SeriesNames scrapes the Prometheus listener: the instruments keep forma2's names (no unit appended twice), histograms use their declared buckets, and the Go runtime collector sits beside them.
func TestPrometheusServesForma2SeriesNames(t *testing.T) {
	tel := setup(t.Context(), config.TelemetryConfig{ServiceName: "forma", PrometheusEnabled: true, PrometheusPort: 9464, TracesSampler: "always_on"}, discardLog(), "127.0.0.1:0")
	t.Cleanup(func() { tel.Shutdown(context.Background()) })
	if tel.meterProvider == nil || tel.metricsServer == nil {
		t.Fatal("PROMETHEUS_ENABLED=1 did not start the metrics listener")
	}
	if tel.tracerProvider != nil {
		t.Error("a tracer provider was created without an OTLP endpoint")
	}
	meter := tel.meterProvider.Meter("forma.jobs")
	counter, _ := meter.Int64Counter("forma_jobs_processed_total")
	counter.Add(t.Context(), 2, metric.WithAttributes(attribute.String("job_name", "system.test-log"), attribute.String("status", "completed")))
	histogram, _ := meter.Float64Histogram("forma_jobs_duration_seconds", metric.WithUnit("s"), metric.WithExplicitBucketBoundaries(0.05, 0.1))
	histogram.Record(t.Context(), 0.07)

	res, err := http.Get("http://" + tel.metricsServer.Addr + "/metrics")
	if err != nil {
		t.Fatalf("scrape: %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	body, _ := io.ReadAll(res.Body)
	text := string(body)
	for _, want := range []string{
		`forma_jobs_processed_total{job_name="system.test-log",otel_scope_name="forma.jobs"`,
		`forma_jobs_duration_seconds_bucket{otel_scope_name="forma.jobs"`,
		`le="0.05"`,
		"go_goroutines",
		`target_info{deployment_environment="production"`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("scrape lacks %s:\n%s", want, text)
		}
	}
	if strings.Contains(text, "_seconds_seconds") || strings.Contains(text, "_total_total") {
		t.Errorf("a unit or counter suffix was appended twice:\n%s", text)
	}
}

// TestHTTPSpansCarryNoTokensOrAddresses runs a request through the tracing middleware into the scrubbing exporter: the span is named after the route and keeps it, and nothing from the raw path, the query, the client address or the error message is exported.
func TestHTTPSpansCarryNoTokensOrAddresses(t *testing.T) {
	exported := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(scrubbingExporter{exported}))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { otel.SetTracerProvider(previous) })

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(Middleware("forma"))
	r.GET("/c/:token", func(c *gin.Context) {
		_ = c.Error(errors.New("contact jane@example.com not found"))
		c.Status(http.StatusInternalServerError)
	})
	r.GET("/health", func(c *gin.Context) { c.Status(http.StatusOK) })
	r.GET("/api/v2/health", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/c/secret-token?answer=my-private-answer", nil)
	req.RemoteAddr = "203.0.113.7:51234"
	r.ServeHTTP(httptest.NewRecorder(), req)
	for _, probe := range []string{"/health", "/api/v2/health"} {
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, probe, nil))
	}

	spans := exported.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("exported %d spans, want only the survey link's (the probes are not traced)", len(spans))
	}
	span := spans[0]
	if span.Name != "GET /c/:token" {
		t.Errorf("span name = %q, want the method and the route pattern", span.Name)
	}
	if span.Status.Code != codes.Error || span.Status.Description != "" {
		t.Errorf("status = %+v, want an error without its message", span.Status)
	}
	route := false
	for _, a := range span.Attributes {
		v := a.Value.String()
		if strings.Contains(v, "secret-token") || strings.Contains(v, "my-private-answer") || strings.Contains(v, "203.0.113.7") || strings.Contains(v, "jane@example.com") {
			t.Errorf("attribute %s = %q leaks the path, query, client address or error", a.Key, v)
		}
		if a.Key == "http.route" && v == "/c/:token" {
			route = true
		}
	}
	if !route {
		t.Errorf("http.route missing from %v", span.Attributes)
	}
}

func TestPoolMetrics(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	db, err := sql.Open("pgx", "postgres://forma@127.0.0.1:1/forma")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(7)
	stop, err := observePool(provider, "main", func() poolStats {
		s := db.Stats()
		return poolStats{idle: int64(s.Idle), inUse: int64(s.InUse), max: int64(s.MaxOpenConnections), waits: s.WaitCount, waited: s.WaitDuration.Seconds()}
	})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(t.Context(), "postgres://forma@127.0.0.1:1/forma?pool_max_conns=3")
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	stopJobs, err := observePool(provider, "jobs", func() poolStats {
		s := pool.Stat()
		return poolStats{max: int64(s.MaxConns())}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stopJobs()

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(t.Context(), &rm); err != nil {
		t.Fatal(err)
	}
	got := map[string]int64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if g, ok := m.Data.(metricdata.Gauge[int64]); ok && m.Name == "forma_db_pool_max_connections" {
				for _, dp := range g.DataPoints {
					name, _ := dp.Attributes.Value("pool")
					got[name.AsString()] = dp.Value
				}
			}
		}
	}
	if got["main"] != 7 || got["jobs"] != 3 {
		t.Errorf("forma_db_pool_max_connections = %v, want main 7 and jobs 3", got)
	}

	stop()
	rm = metricdata.ResourceMetrics{}
	if err := reader.Collect(t.Context(), &rm); err != nil {
		t.Fatal(err)
	}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if g, ok := m.Data.(metricdata.Gauge[int64]); ok && m.Name == "forma_db_pool_max_connections" {
				for _, dp := range g.DataPoints {
					if name, _ := dp.Attributes.Value("pool"); name.AsString() == "main" {
						t.Error("the main pool is still observed after its reporting was stopped")
					}
				}
			}
		}
	}
}

// TestSentryEventsAreScrubbed sends an event whose scope carries a request and a user through forma2's client options: what reaches the transport has neither.
func TestSentryEventsAreScrubbed(t *testing.T) {
	transport := &sentry.MockTransport{}
	opts := sentryOptions(config.TelemetryConfig{SentryDSN: "https://public@o0.ingest.sentry.io/1", SentryEnvironment: "staging"})
	opts.Transport = transport
	client, err := sentry.NewClient(opts)
	if err != nil {
		t.Fatal(err)
	}
	if dc := opts.DataCollection; dc == nil || dc.UserInfo.Or(true) || dc.Cookies.Mode != sentry.CollectionOff || len(dc.HTTPBodies) != 0 || opts.TracesSampleRate != 0 || !opts.DisableClientReports || opts.Release != "" {
		t.Errorf("options = %+v, want no default PII, no tracing, no client reports and no release on a development build", opts)
	}
	hub := sentry.NewHub(client, sentry.NewScope())
	hub.WithScope(func(scope *sentry.Scope) {
		req := httptest.NewRequest(http.MethodPost, "/c/secret-token?answer=private", strings.NewReader(`{"answer":"private"}`))
		req.Header.Set("Authorization", "Bearer secret")
		scope.SetRequest(req)
		scope.SetUser(sentry.User{Email: "jane@example.com", IPAddress: "203.0.113.7"})
		hub.CaptureException(errors.New("boom"))
	})
	events := transport.Events()
	if len(events) != 1 {
		t.Fatalf("sent %d events, want 1", len(events))
	}
	e := events[0]
	if e.Request != nil || e.User.Email != "" || e.User.IPAddress != "" {
		t.Errorf("event kept the request %+v or the user %+v", e.Request, e.User)
	}
	if e.Environment != "staging" {
		t.Errorf("environment = %q, want SENTRY_ENVIRONMENT", e.Environment)
	}
}
