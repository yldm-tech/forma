import "server-only";
import { metrics } from "@opentelemetry/api";

/**
 * Operational metrics for AuthZed relationship sync.
 *
 * Projection is best-effort by design: an outage never turns a successful PostgreSQL mutation into an
 * application error. That is the right trade-off, and it is also why drift can accumulate silently —
 * these counters are how an operator finds out, and what tells them to run `pnpm authzed:backfill`.
 *
 * Uses the OpenTelemetry metrics API, which the app already exports through both the Prometheus and
 * OTLP readers configured in `instrumentation-node.ts`. When neither is enabled `getMeter` returns a
 * no-op meter, so recording is safe with zero configuration and costs nothing.
 *
 * **Every attribute is a bounded, enumerable value — never an identifier.** Same rule as the logger:
 * these leave the deployment when an OTLP endpoint is configured, and an organization or user ID here
 * would be both a cardinality explosion and a privacy leak.
 *
 * Note this covers the always-on request path only. The backfill command is a short-lived process with
 * no scrape window and no flush, so its observability is the counters in its own JSON result and its
 * exit code.
 *
 * **A deliberate deviation from the semantic conventions, which prescribe dots as namespace delimiters
 * (`forma.authzed.projection.duration`) and say a unit need not appear in the name.** This app
 * configures the Prometheus reader *and* an OTLP reader at once, and the two derive a series name
 * differently: the Prometheus exporter sanitizes dots to underscores and appends no unit, while OTLP's
 * Prometheus translation appends the unit unless the name already carries it. Under the conventional
 * spelling the same instrument would surface as `..._duration` on a scrape and `..._duration_seconds`
 * through a collector — so the runbook could not name one series, which is exactly the defect this
 * naming replaced. Prometheus-style names with the unit spelled out are the only form both paths agree
 * on. Revisit if the Prometheus reader is ever dropped.
 */

const meter = metrics.getMeter("forma.authzed");

/** Projection outcomes, by operation and projector. Includes `disabled` so a misconfigured deployment is visible. */
const projectionTotal = meter.createCounter("forma_authzed_projection_total", {
  description: "AuthZed relationship projections by outcome",
});

/**
 * Seconds, with the unit spelled out in the instrument name.
 *
 * The semantic conventions prescribe seconds for durations, which rules out the `_ms` this started as.
 * The unit belongs in the *name* as well because the two exporters this app configures side by side
 * derive the series name differently: `@opentelemetry/exporter-prometheus` appends only `_total`, to
 * monotonic sums, and emits the unit as a `# UNIT` comment rather than a suffix, while OTLP's Prometheus
 * translation appends the unit — skipping it when the name already carries it. Naming it `_seconds` is
 * therefore the one spelling both paths agree on, and the runbook's alert queries can name a single
 * series. Leaving the unit out of the name would export `..._duration` on a scrape and
 * `..._duration_seconds` through a collector, which is how the runbook's histogram query came to match
 * nothing on the scrape path.
 */
const projectionDuration = meter.createHistogram("forma_authzed_projection_duration_seconds", {
  // The SDK's default boundaries are `[0, 5, 10, 25, … 10000]` — a millisecond scale. Recording seconds
  // against them puts every healthy projection in the single `(0, 5]` bucket, and `histogram_quantile`
  // interpolates within a bucket: a p95 over observations that are all ~100ms reports something close to
  // 4.75s, so the runbook's `> 0.5` alert would fire continuously on healthy traffic. These are the
  // semantic conventions' second-scale boundaries, which include 0.5 exactly so the alert threshold
  // falls on a boundary rather than inside a bucket.
  advice: {
    explicitBucketBoundaries: [0.005, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 0.75, 1, 2.5, 5, 7.5, 10],
  },
  description: "Duration of AuthZed relationship projections",
  unit: "s",
});

/**
 * Requests that exhausted their retry budget.
 *
 * The signal that distinguishes a blip from an outage: a sustained rate here means relationships are
 * being dropped and a backfill will be needed once the cause is resolved.
 */
