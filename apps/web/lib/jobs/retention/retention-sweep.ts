import "server-only";
import { prisma } from "@forma/database";
import { logger } from "@forma/logger";
import type { TWorkflowRunStatus } from "@forma/workflows";
import {
  RETENTION_SWEEP_DELETE_BATCH_SIZE,
  RETENTION_SWEEP_DRY_RUN,
  RETENTION_SWEEP_MAX_DELETE_BATCHES,
  RETENTION_SWEEP_SURVEY_PAGE_SIZE,
  RETENTION_TERMINAL_WORKFLOW_RUN_STATUSES,
} from "./constants";

const MS_PER_DAY = 24 * 60 * 60 * 1_000;

export interface TRetentionPassResult {
  /** Rows the pass found eligible. Only populated in dry-run mode, where nothing is deleted. */
  matched: number;
  /** Rows the pass actually removed. Always 0 in dry-run mode. */
  deleted: number;
  /** Pass units that hit the batch cap and deferred their remainder to the next tick. */
  cappedUnits: number;
}

export interface TRetentionSweepResult {
  dryRun: boolean;
  /** Organizations that configured at least one usable window. */
  organizations: number;
  /** Organizations skipped because a configured window was not a positive whole number of days. */
  invalidWindows: number;
  workflowRuns: TRetentionPassResult;
  displays: TRetentionPassResult;
}

/** Rows created strictly before this instant are past the window. */
export const getRetentionCutoff = (now: Date, retentionDays: number): Date =>
  new Date(now.getTime() - retentionDays * MS_PER_DAY);

/**
 * A null window means "keep forever", which disables the pass — it is never a stand-in for a default.
 * A non-null value that is not a positive whole number of days is a bug in whatever wrote the column,
 * and is treated as "keep forever" too: the alternative is deleting everything against a zero window.
 */
const isUsableRetentionWindow = (retentionDays: number | null): retentionDays is number =>
  retentionDays !== null && Number.isInteger(retentionDays) && retentionDays >= 1;

const emptyPassResult = (): TRetentionPassResult => ({ cappedUnits: 0, deleted: 0, matched: 0 });

const addPassResult = (total: TRetentionPassResult, addition: TRetentionPassResult): void => {
  total.matched += addition.matched;
  total.deleted += addition.deleted;
  total.cappedUnits += addition.cappedUnits;
};

/**
 * Retire one pass unit in bounded statements.
 *
 * Each statement deletes at most `RETENTION_SWEEP_DELETE_BATCH_SIZE` rows, so no single transaction is
 * held open across a large history; the loop lets one tick retire more than one batch without
 * monopolizing a connection. A short batch means the unit is drained.
 */
const drainInBatches = async (
  deleteBatch: () => Promise<number>
): Promise<{ deleted: number; capped: boolean }> => {
  let deleted = 0;

  for (let batch = 0; batch < RETENTION_SWEEP_MAX_DELETE_BATCHES; batch++) {
    const count = await deleteBatch();
    deleted += count;
    if (count < RETENTION_SWEEP_DELETE_BATCH_SIZE) return { capped: false, deleted };
  }

  return { capped: true, deleted };
};

const countExpiredWorkflowRuns = async (
  workspaceId: string,
  status: TWorkflowRunStatus,
  cutoff: Date
): Promise<number> => prisma.workflowRun.count({ where: { createdAt: { lt: cutoff }, status, workspaceId } });

/**
 * One status at a time so the predicate is an equality rather than a range over the status column, and
 * `created_at` is the age column because `finishedAt` is null on some terminal runs (a run canceled
 * before it was dispatched never finished).
 */
const deleteExpiredWorkflowRunBatch = async (
  workspaceId: string,
  status: TWorkflowRunStatus,
  cutoff: Date
): Promise<number> =>
  prisma.$executeRaw`
    WITH expired AS (
      SELECT "id"
      FROM "WorkflowRun"
      WHERE "workspaceId" = ${workspaceId}
        AND "status" = ${status}::"WorkflowRunStatus"
        AND "created_at" < ${cutoff}
      ORDER BY "created_at" ASC
      LIMIT ${RETENTION_SWEEP_DELETE_BATCH_SIZE}
    )
    DELETE FROM "WorkflowRun" AS run
    USING expired
    WHERE run."id" = expired."id"
  `;

const countExpiredDisplays = async (surveyId: string, cutoff: Date): Promise<number> =>
  prisma.display.count({ where: { createdAt: { lt: cutoff }, response: { is: null }, surveyId } });

/**
 * The `NOT EXISTS` is the guard, not a hint: `Response_displayId_fkey` is `ON DELETE SET NULL`, so
 * deleting a converted impression would silently blank the response's `displayId` rather than raise.
 * An impression that became a response is the one an operator wants kept.
 */
const deleteExpiredDisplayBatch = async (surveyId: string, cutoff: Date): Promise<number> =>
  prisma.$executeRaw`
    WITH expired AS (
      SELECT display."id"
      FROM "Display" AS display
      WHERE display."surveyId" = ${surveyId}
        AND display."created_at" < ${cutoff}
        AND NOT EXISTS (
          SELECT 1
          FROM "Response" AS response
          WHERE response."displayId" = display."id"
        )
      ORDER BY display."created_at" ASC
      LIMIT ${RETENTION_SWEEP_DELETE_BATCH_SIZE}
    )
    DELETE FROM "Display" AS target
    USING expired
    WHERE target."id" = expired."id"
  `;

