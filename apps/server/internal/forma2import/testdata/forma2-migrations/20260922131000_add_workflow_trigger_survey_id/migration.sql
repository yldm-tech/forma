-- Denormalise a workflow's trigger survey onto the row the response pipeline queries.
--
-- `loadEnabledWorkflowCandidates` (apps/web/modules/workflows/lib/runner/enqueue-response-completed-runs.ts) fetches every enabled workflow in the workspace with its current published version's full definition JSON, then discards all of them whose trigger targets a different survey. Definitions are not small — a `send_email` action node carries user-authored subject and body — so the common zero-match case reads a few KB per enabled workflow on every completed response. With this column the survey filter moves into SQL and the read stops scaling with an unbounded, customer-controlled number.
--
-- **A null "triggerSurveyId" means "matches every survey", never "matches nothing".** That is the whole safety property of this change, and it is why the backfill below is allowed to be partial: a row it does not reach still reaches the in-memory matcher exactly as it does today. The reading query must therefore be `("triggerSurveyId" = $surveyId OR "triggerSurveyId" IS NULL)`, and `matchWorkflowsForResponse` stays the authority on whether a workflow actually fires. Read the other way — null as an empty match — every workflow this statement skipped would silently stop firing, which is a data-loss-shaped bug rather than a performance regression.
--
-- The backfill is deliberately narrow. It writes only rows whose current published version really carries a `response.completed` trigger with a string `surveyId`, and only while the row is `enabled`, which is the only status the reading query looks at; `enableWorkflow` is the sole path that publishes a version, and it sets the column in the same transaction from then on. Anything malformed, any other trigger type, and every draft, disabled or archived row is left null and therefore left matching everything. The statement is re-runnable on its own if the column ever drifts: it is guarded by `"triggerSurveyId" IS NULL`, so re-running it only fills gaps — to re-derive a row that already holds a value, clear that row first.
--
-- Safety, statement by statement, all inside one transaction with a 5s lock_timeout:
--   * ADD COLUMN of a nullable TEXT with no default: catalog-only, no table rewrite, ACCESS EXCLUSIVE on "Workflow" for the duration of the catalog update.
--   * The backfill: one pass over "Workflow" joined to a DISTINCT ON over "WorkflowVersion", which uses "WorkflowVersion_workflowId_version_key". Both tables hold one row per authored workflow (and per published version of one), which is bounded by what people write by hand — this is not a large-table backfill, and it is not the shape the rule against in-migration backfills is aimed at. Row locks on the updated rows only; concurrent workflow edits to an untouched row are unaffected.
--   * CREATE INDEX (not CONCURRENTLY, for the reason spelled out in 20260920120000_add_tag_and_display_lookup_indexes: `prisma migrate deploy` hands the file to PostgreSQL as one multi-statement script, which runs in an implicit transaction, and a concurrent build there aborts with 25001). It holds SHARE on "Workflow" until it finishes: reads keep working, workflow writes block. On a table of this size that is milliseconds.
--   * DROP INDEX of the now-redundant two-column index. A btree led by ("workspaceId", "status") serves every query the old index served, including the workflows list, so dropping it in the same transaction as the create means no window in which neither exists. It takes ACCESS EXCLUSIVE, held only to COMMIT, but acquiring it has to wait out any reader that started before the build; if that wait exceeds the lock_timeout the whole migration rolls back and can simply be re-run.

BEGIN;

SET LOCAL lock_timeout = '5s';

-- AlterTable
ALTER TABLE "Workflow" ADD COLUMN "triggerSurveyId" TEXT;

-- Backfill from the current published version of every enabled workflow. Mirrors the runner's own read: highest "version" per workflow, `trigger.triggerType` must be `response.completed`, and `trigger.config.surveyId` must be a JSON string. "definition" is JSONB, so `->` on a non-object yields NULL rather than raising, and a malformed snapshot simply leaves the row null.
UPDATE "Workflow" w
SET "triggerSurveyId" = latest."surveyId"
FROM (
  SELECT DISTINCT ON (v."workflowId")
         v."workflowId",
         v."definition" -> 'trigger' ->> 'triggerType' AS "triggerType",
         v."definition" -> 'trigger' -> 'config' ->> 'surveyId' AS "surveyId"
  FROM "WorkflowVersion" v
  ORDER BY v."workflowId", v."version" DESC
) latest
WHERE latest."workflowId" = w."id"
  AND latest."triggerType" = 'response.completed'
  AND latest."surveyId" IS NOT NULL
  AND w."status" = 'enabled'
  AND w."triggerSurveyId" IS NULL;

-- CreateIndex
-- squawk-ignore require-concurrent-index-creation
CREATE INDEX "Workflow_workspaceId_status_triggerSurveyId_idx" ON "Workflow"("workspaceId", "status", "triggerSurveyId");

-- DropIndex
-- squawk-ignore require-concurrent-index-deletion
DROP INDEX "Workflow_workspaceId_status_idx";

COMMIT;
