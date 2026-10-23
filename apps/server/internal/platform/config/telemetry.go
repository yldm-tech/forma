package config

import (
	"fmt"
	"net/url"
)

// TelemetryConfig is forma2's observability and product analytics configuration, all off by default: OpenTelemetry traces and metrics (OTEL_*, PROMETHEUS_*), Sentry (SENTRY_*), PostHog (POSTHOG_KEY) and the daily usage report (TELEMETRY_ENABLED).
type TelemetryConfig struct {
	// ServiceName is the service.name resource attribute of every span and metric.
	ServiceName string `env:"OTEL_SERVICE_NAME" envDefault:"forma"`
	// Environment and NodeEnv are forma2's deployment.environment: ENVIRONMENT, else NODE_ENV, else production (the Go server only ever runs a production build, where forma2's NODE_ENV was production).
	Environment string `env:"ENVIRONMENT"`
	NodeEnv     string `env:"NODE_ENV"`
	// OTLPEndpoint switches the OTLP HTTP trace and metric exporters on. The exporters read it, OTEL_EXPORTER_OTLP_HEADERS and the other OTEL_EXPORTER_OTLP_* variables themselves, appending /v1/traces and /v1/metrics.
	OTLPEndpoint string `env:"OTEL_EXPORTER_OTLP_ENDPOINT"`
	// PrometheusEnabled serves the metrics for scraping at 0.0.0.0:<PrometheusPort>/metrics, a listener of its own beside the app's.
	PrometheusEnabled bool `env:"PROMETHEUS_ENABLED" envDefault:"0"`
	PrometheusPort    int  `env:"PROMETHEUS_EXPORTER_PORT" envDefault:"9464"`
	// TracesSampler and TracesSamplerArg are the OpenTelemetry sampler names forma2 accepted (always_on, always_off, traceidratio and the parentbased_ variants) and the ratio. An unknown sampler logs a warning and samples everything; a ratio that is not a number is ignored.
	TracesSampler    string `env:"OTEL_TRACES_SAMPLER" envDefault:"always_on"`
	TracesSamplerArg string `env:"OTEL_TRACES_SAMPLER_ARG"`

	// SentryDSN reports server errors (5xx answers and panics) to Sentry, and SentryEnvironment labels them.
	SentryDSN         string `env:"SENTRY_DSN"`
	SentryEnvironment string `env:"SENTRY_ENVIRONMENT"`

	// PostHogKey sends forma2's product analytics events to PostHog's EU cloud and serves the browser's PostHog requests through /ingest.
	PostHogKey string `env:"POSTHOG_KEY"`

	// UsageReportEnabled sends the daily anonymous usage report to Forma, forma2's opt-in TELEMETRY_ENABLED.
	UsageReportEnabled bool `env:"TELEMETRY_ENABLED" envDefault:"0"`
}

// OTelEnabled is when forma2 loaded OpenTelemetry at all: Prometheus metrics or an OTLP endpoint.
func (t TelemetryConfig) OTelEnabled() bool {
	return t.PrometheusEnabled || t.OTLPEndpoint != ""
}

// DeploymentEnvironment is the deployment.environment resource attribute.
func (t TelemetryConfig) DeploymentEnvironment() string {
	switch {
	case t.Environment != "":
		return t.Environment
	case t.NodeEnv != "":
		return t.NodeEnv
	default:
		return "production"
	}
}

func (t TelemetryConfig) validate() error {
	if t.ServiceName == "" {
		return fmt.Errorf("OTEL_SERVICE_NAME must not be empty")
	}
	if t.OTLPEndpoint != "" {
		if u, err := url.Parse(t.OTLPEndpoint); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("OTEL_EXPORTER_OTLP_ENDPOINT must be an absolute http(s) URL, got %q", t.OTLPEndpoint)
		}
	}
	if t.PrometheusPort < 1 || t.PrometheusPort > 65535 {
		return fmt.Errorf("PROMETHEUS_EXPORTER_PORT must be a port number, got %d", t.PrometheusPort)
	}
	if t.SentryDSN != "" {
		if u, err := url.Parse(t.SentryDSN); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("SENTRY_DSN must be a Sentry DSN URL")
		}
	}
	return nil
}
