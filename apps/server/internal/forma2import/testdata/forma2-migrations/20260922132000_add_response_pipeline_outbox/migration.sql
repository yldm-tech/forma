-- Durable fallback for a response pipeline event the queue would not accept.
--
-- `sendToPipeline` (apps/web/lib/pipelines.ts) never throws, because every caller reaches it after the Response row is committed and a throw there became a 500 on a request whose response already exists — the survey runtime retries a 5xx as a new submission, so one response turned into up to four rows. The price of that contract is that a Valkey blip loses the event outright: no webhook, no follow-up email, no integration row, no workflow run, no billing meter, and only a log line. This table is where the event goes when the fast path's retry budget is exhausted, drained by a recurring job modelled on `claimAuthzedOutboxEvents` / `markAuthzedOutboxEventsDelivered` (apps/web/lib/authzed/outbox-repository.ts). It pays nothing on a healthy request; for the duration of a failover it turns total silent loss into delayed delivery.
--
-- "jobId" is the deterministic BullMQ id the fast path derived for this event, and it is unique here for the same reason it is deterministic there: a replay re-enqueues under the id the lost attempt would have used, BullMQ rejects a duplicate, and the drain needs no delivered-marker reconciliation against the fast path. Writing the row is therefore an upsert on that column, not a plain insert, so a second exhaustion for the same event and response version updates one row instead of accumulating them.
--
-- "payload" holds the `TResponsePipelineJobData` that failed to enqueue, which means it holds respondent answers. Two consequences the drain has to honour: the row is subject to the same retention the AuthZed outbox has (delivered and dead-lettered rows are pruned, they are not history), and the foreign key to "Response" cascades, so deleting a response — or the workspace above it — takes any undelivered copy of its data with it. That cascade is the reason for the plain index on "responseId": without it each cascaded delete would scan this table.
--
-- Unlike "AuthzedProjectionOutbox" the indexes here are declared in the Prisma model rather than in SQL. None of them needs a `WHERE`: this table only ever receives rows written while the queue was unreachable, so carrying delivered history in every index costs nothing at the size it can reach, and keeping them expressible means `prisma db push` on a dev database does not silently drop them.
--
-- Safety: CREATE TABLE and its indexes touch nothing that exists — an empty table cannot lock out a workload, and the four index builds have no rows to read. The one statement that reaches an existing table is the foreign key, which takes a brief SHARE ROW EXCLUSIVE on "Response" (blocking writes to it, not reads) while PostgreSQL validates the referencing side; the referencing side is this empty table, so validation reads nothing and the lock is held for the DDL itself. Under the 5s lock_timeout the worst case is that the migration cannot get that lock, aborts whole, and is re-run. The three ignored rules all describe changing a table that already holds rows, which this migration does not do.
-- squawk-ignore-file require-concurrent-index-creation, constraint-missing-not-valid, adding-foreign-key-constraint

BEGIN;

SET LOCAL lock_timeout = '5s';

-- CreateTable
CREATE TABLE "ResponsePipelineOutbox" (
    "id" TEXT NOT NULL,
    "created_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at" TIMESTAMP(3) NOT NULL,
    "jobId" TEXT NOT NULL,
    "event" "PipelineTriggers" NOT NULL,
    "responseId" TEXT NOT NULL,
    "surveyId" TEXT NOT NULL,
    "workspaceId" TEXT NOT NULL,
    "payload" JSONB NOT NULL,
    "attempts" INTEGER NOT NULL DEFAULT 0,
    "availableAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "leasedAt" TIMESTAMP(3),
    "leaseExpiresAt" TIMESTAMP(3),
    "leaseOwner" TEXT,
    "processedAt" TIMESTAMP(3),
    "deadLetteredAt" TIMESTAMP(3),
    "lastAttemptAt" TIMESTAMP(3),
    "lastErrorMessage" TEXT,

    CONSTRAINT "ResponsePipelineOutbox_pkey" PRIMARY KEY ("id")
);

-- CreateIndex
CREATE UNIQUE INDEX "ResponsePipelineOutbox_jobId_key" ON "ResponsePipelineOutbox"("jobId");

-- CreateIndex
CREATE INDEX "ResponsePipelineOutbox_availableAt_idx" ON "ResponsePipelineOutbox"("availableAt");

-- CreateIndex
CREATE INDEX "ResponsePipelineOutbox_processedAt_idx" ON "ResponsePipelineOutbox"("processedAt");

-- CreateIndex
CREATE INDEX "ResponsePipelineOutbox_responseId_idx" ON "ResponsePipelineOutbox"("responseId");

-- CreateIndex
CREATE INDEX "ResponsePipelineOutbox_workspaceId_created_at_idx" ON "ResponsePipelineOutbox"("workspaceId", "created_at");

-- AddForeignKey
ALTER TABLE "ResponsePipelineOutbox" ADD CONSTRAINT "ResponsePipelineOutbox_responseId_fkey" FOREIGN KEY ("responseId") REFERENCES "Response"("id") ON DELETE CASCADE ON UPDATE CASCADE;

COMMIT;
