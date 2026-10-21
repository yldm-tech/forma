import "server-only";
import { logger } from "@forma/logger";
import { runAuthzedBackfill } from "./backfill";
import { createAuthzedBackfillApply, createAuthzedBackfillNoopApply } from "./backfill-apply";
import { getAuthzedClient } from "./client";
import { isAuthzedEnabled } from "./config";
import { AUTHZED_MAX_PRUNED_RESOURCES_PER_RUN } from "./constants";
import {
  type TAuthzedReconciliationPass,
  recordAuthzedReconciliationAudit,
  recordAuthzedReconciliationPass,
  recordAuthzedReconciliationRepair,
} from "./metrics";
import { pruneAuthzedOutboxHistory, replayAuthzedOutboxDeadLetters } from "./outbox-repository";

/**
 * Run one sweep and record what it cost.
 *
 * Deliberately measures only a pass that returned. A pass that throws leaves no duration here, because
 * this job's contract with the scheduler is unchanged by instrumentation: the throw is the signal, and
 * swallowing it to emit a number would be a behaviour change wearing a measurement's clothes.
 */
const runInstrumentedPass = async (
  pass: TAuthzedReconciliationPass,
  run: () => Promise<Awaited<ReturnType<typeof runAuthzedBackfill>>>
): Promise<Awaited<ReturnType<typeof runAuthzedBackfill>>> => {
  const startedAt = Date.now();
  const result = await run();

  recordAuthzedReconciliationPass({
    durationMs: Date.now() - startedAt,
    failed: result.counters.failed,
    failureCodes: result.failures.map((failure) => failure.code),
    pass,
    status: result.status,
  });

  return result;
};

/**
 * Six-hour full audit. It repairs attributable missing/mismatched edges and never prunes unknown data.
 *
 * It runs in the web process, so it sweeps the whole graph on the request path's one-second channel
 * deadline and shares that channel with live permission checks. Whether that is a problem is an open
 * question, and it stays open until `forma_authzed_reconciliation_duration_seconds` and
 * `forma_authzed_reconciliation_failure_total` have said something. Three restructures are on the table,
 * each with the reading that would justify it and none worth doing before then:
 *
 * - **A second channel at `AUTHZED_BULK_REQUEST_TIMEOUT_MS`** — justified by a sustained
 *   `forma_authzed_reconciliation_failure_total{code="authzed_timeout"}` above zero, which is a page
 *   stranded on the deadline and an organization the sweep repaired nothing for. Nothing below that
 *   justifies it: the deadline is a property of the process today (see `configureAuthzedClientForBulkWork`
 *   in `client.ts`), so a second channel means a second connection and a second circuit breaker.
 * - **Bounded per-organization scopes** — the cheaper answer to the same reading, and the one to reach
 *   for first, since it needs no second channel. Also the answer if the duration histogram shows a pass
 *   in the minutes: a unit that fits the request deadline is schedulable, a whole-graph sweep is not.
 * - **Dropping the confirming pass, or narrowing it to what `apply` touched** — justified only if
 *   `confirm` is a material share of the total duration, which needs the `pass` attribute to show it.
 *   It costs something real: the audit counter currently attests to the state of the whole graph after
 *   repair, and a narrowed confirm changes what `forma_authzed_reconciliation_audit_total` means.
 */
export const processAuthzedScheduledReconciliationJob = async (): Promise<void> => {
  if (!isAuthzedEnabled()) return;
  const client = getAuthzedClient();
  const request = {
    maxPrune: AUTHZED_MAX_PRUNED_RESOURCES_PER_RUN,
    prune: false,
    scope: { kind: "all" },
  } as const;
  const observed = await runInstrumentedPass("dry_run", () =>
    runAuthzedBackfill({ ...request, mode: "dry_run" }, { apply: createAuthzedBackfillNoopApply(), client })
  );
  let result = observed;

  if (observed.status === "drifted") {
    const applied = await runInstrumentedPass("apply", () =>
      runAuthzedBackfill({ ...request, mode: "apply" }, { apply: createAuthzedBackfillApply(), client })
    );
    recordAuthzedReconciliationRepair({
      failed: applied.counters.failed,
      repaired: applied.counters.reconciled,
    });
    result = await runInstrumentedPass("confirm", () =>
      runAuthzedBackfill({ ...request, mode: "dry_run" }, { apply: createAuthzedBackfillNoopApply(), client })
    );
  }

  recordAuthzedReconciliationAudit({
    drift: observed.counters.missing + observed.counters.mismatchedPermissions,
    failures: result.counters.failed,
    status: result.status,
  });

  if (result.status !== "reconciled") {
    logger.warn(
      {
        component: "authzed",
        drift: result.counters.missing + result.counters.mismatchedPermissions,
        failures: result.counters.failed,
        operation: "scheduled_reconciliation",
        status: result.status,
      },
      "Scheduled AuthZed relationship reconciliation did not finish cleanly"
    );
  }

  // Runs whatever the audit concluded: it only deletes rows delivered more than a week ago, so it is
  // never the thing standing between an operator and evidence.
  await pruneAuthzedOutboxHistory();

  // A clean full audit means PostgreSQL and SpiceDB already agree everywhere, so whatever a dead
  // letter was trying to say has since been said by other means. Hand it back to the delivery loop
  // rather than leaving the freshness guard denying every authorization check until someone runs
  // `outbox replay` by hand — a dead-lettered revocation has no age bound in that guard on purpose.
  // A still-poisoned event simply re-dead-letters, so this is a six-hourly retry, not a loop. The
  // audit sweeps organizations, so an event for a deleted user or a cross-tenant pair may not be
  // covered by `reconciled`; replaying it anyway is idempotent and strictly better than denying.
  if (result.status === "reconciled") await replayAuthzedOutboxDeadLetters();
};
