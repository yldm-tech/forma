import "server-only";
import { metrics } from "@opentelemetry/api";
import { logger } from "@forma/logger";
import type { AITracingFeature } from "@/lib/posthog/ai-tracing-feature";

/**
 * What one AI generation cost, recorded at the single point all AI traffic funnels through.
 *
 * Every helper in `@forma/ai` already returns `usage` and no caller reads it, so an operator cannot
 * answer what a feature spends — the numbers existed only inside `AIOutputTokenLimitError`, which is
 * the failure path. This is the success path.
 *
 * Two sinks, because neither reaches every install on its own. The counters go to the OpenTelemetry
 * meter the app already runs (`instrumentation-node.ts`), which exports nothing unless the Prometheus
 * reader or an OTLP endpoint is configured; the log line is what an install that only ships stdout to
 * a log aggregator can read, and it needs `LOG_LEVEL=info` because production defaults to `warn`.
 *
 * **Never the prompt or the response.** A token count, a model id, and the organization the spend
 * belongs to. The organization is in the log line only: as a metric attribute it is unbounded
 * cardinality on a multi-tenant install, and the same rule the AuthZed metrics document applies here.
 */

const meter = metrics.getMeter("forma.ai");

/**
 * `input` and `output` are disjoint, so a sum over `kind` is the call's total billed tokens. That is
 * the whole reason reasoning tokens are a separate instrument rather than a third `kind` — the
 * provider reports them as a share *of* `output`, and counting them here would inflate every total.
 */
const tokensTotal = meter.createCounter("forma_ai_tokens_total", {
  description: "Tokens billed by the configured AI provider, by disjoint input/output kind",
});

/** A subset of the `output` kind above, never additive with it. */
const reasoningTokensTotal = meter.createCounter("forma_ai_reasoning_tokens_total", {
  description: "Reasoning tokens billed by the configured AI provider, a subset of output tokens",
});

/** The denominator: tokens per generation is what makes the hard-coded output caps tunable. */
const generationsTotal = meter.createCounter("forma_ai_generations_total", {
  description: "AI generations that reached the provider and completed",
});

/**
 * `unknown` covers the API-key callers, which carry no tracing context and so name no feature. A
 * bounded literal rather than an absent attribute, so the series is still summable.
 */
export type TAIUsageFeature = AITracingFeature | "unknown";

export type TAIUsageRecord = Readonly<{
  organizationId: string;
  feature: TAIUsageFeature;
  model: string;
  /** Flattened from the SDK's `LanguageModelUsage`, whose every field is optional per provider. */
  inputTokens: number | undefined;
  outputTokens: number | undefined;
  reasoningTokens: number | undefined;
}>;

export const recordAIGenerationUsage = ({
  organizationId,
  feature,
  model,
  inputTokens,
  outputTokens,
  reasoningTokens,
}: TAIUsageRecord): void => {
  // Two guards rather than one: the log line and the meter are independent sinks, and an install
  // usually has only one of them wired up. A shared guard would let a broken pino transport take the
  // counters down with it, and vice versa — losing the one signal the install actually collects.
  try {
    logger.info(
      { organizationId, feature, model, inputTokens, outputTokens, reasoningTokens },
      "AI generation usage"
    );
  } catch {
    // Telemetry must never turn a successful generation into a failed one.
  }

  try {
    const attributes = { feature, model };

    generationsTotal.add(1, attributes);
    if (inputTokens !== undefined) tokensTotal.add(inputTokens, { ...attributes, kind: "input" });
    if (outputTokens !== undefined) tokensTotal.add(outputTokens, { ...attributes, kind: "output" });
    if (reasoningTokens !== undefined) reasoningTokensTotal.add(reasoningTokens, attributes);
  } catch {
    // Same contract: the caller invokes this outside the try/catch that classifies provider failures
    // precisely so an instrumentation bug cannot be reported to the user as a provider error.
  }
};