/** Terminal workflow runs older than the organization's window, one (workspace, status) pass at a time. */
export const sweepWorkflowRunRetention = async (
  organizationId: string,
  retentionDays: number,
  now: Date,
  dryRun: boolean
): Promise<TRetentionPassResult> => {
  const cutoff = getRetentionCutoff(now, retentionDays);
  const result = emptyPassResult();
  const workspaces = await prisma.workspace.findMany({
    where: { organizationId },
    select: { id: true },
  });

  for (const workspace of workspaces) {
    for (const status of RETENTION_TERMINAL_WORKFLOW_RUN_STATUSES) {
      if (dryRun) {
        result.matched += await countExpiredWorkflowRuns(workspace.id, status, cutoff);
        continue;
      }

      const { capped, deleted } = await drainInBatches(() =>
        deleteExpiredWorkflowRunBatch(workspace.id, status, cutoff)
      );
      result.deleted += deleted;
      if (capped) result.cappedUnits += 1;
    }
  }

  return result;
};

/**
 * Walks the organization's surveys in id order rather than loading them all: the per-survey predicate is
 * what keeps each statement on `Display("surveyId", "created_at")`, and no index is added for this pass
 * — building one on `Display` blocks every survey impression for the length of the build.
 */
const forEachSurveyId = async (
  organizationId: string,
  handleSurveyId: (surveyId: string) => Promise<void>
): Promise<void> => {
  let cursor: string | undefined;

  for (;;) {
    const surveys = await prisma.survey.findMany({
      where: { workspace: { organizationId } },
      select: { id: true },
      orderBy: { id: "asc" },
      take: RETENTION_SWEEP_SURVEY_PAGE_SIZE,
      ...(cursor === undefined ? {} : { cursor: { id: cursor }, skip: 1 }),
    });

    if (surveys.length === 0) return;

    for (const survey of surveys) {
      await handleSurveyId(survey.id);
    }

    if (surveys.length < RETENTION_SWEEP_SURVEY_PAGE_SIZE) return;
    cursor = surveys[surveys.length - 1].id;
  }
};

/** Impressions older than the organization's window that never became a response. */
export const sweepDisplayRetention = async (
  organizationId: string,
  retentionDays: number,
  now: Date,
  dryRun: boolean
): Promise<TRetentionPassResult> => {
  const cutoff = getRetentionCutoff(now, retentionDays);
  const result = emptyPassResult();

  await forEachSurveyId(organizationId, async (surveyId) => {
    if (dryRun) {
      result.matched += await countExpiredDisplays(surveyId, cutoff);
      return;
    }

    const { capped, deleted } = await drainInBatches(() => deleteExpiredDisplayBatch(surveyId, cutoff));
    result.deleted += deleted;
    if (capped) result.cappedUnits += 1;
  });

  return result;
};

/**
 * Applies each organization's own retention windows.
 *
 * How long a customer's data is kept is configuration, not a constant in here: both windows live on
 * `Organization` and both are null everywhere today, which means "keep forever" and disables the pass.
 * That is deliberately the same behaviour as before this job existed — an upgrade deletes nothing until
 * somebody sets a window.
 *
 * One organization's failure does not abort the sweep: the rest still get their windows applied.
 */
export const runRetentionSweep = async (
  now: Date = new Date(),
  dryRun: boolean = RETENTION_SWEEP_DRY_RUN
): Promise<TRetentionSweepResult> => {
  // tenant-scope-exempt: instance-wide sweep — it exists to visit every organization that configured a window
  const organizations = await prisma.organization.findMany({
    where: {
      OR: [{ workflowRunRetentionDays: { not: null } }, { displayRetentionDays: { not: null } }],
    },
    select: { id: true, displayRetentionDays: true, workflowRunRetentionDays: true },
  });

  const result: TRetentionSweepResult = {
    displays: emptyPassResult(),
    dryRun,
    invalidWindows: 0,
    organizations: 0,
    workflowRuns: emptyPassResult(),
  };

  for (const organization of organizations) {
    const workflowRunDays = organization.workflowRunRetentionDays;
    const displayDays = organization.displayRetentionDays;
    const hasInvalidWindow =
      (workflowRunDays !== null && !isUsableRetentionWindow(workflowRunDays)) ||
      (displayDays !== null && !isUsableRetentionWindow(displayDays));

    if (hasInvalidWindow) {
      result.invalidWindows += 1;
      logger.warn(
        {
          displayRetentionDays: displayDays,
          organizationId: organization.id,
          workflowRunRetentionDays: workflowRunDays,
        },
        "Retention sweep skipped a window that is not a positive whole number of days"
      );
    }

    const sweepsWorkflowRuns = isUsableRetentionWindow(workflowRunDays);
    const sweepsDisplays = isUsableRetentionWindow(displayDays);
    if (!sweepsWorkflowRuns && !sweepsDisplays) continue;

    result.organizations += 1;

    try {
      if (sweepsWorkflowRuns) {
        addPassResult(
          result.workflowRuns,
          await sweepWorkflowRunRetention(organization.id, workflowRunDays, now, dryRun)
        );
      }

      if (sweepsDisplays) {
        addPassResult(
          result.displays,
          await sweepDisplayRetention(organization.id, displayDays, now, dryRun)
        );
      }
    } catch (error) {
      logger.error(
        { err: error, organizationId: organization.id },
        "Retention sweep failed for an organization"
      );
    }
  }

  return result;
};
