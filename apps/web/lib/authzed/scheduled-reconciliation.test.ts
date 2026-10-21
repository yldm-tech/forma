import { beforeEach, describe, expect, test, vi } from "vitest";
import { type TAuthzedBackfillResult, runAuthzedBackfill } from "./backfill";
import { isAuthzedEnabled } from "./config";
import {
  recordAuthzedReconciliationAudit,
  recordAuthzedReconciliationPass,
  recordAuthzedReconciliationRepair,
} from "./metrics";
import { pruneAuthzedOutboxHistory, replayAuthzedOutboxDeadLetters } from "./outbox-repository";
import { processAuthzedScheduledReconciliationJob } from "./scheduled-reconciliation";

vi.mock("@forma/logger", () => ({ logger: { warn: vi.fn() } }));
vi.mock("./backfill", () => ({ runAuthzedBackfill: vi.fn() }));
vi.mock("./backfill-apply", () => ({
  createAuthzedBackfillApply: vi.fn(() => ({ mode: "apply" })),
  createAuthzedBackfillNoopApply: vi.fn(() => ({ mode: "dry_run" })),
}));
vi.mock("./client", () => ({ getAuthzedClient: vi.fn(() => ({ client: true })) }));
vi.mock("./config", () => ({ isAuthzedEnabled: vi.fn() }));
vi.mock("./metrics", () => ({
  recordAuthzedReconciliationAudit: vi.fn(),
  recordAuthzedReconciliationPass: vi.fn(),
  recordAuthzedReconciliationRepair: vi.fn(),
}));
vi.mock("./outbox-repository", () => ({
  pruneAuthzedOutboxHistory: vi.fn(),
  replayAuthzedOutboxDeadLetters: vi.fn(),
}));

// Built complete rather than cast from a partial: the real result carries twelve counters and ten
// list fields, and a `as` over a three-key object only compiled because the cast silenced it. A zeroed
// base means a field added to the result type surfaces here as a type error rather than as `undefined`
// reaching the code under test.
const ZERO_COUNTERS: TAuthzedBackfillResult["counters"] = {
  failed: 0,
  ignored: 0,
  invalid: 0,
  mismatchedParents: 0,
  mismatchedPermissions: 0,
  missing: 0,
  orphaned: 0,
  pruned: 0,
  reconciled: 0,
  scanned: 0,
  skipped: 0,
  unmanaged: 0,
};

const result = (
  status: "drifted" | "failed" | "reconciled",
  missing = 0,
  mismatchedPermissions = 0,
  reconciled = 0,
  failures: ReadonlyArray<string> = status === "failed" ? ["authzed_internal"] : []
): TAuthzedBackfillResult => ({
  completedAtSnapshot: null,
  counters: { ...ZERO_COUNTERS, failed: failures.length, mismatchedPermissions, missing, reconciled },
  failures: failures.map((code) => ({ attempts: 1, code, organizationId: "org", retryable: false })),
  lastOrganizationId: null,
  mismatchedParents: [],
  mismatchedPermissions: [],
  mode: "apply",
  orphanScope: "known_resources",
  orphans: [],
  scope: "all",
  status,
  truncated: false,
  unmanaged: [],
});

