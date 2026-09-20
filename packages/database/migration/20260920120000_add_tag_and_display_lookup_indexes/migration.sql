-- Two index changes for access paths the squashed init schema left as sequential or filtered scans.
--
-- TagsOnResponses is indexed only by its primary key, which is led by "responseId". Every tagId-led access therefore reads the whole junction table: the `tags.applied` response filter, the tag merge in the workspace settings, the groupBy that counts responses per tag, and the ON DELETE CASCADE fired when a tag is deleted.
--
-- Display is indexed by "surveyId" alone, but the summary always counts displays for one survey within a date range, so the "created_at" predicate is applied per heap tuple instead of by the index. Extending the index keeps "surveyId" in the lead, so the bare-surveyId lookups and the cascade from Survey still use it and the single-column index becomes redundant.
--
-- Both builds are plain CREATE INDEX rather than CREATE INDEX CONCURRENTLY, and the file carries explicit transaction boundaries. `prisma migrate deploy` hands the whole file to PostgreSQL as one multi-statement script, and PostgreSQL runs such a script in an implicit transaction, so a concurrent build here would abort with 25001 ("CREATE INDEX CONCURRENTLY cannot run inside a transaction block") on every deploy. Dropping to a single statement is not a way out either: squawk's require-lock-timeout needs a preceding SET, which makes the file multi-statement again. This is the shape the rest of the repo already ships — see the same squawk ignore on 20260916140000_init, and docs/self-hosting/advanced/migration.mdx, which documents the identical trade-off for the Response index.
--
-- The cost that buys, which self-hosters need before scheduling an upgrade: a plain build holds a SHARE lock on its table until it finishes. Reads keep working, writes block. Neither table is small on a real instance — TagsOnResponses grows with every tag applied to a response, and Display with every survey impression, which makes Display one of the largest tables an instance has. So tagging a response and recording a display both stall for the duration of the matching build: seconds on a small instance, minutes on one holding millions of rows. Run the upgrade in a window where that pause is acceptable.

BEGIN;

SET LOCAL lock_timeout = '5s';

-- CreateIndex
-- squawk-ignore require-concurrent-index-creation
CREATE INDEX "TagsOnResponses_tagId_idx" ON "TagsOnResponses"("tagId");

-- CreateIndex
-- squawk-ignore require-concurrent-index-creation
CREATE INDEX "Display_surveyId_created_at_idx" ON "Display"("surveyId", "created_at");

-- DropIndex
-- Redundant once the index above exists: a btree led by "surveyId" serves every query the single-column index served. Dropping it in the same transaction as the create means no window in which neither index is available.
-- DROP INDEX CONCURRENTLY is unavailable for the same reason as the builds above, so this takes an ACCESS EXCLUSIVE lock on Display that blocks reads as well as writes. It is the last statement, so that lock is held only to COMMIT — but the transaction reaches it already holding SHARE, and acquiring ACCESS EXCLUSIVE still has to wait out any reader that started before the build did. If that wait exceeds the lock_timeout above the migration aborts and rolls back whole; re-run it.
-- squawk-ignore require-concurrent-index-deletion
DROP INDEX "Display_surveyId_idx";

COMMIT;
