// Package telemetry is the process's OpenTelemetry setup (forma2's instrumentation-node.ts): traces and metrics exported over OTLP HTTP and metrics served for Prometheus, both off unless configured, plus Sentry error reporting. Instruments are created from the global providers (otel.Meter, otel.Tracer), so code that records metrics does not depend on whether an exporter is configured: without one the global providers are no-ops.
package telemetry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"

	"github.com/yldm-tech/forma/server/internal/platform/buildinfo"
	"github.com/yldm-tech/forma/server/internal/platform/config"
)

// forma2's exporter settings: the OTLP metric reader exports every minute, and the batch span processor's queue, batch size, delay and export timeout.
const (
	metricExportInterval = 60 * time.Second
	spanQueueSize        = 2048
	spanBatchSize        = 512
	spanBatchDelay       = 5 * time.Second
	spanExportTimeout    = 30 * time.Second
)

// Telemetry is the running OpenTelemetry pipeline of one process.
type Telemetry struct {
	tracerProvider *sdktrace.TracerProvider
	meterProvider  *sdkmetric.MeterProvider
	metricsServer  *http.Server
	log            *slog.Logger
}

// Setup starts OpenTelemetry when PROMETHEUS_ENABLED=1 or OTEL_EXPORTER_OTLP_ENDPOINT is set and installs its providers as the global ones; otherwise it does nothing and every instrument stays a no-op, as forma2 did not load OpenTelemetry at all. An exporter that cannot be created is logged and left out rather than failing the start. Call Shutdown before the process exits so buffered spans and metrics are flushed.
func Setup(ctx context.Context, cfg config.Config, log *slog.Logger) *Telemetry {
	return setup(ctx, cfg.Telemetry, log, fmt.Sprintf("0.0.0.0:%d", cfg.Telemetry.PrometheusPort))
}

func setup(ctx context.Context, cfg config.TelemetryConfig, log *slog.Logger, prometheusAddr string) *Telemetry {
	t := &Telemetry{log: log}
	if !cfg.OTelEnabled() {
		return t
	}
	res := newResource(ctx, cfg, log)
	var exporters []string

	var readers []sdkmetric.Option
	if cfg.OTLPEndpoint != "" {
		spans, err := otlptracehttp.New(ctx)
		if err != nil {
			log.ErrorContext(ctx, "Failed to create the OTLP trace exporter. Traces will not be exported.", "err", err)
		} else {
			sampler, samplerName := newSampler(cfg.TracesSampler, cfg.TracesSamplerArg, log)
			t.tracerProvider = sdktrace.NewTracerProvider(
				sdktrace.WithResource(res),
				sdktrace.WithSampler(sampler),
				sdktrace.WithBatcher(scrubbingExporter{spans},
					sdktrace.WithMaxQueueSize(spanQueueSize),
					sdktrace.WithMaxExportBatchSize(spanBatchSize),
					sdktrace.WithBatchTimeout(spanBatchDelay),
					sdktrace.WithExportTimeout(spanExportTimeout)),
			)
			exporters = append(exporters, "traces")
			cfg.TracesSampler = samplerName
		}
		metrics, err := otlpmetrichttp.New(ctx)
		if err != nil {
			log.ErrorContext(ctx, "Failed to create the OTLP metric exporter. Metrics will not be exported over OTLP.", "err", err)
		} else {
			readers = append(readers, sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metrics, sdkmetric.WithInterval(metricExportInterval))))
			exporters = append(exporters, "otlp-metrics")
		}
	}
	if cfg.PrometheusEnabled {
		reader, err := t.startPrometheus(ctx, prometheusAddr)
		if err != nil {
			log.ErrorContext(ctx, "Failed to start the Prometheus exporter. Metrics will not be served for scraping.", "addr", prometheusAddr, "err", err)
		} else {
			readers = append(readers, sdkmetric.WithReader(reader))
			exporters = append(exporters, "prometheus-metrics")
		}
	}
	if len(readers) > 0 {
		t.meterProvider = newMeterProvider(append(readers, sdkmetric.WithResource(res))...)
		otel.SetMeterProvider(t.meterProvider)
	}
	if t.tracerProvider != nil {
		otel.SetTracerProvider(t.tracerProvider)
	}
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		log.Warn("OpenTelemetry export failed", "err", err)
	}))

	if len(exporters) == 0 {
		log.InfoContext(ctx, fmt.Sprintf("OpenTelemetry initialized (no exporters): service=%s, version=%s, environment=%s", cfg.ServiceName, buildinfo.Version, cfg.DeploymentEnvironment()))
		return t
	}
	samplerArg := ""
	if cfg.TracesSamplerArg != "" {
		samplerArg = ", samplerArg=" + cfg.TracesSamplerArg
	}
	log.InfoContext(ctx, fmt.Sprintf("OpenTelemetry initialized: service=%s, version=%s, environment=%s, exporters=%s, sampler=%s%s", cfg.ServiceName, buildinfo.Version, cfg.DeploymentEnvironment(), strings.Join(exporters, "+"), cfg.TracesSampler, samplerArg))
	return t
}

