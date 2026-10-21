import { metrics } from "@opentelemetry/api";
import {
  AggregationTemporality,
  InMemoryMetricExporter,
  MeterProvider,
  PeriodicExportingMetricReader,
} from "@opentelemetry/sdk-metrics";
import { afterEach, describe, expect, test } from "vitest";

/**
 * Bucket configuration, verified against the real SDK rather than by asserting the advice object.
 *
 * The advice is only a *hint*: whether it takes effect depends on the SDK honouring it, so asserting the
 * literal we passed in would restate the source and prove nothing. What is worth pinning is the
 * boundaries that end up on the exported data point.
 *
 * What this guards against is concrete. The SDK's default boundaries are `[0, 5, 10, 25, … 10000]`, a
 * millisecond scale, and this histogram records seconds. Under the defaults every healthy projection
 * lands in the single `(0, 5]` bucket, and because `histogram_quantile` interpolates *within* a bucket, a
 * p95 over observations that are all ~100ms reports something near 4.75s. The runbook's `> 0.5` alert
 * would then fire continuously on healthy traffic — worse than no alert, because it teaches its audience
 * to ignore it.
 */

const HISTOGRAM_NAME = "forma_authzed_projection_duration_seconds";

const recordOneProjection = async () => {
  const exporter = new InMemoryMetricExporter(AggregationTemporality.CUMULATIVE);
  // A long interval so nothing exports on a timer; `forceFlush` is what drives the export here.
  const reader = new PeriodicExportingMetricReader({ exporter, exportIntervalMillis: 60_000 });
  const provider = new MeterProvider({ readers: [reader] });
  metrics.setGlobalMeterProvider(provider);

  const { recordAuthzedProjection } = await import("./metrics");
  recordAuthzedProjection({
    durationMs: 100,
    operation: "reconcile_organization_memberships",
    projection: "organization_membership",
    status: "projected",
  });

  await reader.forceFlush();
  const histogram = exporter
    .getMetrics()
    .flatMap((resourceMetric) => resourceMetric.scopeMetrics)
    .flatMap((scopeMetric) => scopeMetric.metrics)
    .find((metric) => metric.descriptor.name === HISTOGRAM_NAME);

  return { provider, value: histogram?.dataPoints[0]?.value };
};

const RECONCILIATION_HISTOGRAM_NAME = "forma_authzed_reconciliation_duration_seconds";

const recordOneReconciliationPass = async (durationMs: number) => {
  const exporter = new InMemoryMetricExporter(AggregationTemporality.CUMULATIVE);
  const reader = new PeriodicExportingMetricReader({ exporter, exportIntervalMillis: 60_000 });
  const provider = new MeterProvider({ readers: [reader] });
  metrics.setGlobalMeterProvider(provider);

  const { recordAuthzedReconciliationPass } = await import("./metrics");
  recordAuthzedReconciliationPass({
    durationMs,
    failed: 0,
    failureCodes: [],
    pass: "dry_run",
    status: "reconciled",
  });

  await reader.forceFlush();
  const histogram = exporter
    .getMetrics()
    .flatMap((resourceMetric) => resourceMetric.scopeMetrics)
    .flatMap((scopeMetric) => scopeMetric.metrics)
    .find((metric) => metric.descriptor.name === RECONCILIATION_HISTOGRAM_NAME);

  return { provider, value: histogram?.dataPoints[0]?.value };
};

describe("projection duration histogram", () => {
  let shutdown: (() => Promise<void>) | undefined;

  afterEach(async () => {
    await shutdown?.();
    shutdown = undefined;
    metrics.disable();
  });

  test("exports second-scale buckets, so a sub-second p95 is measurable", async () => {
    const { provider, value } = await recordOneProjection();
    shutdown = () => provider.shutdown();

    const boundaries = (value as Readonly<{ buckets: Readonly<{ boundaries: number[] }> }> | undefined)
      ?.buckets.boundaries;

    expect(boundaries).toBeDefined();
    // The alert threshold has to fall on a boundary. Inside a bucket, the quantile it is compared against
    // is an interpolation across whatever range contains it.
    expect(boundaries).toContain(0.5);
    // Seconds, not milliseconds: the default scale reaches 10,000 and swallows every real observation.
    expect(Math.max(...(boundaries ?? []))).toBeLessThanOrEqual(10);
    // And enough resolution below the threshold for a healthy value to be distinguishable from it.
    expect((boundaries ?? []).filter((boundary) => boundary < 0.5)).toHaveLength(7);
  });

  test("records the observation in seconds, not milliseconds", async () => {
    const { provider, value } = await recordOneProjection();
    shutdown = () => provider.shutdown();

    // 100ms in, 0.1 recorded. A unit mismatch here would be invisible to the bucket assertions above.
    expect((value as Readonly<{ sum?: number }> | undefined)?.sum).toBeCloseTo(0.1);
  });
});

/**
 * The same hazard on a different scale. This histogram measures a full-graph sweep rather than a single
 * call, so the projection histogram's boundaries would be as wrong here as the SDK defaults are there:
 * every pass on any deployment past a handful of organizations would land in the final overflow bucket,
 * and a p95 over an overflow bucket is unbounded above. The point of this instrument is to say whether a
 * pass fits inside the request-path channel deadline it shares with live permission checks, which means
 * the seconds either side of that deadline have to be resolvable.
 */
describe("reconciliation duration histogram", () => {
  let shutdown: (() => Promise<void>) | undefined;

  afterEach(async () => {
    await shutdown?.();
    shutdown = undefined;
    metrics.disable();
  });

  test("resolves a sweep from seconds to the hour, in seconds", async () => {
    const { provider, value } = await recordOneReconciliationPass(92_500);
    shutdown = () => provider.shutdown();

    const boundaries = (value as Readonly<{ buckets: Readonly<{ boundaries: number[] }> }> | undefined)
      ?.buckets.boundaries;

    expect(boundaries).toBeDefined();
    // A pass that overran the one-second request-path deadline must be distinguishable from one that did
    // not, which is the whole question this instrument exists to settle.
    expect(boundaries).toContain(1);
    // And a minutes-long sweep must not be indistinguishable from an hours-long one.
    expect((boundaries ?? []).filter((boundary) => boundary > 60).length).toBeGreaterThanOrEqual(3);
    // 92.5s in, 92.5 recorded — seconds, like every other duration here.
    expect((value as Readonly<{ sum?: number }> | undefined)?.sum).toBeCloseTo(92.5);
  });
});
