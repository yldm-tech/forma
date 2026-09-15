-- Rebrand Formbricks -> Forma for the two database objects that carried the old brand in their
-- name: the FeedbackSourceFormbricksMapping table and the FeedbackSourceType.formbricks_survey
-- enum value.
--
-- Everything is renamed in place, so no data moves and no rows are rewritten. RENAME VALUE keeps
-- every existing FeedbackSource row pointing at the same enum member under its new label.
--
-- ALTER TABLE ... RENAME TO does not touch the names of that table's indexes and constraints, so
-- each one is renamed explicitly. The target names are exactly the `map:` values in
-- schema/main.prisma (and, where there is no `map:`, exactly what Prisma derives from the new model
-- name) — if the two ever disagree, check-migration-drift fails.
--
-- Wrapped in an explicit transaction: Prisma does not add one, and a rename that failed part way
-- through would leave the table's name and its constraints' names disagreeing, which no rerun
-- could repair. Every statement here is transactional DDL, RENAME VALUE included.

BEGIN;

-- Each statement takes ACCESS EXCLUSIVE on a small table. The timeout bounds how long that waits
-- behind an open transaction rather than queueing reads and writes behind it indefinitely.
SET LOCAL lock_timeout = '5s';

-- Rename the enum value
ALTER TYPE "FeedbackSourceType" RENAME VALUE 'formbricks_survey' TO 'forma_survey';

-- Rename the table. Clients addressing it by its old name break, which is the point of the rebrand:
-- the only reader is this repo's own Prisma client, regenerated from the schema in the same commit.
-- squawk-ignore renaming-table
ALTER TABLE "FeedbackSourceFormbricksMapping" RENAME TO "FeedbackSourceFormaMapping";

-- Rename the primary-key index
ALTER INDEX "FeedbackSourceFormbricksMapping_pkey" RENAME TO "FeedbackSourceFormaMapping_pkey";

-- Rename the remaining indexes. The unique index was created under a name Postgres truncated to 63
-- bytes; the new name is 58 bytes and needs no truncation.
ALTER INDEX "FeedbackSourceFormbricksMapping_workspaceId_feedbackSourceId_su"
  RENAME TO "FeedbackSourceFormaMapping_workspaceId_feedbackSourceId_su";
ALTER INDEX "FeedbackSourceFormbricksMapping_workspaceId_surveyId_idx"
  RENAME TO "FeedbackSourceFormaMapping_workspaceId_surveyId_idx";
ALTER INDEX "FeedbackSourceFormbricksMapping_surveyId_idx"
  RENAME TO "FeedbackSourceFormaMapping_surveyId_idx";

-- Rename the foreign-key constraints. As above, the first was truncated to 63 bytes on creation.
ALTER TABLE "FeedbackSourceFormaMapping"
  RENAME CONSTRAINT "FeedbackSourceFormbricksMapping_feedbackSourceId_workspaceId_fk"
  TO "FeedbackSourceFormaMapping_feedbackSourceId_workspaceId_fk";
ALTER TABLE "FeedbackSourceFormaMapping"
  RENAME CONSTRAINT "FeedbackSourceFormbricksMapping_surveyId_workspaceId_fkey"
  TO "FeedbackSourceFormaMapping_surveyId_workspaceId_fkey";

COMMIT;