// newMeterProvider is the meter provider with the views every reader shares.
func newMeterProvider(opts ...sdkmetric.Option) *sdkmetric.MeterProvider {
	return sdkmetric.NewMeterProvider(append(opts, sdkmetric.WithView(httpServerView))...)
}

// httpServerView drops server.address and server.port from the HTTP server instruments. otelgin takes the port from the request's Host header, so without it any client could mint a series per port until the cardinality limit pushes every real route into the overflow set; both name this one service anyway.
var httpServerView = sdkmetric.NewView(
	sdkmetric.Instrument{Name: "http.server.*"},
	sdkmetric.Stream{AttributeFilter: attribute.NewDenyKeysFilter("server.address", "server.port")},
)

// newResource is forma2's resource: service.name, service.version and deployment.environment, over whatever OTEL_RESOURCE_ATTRIBUTES adds.
func newResource(ctx context.Context, cfg config.TelemetryConfig, log *slog.Logger) *resource.Resource {
	res, err := resource.New(ctx,
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
		resource.WithAttributes(
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceVersion(buildinfo.Version),
			attribute.String("deployment.environment", cfg.DeploymentEnvironment()),
		),
	)
	if err != nil {
		// A partial resource (a malformed OTEL_RESOURCE_ATTRIBUTES) is still usable; the error says what was dropped.
		log.WarnContext(ctx, "OpenTelemetry resource is incomplete", "err", err)
	}
	return res
}

// newSampler is forma2's sampler selection. It returns the sampler and the name it was selected by, always_on for an unknown one.
func newSampler(name, arg string, log *slog.Logger) (sdktrace.Sampler, string) {
	ratio := 1.0
	if arg != "" {
		if v, err := strconv.ParseFloat(arg, 64); err == nil && !math.IsNaN(v) {
			ratio = v
		}
	}
	switch name {
	case "always_on":
		return sdktrace.AlwaysSample(), name
	case "always_off":
		return sdktrace.NeverSample(), name
	case "traceidratio":
		return sdktrace.TraceIDRatioBased(ratio), name
	case "parentbased_traceidratio":
		return sdktrace.ParentBased(sdktrace.TraceIDRatioBased(ratio)), name
	case "parentbased_always_on":
		return sdktrace.ParentBased(sdktrace.AlwaysSample()), name
	case "parentbased_always_off":
		return sdktrace.ParentBased(sdktrace.NeverSample()), name
	default:
		log.Warn(fmt.Sprintf("Unknown sampler type: %s. Using always_on.", name))
		return sdktrace.AlwaysSample(), "always_on"
	}
}

// startPrometheus serves the metrics at addr/metrics on a listener of its own, which the ServiceMonitor scrapes, with the Go runtime and process collectors beside the application's instruments.
func (t *Telemetry) startPrometheus(ctx context.Context, addr string) (*otelprom.Exporter, error) {
	registry := prometheus.NewRegistry()
	if err := registry.Register(collectors.NewGoCollector()); err != nil {
		return nil, fmt.Errorf("register the Go runtime collector: %w", err)
	}
	if err := registry.Register(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{})); err != nil {
		return nil, fmt.Errorf("register the process collector: %w", err)
	}
	exporter, err := otelprom.New(otelprom.WithRegisterer(registry))
	if err != nil {
		return nil, fmt.Errorf("create the Prometheus exporter: %w", err)
	}
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("listen: %w", err)
	}
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
	t.metricsServer = &http.Server{Addr: listener.Addr().String(), Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := t.metricsServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.log.Error("Prometheus metrics server stopped", "err", err)
		}
	}()
	return exporter, nil
}

// Shutdown flushes buffered spans and metrics and stops the metrics listener.
func (t *Telemetry) Shutdown(ctx context.Context) {
	if t.tracerProvider != nil {
		if err := t.tracerProvider.Shutdown(ctx); err != nil {
			t.log.ErrorContext(ctx, "Error during OpenTelemetry shutdown", "err", err)
		}
	}
	if t.meterProvider != nil {
		if err := t.meterProvider.Shutdown(ctx); err != nil {
			t.log.ErrorContext(ctx, "Error during OpenTelemetry shutdown", "err", err)
		}
	}
	if t.metricsServer != nil {
		if err := t.metricsServer.Shutdown(ctx); err != nil {
			t.log.ErrorContext(ctx, "Error stopping the Prometheus metrics server", "err", err)
		}
	}
}