const requestFailuresTotal = meter.createCounter("forma_authzed_request_failures_total", {
  description: "AuthZed requests that failed after exhausting retries",
});

/** Retries scheduled. Elevated but non-failing means SpiceDB is degraded rather than down. */
const requestRetriesTotal = meter.createCounter("forma_authzed_request_retries_total", {
  description: "AuthZed requests retried after a retryable failure",
});

/**
 * Request-path circuit breaker state, as a state set: the state in force reports 1 and the other two
 * report 0, so an alert can name one series (`forma_authzed_request_circuit_state{state="open"} == 1`).
 *
 * The signal the runbook has no equivalent of today — "degraded" is currently only inferable from the
 * ratio of retries to failures. Written on transition only, so a process that never failed exports no
 * series at all rather than a permanently reassuring zero.
 */
const requestCircuitState = meter.createGauge("forma_authzed_request_circuit_state", {
  description: "Request-path AuthZed circuit breaker state, 1 for the state in force",
  unit: "{state}",
});

/** Checks refused locally by the open circuit. These never reached SpiceDB, so they are not failures. */
const requestShortCircuitsTotal = meter.createCounter("forma_authzed_request_short_circuits_total", {
  description: "AuthZed requests refused locally by an open request-path circuit",
});

const outboxDeliveryTotal = meter.createCounter("forma_authzed_projection_outbox_delivery_total", {
  description: "Authorization projection outbox events processed by outcome",
});

const outboxDeliveryDuration = meter.createHistogram(
  "forma_authzed_projection_outbox_delivery_duration_seconds",
  {
    advice: {
      explicitBucketBoundaries: [0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30],
    },
    description: "Duration of an authorization projection outbox delivery batch",
    unit: "s",
  }
);

const reconciliationAuditTotal = meter.createCounter("forma_authzed_reconciliation_audit_total", {
  description: "Scheduled authorization relationship audits by outcome",
});

const reconciliationDriftTotal = meter.createCounter("forma_authzed_reconciliation_drift_total", {
  description: "Attributable relationship differences observed by scheduled audits",
});

const reconciliationRepairTotal = meter.createCounter("forma_authzed_reconciliation_repair_total", {
  description: "Attributable relationship repair results from scheduled reconciliation",
});

/**
 * Wall-clock cost of one pass of the scheduled reconciliation, by pass and outcome.
 *
 * The job is the largest recurring unit of work the web process runs — a full-graph sweep, in-process,
 * on whatever channel deadline the process happens to hold — and until this instrument existed it
 * reported only what it found, never what it cost. That left an open question the tree answers two ways:
 * `client.ts` argues the request-path deadline strands a sweep on its first slow page, while
 * `constants.ts` argues a 250-relationship page leaves generous headroom under it. Both are assertions;
 * neither is a measurement, and the scheduled job runs at the request deadline either way. This is the
 * measurement, and `reconciliationFailureTotal` below is what says whether a page was stranded.
 *
 * Seconds, with the unit in the name, for the reason the projection histogram spells out at length.
 */
const reconciliationDuration = meter.createHistogram("forma_authzed_reconciliation_duration_seconds", {
  // A different scale from every other histogram here, because this measures a sweep rather than a call.
  // A pass enumerates every organization and streams every managed relationship, so the interesting
  // range runs from seconds on an empty deployment to minutes on a large one, and the top boundary sits
  // an order of magnitude below the six-hour period so a pass approaching its own cadence is visible
  // rather than buried in an overflow bucket.
  advice: {
    explicitBucketBoundaries: [1, 5, 10, 30, 60, 120, 300, 600, 1_800, 3_600],
  },
  description: "Duration of one pass of the scheduled authorization reconciliation",
  unit: "s",
});

