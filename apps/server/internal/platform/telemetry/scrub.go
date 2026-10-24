package telemetry

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
)

// scrubbedAttributes are the HTTP semantic convention attributes that can carry a secret or personal data. A raw path can hold a token (a personal survey link /c/<token>, a reset link), the query string anything, and the client and peer addresses identify a person. http.route, the matched pattern, says which endpoint was called without any of that.
var scrubbedAttributes = map[attribute.Key]bool{
	semconv.URLPathKey:            true,
	semconv.URLQueryKey:           true,
	semconv.URLFullKey:            true,
	semconv.ClientAddressKey:      true,
	semconv.ClientPortKey:         true,
	semconv.NetworkPeerAddressKey: true,
	semconv.NetworkPeerPortKey:    true,
	semconv.ExceptionMessageKey:   true,
}

// scrubbingExporter removes what must never leave the deployment from every span before it is exported: the attributes above, and the status description, which is an error's message.
type scrubbingExporter struct {
	sdktrace.SpanExporter
}

func (e scrubbingExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	scrubbed := make([]sdktrace.ReadOnlySpan, len(spans))
	for i, s := range spans {
		scrubbed[i] = scrubbedSpan{s}
	}
	return e.SpanExporter.ExportSpans(ctx, scrubbed)
}

type scrubbedSpan struct {
	sdktrace.ReadOnlySpan
}

func (s scrubbedSpan) Attributes() []attribute.KeyValue {
	return scrub(s.ReadOnlySpan.Attributes())
}

func (s scrubbedSpan) Status() sdktrace.Status {
	return sdktrace.Status{Code: s.ReadOnlySpan.Status().Code}
}

func (s scrubbedSpan) Events() []sdktrace.Event {
	events := s.ReadOnlySpan.Events()
	out := make([]sdktrace.Event, len(events))
	for i, e := range events {
		e.Attributes = scrub(e.Attributes)
		out[i] = e
	}
	return out
}

func scrub(attrs []attribute.KeyValue) []attribute.KeyValue {
	kept := make([]attribute.KeyValue, 0, len(attrs))
	for _, a := range attrs {
		if !scrubbedAttributes[a.Key] {
			kept = append(kept, a)
		}
	}
	return kept
}
