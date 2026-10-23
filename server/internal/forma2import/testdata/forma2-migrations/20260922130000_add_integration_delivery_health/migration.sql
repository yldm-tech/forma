-- Integration delivery health.
--
-- `handleIntegrations` (apps/web/modules/response-pipeline/lib/handle-integrations.ts) has four duplicated `if (!result.ok) { logger.error(...) }` arms and nothing else: a workspace whose Airtable base was deleted, or whose sheet lost write permission, finds out when someone opens the sheet weeks later. These three columns make that a recorded state the integration's manage page can render. The per-type credential probes cannot see this case — the token is valid and the failure is per-resource — and Notion has no probe at all.
--
-- There is deliberately no `lastSyncAt`. A success timestamp would be written on the healthy path, which means every response in a workspace updating the same handful of rows; the columns below are touched only on a state transition, so a healthy install issues zero writes here. The cost of that choice is that `lastErrorAt IS NULL` cannot distinguish "recovered" from "never attempted", which the application is expected to read as "nothing wrong is known" rather than as evidence of a delivery.
--
-- Safety: one ALTER TABLE, so one ACCESS EXCLUSIVE lock on "Integration", held for a catalog update and nothing else. `consecutiveFailures` is NOT NULL with a constant DEFAULT, which PostgreSQL 11 and later store as a missing-value in `pg_attribute` rather than rewriting the table; the lowest major this repo supports is 15 (packages/database/.squawk.toml), so no supported version rewrites here. "Integration" holds at most one row per integration type per workspace in any case, so the statement is instant even on the largest install. No index is built and no existing row is read.

BEGIN;

SET LOCAL lock_timeout = '5s';

-- AlterTable
ALTER TABLE "Integration"
  ADD COLUMN "lastErrorAt" TIMESTAMP(3),
  ADD COLUMN "lastErrorMessage" TEXT,
  ADD COLUMN "consecutiveFailures" INTEGER NOT NULL DEFAULT 0;

COMMIT;