/**
 * Reconciliation unit failures by error code and pass.
 *
 * `forma_authzed_reconciliation_drift_total{kind="failure"}` already carries the total, but a total is
 * exactly what cannot answer the question the duration histogram raises: `authzed_timeout` means a page
 * ran through the channel deadline and the sweep repaired nothing for that organization, while every
 * other code means the sweep reached SpiceDB and something else went wrong. Those call for opposite
 * responses, and the audit counter cannot tell them apart.
 */
const reconciliationFailureTotal = meter.createCounter("forma_authzed_reconciliation_failure_total", {
  description: "Scheduled authorization reconciliation unit failures by error code",
});

const revocationDeliveryDuration = meter.createHistogram(
  "forma_authzed_projection_revocation_delivery_duration_seconds",
  {
    advice: {
      explicitBucketBoundaries: [0.1, 0.5, 1, 2.5, 5, 10, 15, 30, 45, 60, 120, 300],
    },
    description: "Time from a committed authorization revocation to successful SpiceDB delivery",
    unit: "s",
  }
);

const outboxStatus = meter.createGauge("forma_authzed_projection_outbox_status", {
  description: "Point-in-time authorization projection outbox counts by bounded state",
  unit: "{event}",
});

const outboxOldestPendingAge = meter.createGauge(
  "forma_authzed_projection_outbox_oldest_pending_age_seconds",
  {
    description: "Point-in-time age of the oldest pending authorization projection event",
    unit: "s",
  }
);

export type TAuthzedProjectionMetric = Readonly<{
  durationMs: number;
  operation: string;
  projection: string;
  status: "disabled" | "failed" | "projected";
}>;

export const recordAuthzedProjection = ({
  durationMs,
  operation,
  projection,
  status,
}: TAuthzedProjectionMetric): void => {
  const attributes = { operation, projection, status };
  projectionTotal.add(1, attributes);

  // `disabled` short-circuits before any work, so its duration is a structural zero rather than a
  // measurement. Recording it would drag the latency distribution of every quantile toward zero on a
  // deployment that has AuthZed switched off — and latency is the signal the runbook calls user-visible.
  if (status !== "disabled") {
    projectionDuration.record(durationMs / 1000, attributes);
  }
};

export type TAuthzedRequestFailureMetric = Readonly<{
  code: string;
  operation: string;
  retryable: boolean;
}>;

export const recordAuthzedRequestFailure = ({
  code,
  operation,
  retryable,
}: TAuthzedRequestFailureMetric): void => {
  requestFailuresTotal.add(1, { code, operation, retryable });
};

export const recordAuthzedRequestRetry = ({
  code,
  operation,
}: Readonly<{ code: string; operation: string }>): void => {
  requestRetriesTotal.add(1, { code, operation });
};

export type TAuthzedCircuitStateMetric = "closed" | "half_open" | "open";

const AUTHZED_CIRCUIT_STATES = ["closed", "half_open", "open"] as const;

export const recordAuthzedRequestCircuitState = (state: TAuthzedCircuitStateMetric): void => {
  for (const candidate of AUTHZED_CIRCUIT_STATES) {
    requestCircuitState.record(candidate === state ? 1 : 0, { state: candidate });
  }
};

export const recordAuthzedRequestShortCircuit = ({
  code,
  operation,
}: Readonly<{ code: string; operation: string }>): void => {
  requestShortCircuitsTotal.add(1, { code, operation });
};

export const recordAuthzedOutboxDelivery = ({
  count,
  durationMs,
  status,
}: Readonly<{
  count: number;
  durationMs: number;
  status: "delivered" | "failed";
}>): void => {
  try {
    outboxDeliveryTotal.add(count, { status });
    outboxDeliveryDuration.record(durationMs / 1000, { status });
  } catch {
    // Observability cannot turn an already-committed delivery result into an outbox failure.
  }
};

export const recordAuthzedRevocationDelivery = (durationMs: number): void => {
  try {
    revocationDeliveryDuration.record(Math.max(0, durationMs) / 1_000);
  } catch {
    // Observability cannot turn an already-delivered revocation into an outbox failure.
  }
};

