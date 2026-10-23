package ai

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// usageMeters are forma2's AI usage counters (apps/web/lib/ai/usage-metrics.ts), the metric half of recordUsage. Their attributes are the feature and the model; the organization is in the log line only, since as an attribute it would be unbounded on a multi-tenant instance. Input and output tokens are disjoint kinds, so their sum is a call's billed total; reasoning tokens are a share of output and get a counter of their own.
type usageMeters struct {
	tokens          metric.Int64Counter
	reasoningTokens metric.Int64Counter
	generations     metric.Int64Counter
}

// Creating an instrument fails only on an invalid name or option, which these constants are not; a failure still yields a usable no-op instrument.
func newUsageMeters(mp metric.MeterProvider) *usageMeters {
	meter := mp.Meter("forma.ai")
	m := &usageMeters{}
	m.tokens, _ = meter.Int64Counter("forma_ai_tokens_total", metric.WithDescription("Tokens billed by the configured AI provider, by disjoint input/output kind"))
	m.reasoningTokens, _ = meter.Int64Counter("forma_ai_reasoning_tokens_total", metric.WithDescription("Reasoning tokens billed by the configured AI provider, a subset of output tokens"))
	m.generations, _ = meter.Int64Counter("forma_ai_generations_total", metric.WithDescription("AI generations that reached the provider and completed"))
	return m
}

// usageInstruments record through the global meter provider, which telemetry.Setup installs when an exporter is configured.
var usageInstruments = newUsageMeters(otel.GetMeterProvider())

func (m *usageMeters) record(ctx context.Context, feature, model string, u Usage) {
	attrs := []attribute.KeyValue{attribute.String("feature", feature), attribute.String("model", model)}
	m.generations.Add(ctx, 1, metric.WithAttributes(attrs...))
	if u.InputTokens != nil {
		m.tokens.Add(ctx, *u.InputTokens, metric.WithAttributes(append(attrs, attribute.String("kind", "input"))...))
	}
	if u.OutputTokens != nil {
		m.tokens.Add(ctx, *u.OutputTokens, metric.WithAttributes(append(attrs, attribute.String("kind", "output"))...))
	}
	if u.ReasoningTokens != nil {
		m.reasoningTokens.Add(ctx, *u.ReasoningTokens, metric.WithAttributes(attrs...))
	}
}
