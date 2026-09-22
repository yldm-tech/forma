import type { TWorkflowRunStatus } from "@forma/workflows";

/**
 * Rows retired per statement. Deliberately smaller than the AuthZed outbox's 10_000: a `WorkflowRun`
 * carries the whole trigger payload and cascades to its `WorkflowRunLog` rows, so one batch touches far
 * more than its own width.
 */
export const RETENTION_SWEEP_DELETE_BATCH_SIZE = 1_000;

/**
 * Upper bound on statements per pass unit — one (workspace, status) pair for workflow runs, one survey
 * for displays. Reaching it defers the remainder to the next tick rather than holding a connection for
 * the length of a first sweep over years of history.
 */
export const RETENTION_SWEEP_MAX_DELETE_BATCHES = 50;

/** Surveys resolved per round-trip when walking an organization's displays. */
export const RETENTION_SWEEP_SURVEY_PAGE_SIZE = 200;

/**
 * The statuses a run never leaves. A `queued` or `running` row is live work: it is excluded by status
 * rather than by age, so a run stuck behind a long backlog is never swept out from under its worker.
 */
export const RETENTION_TERMINAL_WORKFLOW_RUN_STATUSES: readonly TWorkflowRunStatus[] = [
  "completed",
  "failed",
  "canceled",
];

/** Daily. The windows are measured in days, so a shorter period only re-scans the same empty ranges. */
export const RETENTION_SWEEP_INTERVAL_MS = 24 * 60 * 60 * 1_000;

/**
 * Whether the sweep reports what it matched instead of deleting it.
 *
 * `true` for this first release, on purpose: deleting product data is irreversible and no test in this
 * repo would catch a window that is too short, so an operator gets to see the volume in the logs for a
 * release before any row goes. Flipping it is a separate, deliberate change.
 *
 * Typed `boolean` rather than left to literal inference so the delete path stays reachable to the type
 * checker while the flag is pinned on.
 */
export const RETENTION_SWEEP_DRY_RUN: boolean = true;
