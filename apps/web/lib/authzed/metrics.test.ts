import { beforeEach, describe, expect, test, vi } from "vitest";
import { AUTHZED_ERROR_CODES } from "./errors";

const counters = new Map<string, { add: ReturnType<typeof vi.fn> }>();
const gauges = new Map<string, { record: ReturnType<typeof vi.fn> }>();
const histograms = new Map<string, { record: ReturnType<typeof vi.fn> }>();

vi.mock("@opentelemetry/api", () => ({
  metrics: {
    getMeter: vi.fn(() => ({
      createCounter: vi.fn((name: string) => {
        const instrument = { add: vi.fn() };
        counters.set(name, instrument);
        return instrument;
      }),
      createGauge: vi.fn((name: string) => {
        const instrument = { record: vi.fn() };
        gauges.set(name, instrument);
        return instrument;
      }),
      createHistogram: vi.fn((name: string) => {
        const instrument = { record: vi.fn() };
        histograms.set(name, instrument);
        return instrument;
      }),
    })),
  },
}));

const {
  recordAuthzedOutboxDelivery,
  recordAuthzedOutboxStatus,
  recordAuthzedProjection,
  recordAuthzedReconciliationAudit,
  recordAuthzedReconciliationPass,
  recordAuthzedReconciliationRepair,
  recordAuthzedRequestFailure,
  recordAuthzedRequestRetry,
  recordAuthzedRevocationDelivery,
} = await import("./metrics");

const counter = (name: string) => counters.get(name)!;
const histogram = (name: string) => histograms.get(name)!;

beforeEach(() => {
  for (const instrument of counters.values()) {
    instrument.add.mockClear();
  }
  for (const instrument of histograms.values()) {
    instrument.record.mockClear();
  }
  for (const instrument of gauges.values()) {
    instrument.record.mockClear();
  }
});

describe("recordAuthzedProjection", () => {
  test.each(["projected", "failed"] as const)("records a %s outcome and its duration", (status) => {
    recordAuthzedProjection({
      durationMs: 4200,
      operation: "reconcile_organization_memberships",
      projection: "organization_membership",
      status,
    });

    const attributes = {
      operation: "reconcile_organization_memberships",
      projection: "organization_membership",
      status,
    };
    expect(counter("forma_authzed_projection_total").add).toHaveBeenCalledWith(1, attributes);
    // Seconds, per the OpenTelemetry duration convention.
    expect(histogram("forma_authzed_projection_duration_seconds").record).toHaveBeenCalledWith(
      4.2,
      attributes
    );
  });

  test("counts a disabled projection but keeps its structural zero out of the latency histogram", () => {
    recordAuthzedProjection({
      durationMs: 0,
      operation: "reconcile_api_key_relationships",
      projection: "api_key",
      status: "disabled",
    });

    expect(counter("forma_authzed_projection_total").add).toHaveBeenCalledOnce();
    expect(histogram("forma_authzed_projection_duration_seconds").record).not.toHaveBeenCalled();
  });

  test("names the duration instrument so both exporters produce the series the runbook queries", () => {
    // The two exporters configured side by side derive the series name differently: the Prometheus
    // exporter appends only `_total` and emits the unit as a comment, while OTLP's translation appends
    // the unit unless the name already carries it. `_seconds` is the one spelling both agree on — and
    // the runbook's histogram_quantile query names exactly this series.
    expect(histograms.has("forma_authzed_projection_duration_seconds")).toBe(true);
    // The unit-less name would export as `..._duration` on a scrape, matching nothing the runbook asks
    // for; `_ms` was the original defect.
    expect(histograms.has("forma_authzed_projection_duration")).toBe(false);
    expect(histograms.has("forma_authzed_projection_duration_ms")).toBe(false);
  });
});

describe("recordAuthzedRequestFailure", () => {
  test("records the sanitized code, operation, and retryability", () => {
    recordAuthzedRequestFailure({
      code: AUTHZED_ERROR_CODES.UNAVAILABLE,
      operation: "write_relationships",
      retryable: true,
    });

    expect(counter("forma_authzed_request_failures_total").add).toHaveBeenCalledWith(1, {
      code: AUTHZED_ERROR_CODES.UNAVAILABLE,
      operation: "write_relationships",
      retryable: true,
    });
  });
});

