-- Two more index changes for access paths the squashed init schema left as sequential or unordered scans.
--
-- Contact is indexed by "workspaceId" alone, but the contacts list always reads one workspace ordered by "created_at" DESC (apps/web/modules/contacts/lib/contacts.ts), so a page reads the workspace's whole contact set and sorts it to return 30 rows. Extending the index keeps "workspaceId" in the lead, so the bare-workspaceId lookups and the cascade from Workspace still use it and the single-column index becomes redundant.
--
-- SurveyFollowUp is indexed only by its primary key on "id". Prisma resolves the `followUps` relation with a separate SELECT ... WHERE "surveyId" IN (...), which every read through selectSurvey (apps/web/lib/survey/service.ts) and every processed response (apps/web/modules/response-pipeline/lib/process-response-pipeline-job.ts) triggers, and each one sequential-scans the table. The ON DELETE CASCADE from Survey scans it too, once per deleted survey.
--
-- Both builds are plain CREATE INDEX rather than CREATE INDEX CONCURRENTLY, and the file carries explicit transaction boundaries, for the reason spelled out in 20260920120000_add_tag_and_display_lookup_indexes: `prisma migrate deploy` hands the whole file to PostgreSQL as one multi-statement script, PostgreSQL runs such a script in an implicit transaction, and a concurrent build there aborts with 25001.
--
-- The cost that buys, which self-hosters need before scheduling an upgrade: a plain build holds a SHARE lock on its table until it finishes. Reads keep working, writes block. SurveyFollowUp holds per-survey configuration rows and is small everywhere, so its pause is negligible. Contact is not — it grows with every identified user and every CSV import — so contact writes and imports stall for the duration of that build: seconds on a small instance, minutes on one holding millions of contacts. Run the upgrade in a window where that pause is acceptable.

BEGIN;

SET LOCAL lock_timeout = '5s';

-- CreateIndex
-- squawk-ignore require-concurrent-index-creation
CREATE INDEX "Contact_workspaceId_created_at_idx" ON "Contact"("workspaceId", "created_at");

-- CreateIndex
-- squawk-ignore require-concurrent-index-creation
CREATE INDEX "SurveyFollowUp_surveyId_idx" ON "SurveyFollowUp"("surveyId");

-- DropIndex
-- Redundant once the index above exists: a btree led by "workspaceId" serves every query the single-column index served, including the ON DELETE CASCADE from Workspace. Dropping it in the same transaction as the create means no window in which neither index is available.
-- DROP INDEX CONCURRENTLY is unavailable for the same reason as the builds above, so this takes an ACCESS EXCLUSIVE lock on Contact that blocks reads as well as writes. It is the last statement, so that lock is held only to COMMIT — but the transaction reaches it already holding SHARE, and acquiring ACCESS EXCLUSIVE still has to wait out any reader that started before the build did. If that wait exceeds the lock_timeout above the migration aborts and rolls back whole; re-run it.
-- squawk-ignore require-concurrent-index-deletion
DROP INDEX "Contact_workspaceId_idx";

COMMIT;
