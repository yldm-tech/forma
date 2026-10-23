package telemetry

import (
	"context"
	"log/slog"
	"time"

	"github.com/getsentry/sentry-go"

	"github.com/yldm-tech/forma/server/internal/platform/buildinfo"
	"github.com/yldm-tech/forma/server/internal/platform/config"
)

// sentryFlushTimeout bounds how long shutdown waits for queued error reports.
const sentryFlushTimeout = 5 * time.Second

// SetupSentry installs the global Sentry client when SENTRY_DSN is set, with forma2's settings: no tracing (OpenTelemetry owns it), no default PII, no client reports, the release version and SENTRY_ENVIRONMENT. Every event is scrubbed of the request and the user before it is sent. The returned function flushes queued events; call it before the process exits.
func SetupSentry(ctx context.Context, cfg config.Config, log *slog.Logger) func() {
	if cfg.Telemetry.SentryDSN == "" {
		log.WarnContext(ctx, "Sentry DSN not found, Sentry will be disabled on the server")
		return func() {}
	}
	if err := sentry.Init(sentryOptions(cfg.Telemetry)); err != nil {
		log.ErrorContext(ctx, "Sentry could not be initialized, Sentry will be disabled on the server", "err", err)
		return func() {}
	}
	log.InfoContext(ctx, "Sentry DSN found, enabling Sentry on the server")
	return func() { sentry.Flush(sentryFlushTimeout) }
}

func sentryOptions(cfg config.TelemetryConfig) sentry.ClientOptions {
	release := ""
	if buildinfo.Released() {
		release = buildinfo.Version
	}
	return sentry.ClientOptions{
		Dsn:                  cfg.SentryDSN,
		Release:              release,
		Environment:          cfg.SentryEnvironment,
		TracesSampleRate:     0,
		SendDefaultPII:       false,
		DisableClientReports: true,
		BeforeSend:           scrubSentryEvent,
	}
}

// scrubSentryEvent drops what an event must never carry out of the deployment: the request (its URL, query, headers, cookies and body can hold tokens and answers), the user and breadcrumbs. What identifies a failure is set by the reporter as tags: the API version, the route pattern, the method and the request id.
func scrubSentryEvent(event *sentry.Event, _ *sentry.EventHint) *sentry.Event {
	event.Request = nil
	event.User = sentry.User{}
	event.Breadcrumbs = nil
	return event
}