describe("recordAuthzedRequestRetry", () => {
  test("records retries separately from failures", () => {
    // A retry that later succeeds never reaches the failure counter, so a degraded SpiceDB would
    // otherwise be invisible until it started dropping writes outright.
    recordAuthzedRequestRetry({
      code: AUTHZED_ERROR_CODES.TIMEOUT,
      operation: "read_relationships",
    });

    expect(counter("forma_authzed_request_retries_total").add).toHaveBeenCalledWith(1, {
      code: AUTHZED_ERROR_CODES.TIMEOUT,
      operation: "read_relationships",
    });
    expect(counter("forma_authzed_request_failures_total").add).not.toHaveBeenCalled();
  });
});

describe("recordAuthzedOutboxStatus", () => {
  test("records point-in-time queue state without identifier attributes", () => {
    recordAuthzedOutboxStatus({
      deadLettered: 2,
      oldestPendingAgeSeconds: 47,
      pending: 11,
      revocationsPastCritical: 1,
      revocationsPastWarning: 3,
    });

    const status = gauges.get("forma_authzed_projection_outbox_status")!;
    expect(status.record.mock.calls).toEqual([
      [11, { state: "pending" }],
      [2, { state: "dead_lettered" }],
      [3, { state: "revocation_warning" }],
      [1, { state: "revocation_critical" }],
    ]);
    expect(
      gauges.get("forma_authzed_projection_outbox_oldest_pending_age_seconds")!.record
    ).toHaveBeenCalledWith(47);
  });
});

describe("direct-authority recovery metrics", () => {
  test("records exact revocation propagation in seconds without attributes", () => {
    recordAuthzedRevocationDelivery(12_500);

    expect(
      histogram("forma_authzed_projection_revocation_delivery_duration_seconds").record
    ).toHaveBeenCalledWith(12.5);
  });

  test("records repaired and failed relationship counts separately", () => {
    recordAuthzedReconciliationRepair({ failed: 2, repaired: 7 });

    expect(counter("forma_authzed_reconciliation_repair_total").add.mock.calls).toEqual([
      [7, { status: "repaired" }],
      [2, { status: "failed" }],
    ]);
  });

  test("does not let exporter failures alter revocation delivery or repair", () => {
    histogram("forma_authzed_projection_revocation_delivery_duration_seconds").record.mockImplementationOnce(
      () => {
        throw new Error("exporter unavailable");
      }
    );
    counter("forma_authzed_reconciliation_repair_total").add.mockImplementationOnce(() => {
      throw new Error("exporter unavailable");
    });

    expect(() => recordAuthzedRevocationDelivery(1)).not.toThrow();
    expect(() => recordAuthzedReconciliationRepair({ failed: 0, repaired: 1 })).not.toThrow();
  });

  test("does not let exporter failures alter delivery, drain, or audit results", () => {
    counter("forma_authzed_projection_outbox_delivery_total").add.mockImplementationOnce(() => {
      throw new Error("exporter unavailable");
    });
    gauges.get("forma_authzed_projection_outbox_status")!.record.mockImplementationOnce(() => {
      throw new Error("exporter unavailable");
    });
    counter("forma_authzed_reconciliation_audit_total").add.mockImplementationOnce(() => {
      throw new Error("exporter unavailable");
    });

    expect(() => recordAuthzedOutboxDelivery({ count: 1, durationMs: 2, status: "delivered" })).not.toThrow();
    expect(() =>
      recordAuthzedOutboxStatus({
        deadLettered: 0,
        oldestPendingAgeSeconds: 1,
        pending: 1,
        revocationsPastCritical: 0,
        revocationsPastWarning: 0,
      })
    ).not.toThrow();
    expect(() =>
      recordAuthzedReconciliationAudit({ drift: 0, failures: 0, status: "reconciled" })
    ).not.toThrow();
  });
});