export const recordAuthzedOutboxStatus = ({
  deadLettered,
  oldestPendingAgeSeconds,
  pending,
  revocationsPastCritical,
  revocationsPastWarning,
}: Readonly<{
  deadLettered: number;
  oldestPendingAgeSeconds: number | null;
  pending: number;
  revocationsPastCritical: number;
  revocationsPastWarning: number;
}>): void => {
  try {
    outboxStatus.record(pending, { state: "pending" });
    outboxStatus.record(deadLettered, { state: "dead_lettered" });
    outboxStatus.record(revocationsPastWarning, { state: "revocation_warning" });
    outboxStatus.record(revocationsPastCritical, { state: "revocation_critical" });
    outboxOldestPendingAge.record(oldestPendingAgeSeconds ?? 0);
  } catch {
    // A metrics exporter failure must not discard the caller's drain result.
  }
};

export const recordAuthzedReconciliationAudit = ({
  drift,
  failures,
  status,
}: Readonly<{
  drift: number;
  failures: number;
  status: "drifted" | "failed" | "reconciled";
}>): void => {
  try {
    reconciliationAuditTotal.add(1, { status });
    if (drift > 0) reconciliationDriftTotal.add(drift, { kind: "attributable" });
    if (failures > 0) reconciliationDriftTotal.add(failures, { kind: "failure" });
  } catch {
    // Pruning and dead-letter recovery must still run when the exporter is unavailable.
  }
};

export const recordAuthzedReconciliationRepair = ({
  failed,
  repaired,
}: Readonly<{ failed: number; repaired: number }>): void => {
  try {
    if (repaired > 0) reconciliationRepairTotal.add(repaired, { status: "repaired" });
    if (failed > 0) reconciliationRepairTotal.add(failed, { status: "failed" });
  } catch {
    // The second verification audit must still run when a metrics exporter is unavailable.
  }
};

/**
 * Which of the scheduled job's three sweeps this was.
 *
 * `confirm` is the dry run that re-reads the graph after a repair, and it is deliberately distinct from
 * the opening `dry_run`: the two do identical work but answer different questions, and a deployment
 * where only `confirm` is slow is a deployment where repair, not observation, is what costs.
 */
export type TAuthzedReconciliationPass = "apply" | "confirm" | "dry_run";

/**
 * The `code` attributed to failures the run counted but did not name.
 *
 * A backfill result carries every failure's code in a list capped at 100 entries, while its `failed`
 * counter carries the true total — so on a badly broken instance the codes account for less than the
 * count. Booking the difference under one bounded sentinel keeps the counter's sum equal to `failed`,
 * which is what lets it be compared against `forma_authzed_reconciliation_drift_total{kind="failure"}`
 * at all. Silently dropping the remainder would make the two disagree exactly when it matters most.
 */
const RECONCILIATION_FAILURE_CODE_UNREPORTED = "unreported";

export const recordAuthzedReconciliationPass = ({
  durationMs,
  failed,
  failureCodes,
  pass,
  status,
}: Readonly<{
  durationMs: number;
  failed: number;
  failureCodes: ReadonlyArray<string>;
  pass: TAuthzedReconciliationPass;
  status: "drifted" | "failed" | "reconciled";
}>): void => {
  try {
    reconciliationDuration.record(Math.max(0, durationMs) / 1_000, { pass, status });

    const countsByCode = new Map<string, number>();
    for (const code of failureCodes) {
      countsByCode.set(code, (countsByCode.get(code) ?? 0) + 1);
    }
    const unreported = Math.max(0, failed - failureCodes.length);
    if (unreported > 0) countsByCode.set(RECONCILIATION_FAILURE_CODE_UNREPORTED, unreported);

    for (const [code, count] of countsByCode) {
      reconciliationFailureTotal.add(count, { code, pass });
    }
  } catch {
    // Observability cannot turn a completed sweep into a failed one: the pruning and dead-letter
    // recovery that follow this pass matter more than the measurement of it.
  }
};