describe("scheduled AuthZed reconciliation", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(isAuthzedEnabled).mockReturnValue(true);
  });

  test("does no database or AuthZed work when disabled", async () => {
    vi.mocked(isAuthzedEnabled).mockReturnValue(false);

    await processAuthzedScheduledReconciliationJob();

    expect(runAuthzedBackfill).not.toHaveBeenCalled();
  });

  test("stops after one clean dry-run audit", async () => {
    vi.mocked(runAuthzedBackfill).mockResolvedValue(result("reconciled"));

    await processAuthzedScheduledReconciliationJob();

    expect(runAuthzedBackfill).toHaveBeenCalledOnce();
    expect(runAuthzedBackfill).toHaveBeenCalledWith(
      expect.objectContaining({ mode: "dry_run", prune: false, scope: { kind: "all" } }),
      expect.any(Object)
    );
    expect(recordAuthzedReconciliationAudit).toHaveBeenCalledWith({
      drift: 0,
      failures: 0,
      status: "reconciled",
    });
    expect(pruneAuthzedOutboxHistory).toHaveBeenCalledOnce();
  });

  // A dead-lettered revocation denies every enforced authorization check with no age bound, so the
  // audit is the only thing standing between one poison event and an indefinite outage.
  test("returns dead letters to the delivery loop once an audit comes back clean", async () => {
    vi.mocked(runAuthzedBackfill).mockResolvedValue(result("reconciled"));

    await processAuthzedScheduledReconciliationJob();

    expect(replayAuthzedOutboxDeadLetters).toHaveBeenCalledOnce();
  });

  test("leaves dead letters alone while PostgreSQL and SpiceDB still disagree", async () => {
    for (const status of ["drifted", "failed"] as const) {
      vi.clearAllMocks();
      vi.mocked(isAuthzedEnabled).mockReturnValue(true);
      vi.mocked(runAuthzedBackfill).mockResolvedValue(result(status));

      await processAuthzedScheduledReconciliationJob();

      expect(replayAuthzedOutboxDeadLetters).not.toHaveBeenCalled();
      expect(pruneAuthzedOutboxHistory).toHaveBeenCalledOnce();
    }
  });

  test("repairs attributable drift and verifies it with a second dry run", async () => {
    vi.mocked(runAuthzedBackfill)
      .mockResolvedValueOnce(result("drifted", 2, 1))
      .mockResolvedValueOnce(result("drifted", 0, 0, 3))
      .mockResolvedValueOnce(result("reconciled"));

    await processAuthzedScheduledReconciliationJob();

    expect(vi.mocked(runAuthzedBackfill).mock.calls.map(([request]) => request.mode)).toEqual([
      "dry_run",
      "apply",
      "dry_run",
    ]);
    expect(recordAuthzedReconciliationAudit).toHaveBeenCalledWith({
      drift: 3,
      failures: 0,
      status: "reconciled",
    });
    expect(recordAuthzedReconciliationRepair).toHaveBeenCalledWith({ failed: 0, repaired: 3 });
  });

  // The sweep is the largest recurring unit of work the web process runs and reported only what it
  // found, never what it cost — so nobody could say whether it finishes inside the request-path channel
  // deadline it shares with live permission checks.
  test("reports what the one pass of a clean audit cost", async () => {
    vi.mocked(runAuthzedBackfill).mockResolvedValue(result("reconciled"));

    await processAuthzedScheduledReconciliationJob();

    expect(recordAuthzedReconciliationPass).toHaveBeenCalledOnce();
    expect(recordAuthzedReconciliationPass).toHaveBeenCalledWith({
      durationMs: expect.any(Number),
      failed: 0,
      failureCodes: [],
      pass: "dry_run",
      status: "reconciled",
    });
  });

  // `confirm` does identical work to the opening `dry_run`, so without distinct labels the histogram
  // cannot say whether a slow cycle is slow to observe or slow to repair — which is the reading that
  // decides whether the confirming pass is worth keeping.
  test("labels the observing, repairing, and confirming passes apart", async () => {
    vi.mocked(runAuthzedBackfill)
      .mockResolvedValueOnce(result("drifted", 2, 1))
      .mockResolvedValueOnce(result("drifted", 0, 0, 3))
      .mockResolvedValueOnce(result("reconciled"));

    await processAuthzedScheduledReconciliationJob();

    expect(vi.mocked(recordAuthzedReconciliationPass).mock.calls.map(([recorded]) => recorded.pass)).toEqual([
      "dry_run",
      "apply",
      "confirm",
    ]);
  });

  // A deadline-stranded page and real drift both land in `counters.failed`, and they call for opposite
  // responses: the first means the sweep never reached SpiceDB for that organization.
  test("carries each failure's code through so a stranded page is distinguishable from real drift", async () => {
    vi.mocked(runAuthzedBackfill).mockResolvedValue(
      result("failed", 0, 0, 0, ["authzed_timeout", "authzed_timeout", "authzed_internal"])
    );

    await processAuthzedScheduledReconciliationJob();

    expect(recordAuthzedReconciliationPass).toHaveBeenCalledWith(
      expect.objectContaining({
        failed: 3,
        failureCodes: ["authzed_timeout", "authzed_timeout", "authzed_internal"],
        status: "failed",
      })
    );
  });

  test("measures the pass rather than the job", async () => {
    vi.useFakeTimers();
    try {
      vi.mocked(runAuthzedBackfill).mockImplementation(async () => {
        vi.advanceTimersByTime(4_200);
        return result("reconciled");
      });

      await processAuthzedScheduledReconciliationJob();

      expect(recordAuthzedReconciliationPass).toHaveBeenCalledWith(
        expect.objectContaining({ durationMs: 4_200 })
      );
    } finally {
      vi.useRealTimers();
    }
  });
});