describe("recordAuthzedReconciliationPass", () => {
  test("records the pass duration in seconds, by pass and outcome", () => {
    recordAuthzedReconciliationPass({
      durationMs: 92_500,
      failed: 0,
      failureCodes: [],
      pass: "confirm",
      status: "reconciled",
    });

    expect(histogram("forma_authzed_reconciliation_duration_seconds").record).toHaveBeenCalledWith(92.5, {
      pass: "confirm",
      status: "reconciled",
    });
    expect(counter("forma_authzed_reconciliation_failure_total").add).not.toHaveBeenCalled();
  });

  test("aggregates failures by code so a stranded page is separable from every other fault", () => {
    recordAuthzedReconciliationPass({
      durationMs: 1_000,
      failed: 3,
      failureCodes: [AUTHZED_ERROR_CODES.TIMEOUT, AUTHZED_ERROR_CODES.INTERNAL, AUTHZED_ERROR_CODES.TIMEOUT],
      pass: "dry_run",
      status: "failed",
    });

    expect(counter("forma_authzed_reconciliation_failure_total").add.mock.calls).toEqual([
      [2, { code: AUTHZED_ERROR_CODES.TIMEOUT, pass: "dry_run" }],
      [1, { code: AUTHZED_ERROR_CODES.INTERNAL, pass: "dry_run" }],
    ]);
  });

  // A backfill result names at most 100 failures but counts them all. Dropping the difference would put
  // this counter permanently below `forma_authzed_reconciliation_drift_total{kind="failure"}` on exactly
  // the broken instance where the two are being compared.
  test("books failures the run counted but did not name under one bounded code", () => {
    recordAuthzedReconciliationPass({
      durationMs: 1_000,
      failed: 140,
      failureCodes: Array.from({ length: 100 }, () => AUTHZED_ERROR_CODES.TIMEOUT),
      pass: "apply",
      status: "failed",
    });

    const recorded = counter("forma_authzed_reconciliation_failure_total").add.mock.calls;
    expect(recorded).toEqual([
      [100, { code: AUTHZED_ERROR_CODES.TIMEOUT, pass: "apply" }],
      [40, { code: "unreported", pass: "apply" }],
    ]);
    expect(recorded.reduce((total, [count]) => total + (count as number), 0)).toBe(140);
  });

  test("clamps a negative duration rather than exporting it", () => {
    // `Date.now()` is not monotonic: an NTP step backwards mid-sweep would otherwise put a negative
    // observation into the histogram's sum, which no quantile can recover from.
    recordAuthzedReconciliationPass({
      durationMs: -5,
      failed: 0,
      failureCodes: [],
      pass: "dry_run",
      status: "reconciled",
    });

    expect(histogram("forma_authzed_reconciliation_duration_seconds").record).toHaveBeenCalledWith(
      0,
      expect.any(Object)
    );
  });

  test("does not let an exporter failure turn a completed sweep into a failed one", () => {
    histogram("forma_authzed_reconciliation_duration_seconds").record.mockImplementationOnce(() => {
      throw new Error("exporter unavailable");
    });

    expect(() =>
      recordAuthzedReconciliationPass({
        durationMs: 1,
        failed: 0,
        failureCodes: [],
        pass: "dry_run",
        status: "reconciled",
      })
    ).not.toThrow();
  });
});

describe("attribute cardinality", () => {
  test("never carries an identifier", () => {
    // These attributes leave the deployment when an OTLP endpoint is configured. An organization or
    // user ID here would be both a cardinality explosion and a privacy leak — the same rule the
    // logger follows.
    recordAuthzedProjection({
      durationMs: 1,
      operation: "reconcile_api_key_relationships",
      projection: "api_key",
      status: "failed",
    });
    recordAuthzedRequestFailure({
      code: AUTHZED_ERROR_CODES.INTERNAL,
      operation: "write_relationships",
      retryable: false,
    });
    recordAuthzedOutboxStatus({
      deadLettered: 0,
      oldestPendingAgeSeconds: null,
      pending: 1,
      revocationsPastCritical: 0,
      revocationsPastWarning: 0,
    });
    recordAuthzedReconciliationRepair({ failed: 1, repaired: 2 });
    recordAuthzedReconciliationPass({
      durationMs: 1,
      failed: 1,
      failureCodes: [AUTHZED_ERROR_CODES.TIMEOUT],
      pass: "apply",
      status: "failed",
    });
    recordAuthzedRevocationDelivery(1);

    const recordedAttributes = [
      ...counter("forma_authzed_projection_total").add.mock.calls,
      ...counter("forma_authzed_request_failures_total").add.mock.calls,
      ...counter("forma_authzed_reconciliation_repair_total").add.mock.calls,
      ...counter("forma_authzed_reconciliation_failure_total").add.mock.calls,
      ...histogram("forma_authzed_reconciliation_duration_seconds").record.mock.calls,
      ...gauges.get("forma_authzed_projection_outbox_status")!.record.mock.calls,
    ].flatMap(([, attributes]) => Object.keys(attributes as object));

    expect([...new Set(recordedAttributes)].sort()).toEqual([
      "code",
      "operation",
      "pass",
      "projection",
      "retryable",
      "state",
      "status",
    ]);
  });
});
