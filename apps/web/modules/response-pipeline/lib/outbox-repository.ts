import "server-only";
import { randomUUID } from "node:crypto";
import { prisma } from "@forma/database";
import type { PipelineTriggers, Prisma } from "@forma/database/prisma";
import type { TResponsePipelineJobData } from "@forma/jobs";

/**
 * Storage for response pipeline events the queue would not accept.
 *
 * `sendToPipeline` never throws, so before this table a Valkey blip lost the event outright — no
 * webhook, no follow-up email, no integration row, no workflow run, no billing meter, and only a log
 * line. A row is written only once the fast path has exhausted its in-process retry budget, which
 * means an empty table is the healthy state and a non-empty one is the alert. Nothing here pays a cost
 * on a healthy request.
 *
 * Shaped after `apps/web/lib/authzed/outbox-repository.ts` — lease column, attempt counter,
 * exponential backoff, dead-letter — with one deliberate divergence, in `pruneResponsePipelineOutbox`.
 */

/** Rows claimed per pass. Small on purpose: a pass only has work during or just after an outage. */
export const RESPONSE_PIPELINE_OUTBOX_BATCH_SIZE = 50;
export const RESPONSE_PIPELINE_OUTBOX_LEASE_MS = 60_000;

/**
 * Delivery attempts before a row dead-letters.
 *
 * Unlike the AuthZed outbox this needs no separate permanent-failure budget: there is exactly one
 * failure mode here (the queue would not take the job), and no outcome of it is attributable to the
 * event rather than to the queue. A payload that will not parse is dead-lettered on sight instead,
 * because retrying it cannot change the result. With the backoff below, twenty attempts is roughly an
 * hour of a queue that will not accept work — comfortably past any Valkey failover, and the point past
 * which a human has to look rather than the drain keep trying.
 */
export const RESPONSE_PIPELINE_OUTBOX_MAX_ATTEMPTS = 20;

/** Bound on the backoff exponent, not on the attempt count: `2 ^ attempts` overflows long before 12. */
export const RESPONSE_PIPELINE_OUTBOX_MAX_BACKOFF_ATTEMPTS = 12;
export const RESPONSE_PIPELINE_OUTBOX_MAX_RETRY_DELAY_MS = 5 * 60_000;

/** Matches `AUTHZED_OUTBOX_HISTORY_RETENTION_DAYS`, so both outboxes retire history on one schedule. */
export const RESPONSE_PIPELINE_OUTBOX_HISTORY_RETENTION_DAYS = 7;
export const RESPONSE_PIPELINE_OUTBOX_HISTORY_DELETE_BATCH_SIZE = 5_000;
export const RESPONSE_PIPELINE_OUTBOX_HISTORY_MAX_DELETE_BATCHES = 20;

const ERROR_MESSAGE_MAX_LENGTH = 500;

export interface TResponsePipelineOutboxEntry {
  attempts: number;
  createdAt: Date;
  event: PipelineTriggers;
  id: string;
  jobId: string;
  payload: Prisma.JsonValue;
  responseId: string;
  surveyId: string;
  workspaceId: string;
}

export interface TResponsePipelineOutboxStatus {
  deadLettered: number;
  oldestPendingAgeSeconds: number | null;
  pending: number;
}

/**
 * Keep an error message short and stringly typed for storage.
 *
 * This is our own queue client's error rather than a third party's, but it still ends up in an
 * operator-facing column, so it is bounded and never carries the error object's other properties —
 * `redisUrl` among them on some ioredis failures.
 */
export const toOutboxErrorMessage = (error: unknown): string => {
  const message = error instanceof Error ? error.message : String(error);
  return message.slice(0, ERROR_MESSAGE_MAX_LENGTH);
};

/**
 * `TResponsePipelineJobData` carries `Date`s, which `Prisma.InputJsonValue` does not accept. Going
 * through JSON is what the queue does with the same payload anyway, and `ZResponsePipelineJobData`
 * coerces the ISO strings back on read.
 */
const toJsonPayload = (job: TResponsePipelineJobData): Prisma.InputJsonValue =>
  JSON.parse(JSON.stringify(job)) as Prisma.InputJsonValue;

export const createResponsePipelineOutboxLeaseOwner = (): string => randomUUID();

/**
 * Persist an event the fast path could not enqueue.
 *
 * An upsert on `jobId`, not a create: `jobId` is deterministic in the event and the response version,
 * so a second exhaustion for the same pair must update one row rather than accumulate rows. The update
 * deliberately touches neither `processedAt` nor `deadLetteredAt` nor the backoff columns — a row the
 * drain already delivered stays delivered (the event did reach the queue), and a dead-lettered row
 * stays dead-lettered rather than silently re-arming into the same failure.
 */
export const recordDroppedResponsePipelineEvent = async (
  jobId: string,
  job: TResponsePipelineJobData,
  errorMessage: string
): Promise<void> => {
  const payload = toJsonPayload(job);

  await prisma.responsePipelineOutbox.upsert({
    create: {
      event: job.event,
      jobId,
      lastErrorMessage: errorMessage,
      payload,
      responseId: job.response.id,
      surveyId: job.surveyId,
      workspaceId: job.workspaceId,
    },
    update: { lastErrorMessage: errorMessage, payload },
    where: { jobId },
  });
};

export const claimResponsePipelineOutboxEntries = async (
  leaseOwner: string,
  limit = RESPONSE_PIPELINE_OUTBOX_BATCH_SIZE
): Promise<ReadonlyArray<TResponsePipelineOutboxEntry>> =>
  // tenant-scope-exempt: the drain is a global sweep over every workspace's undelivered events, so there is no caller tenant to scope it to; `workspaceId` travels with each row for the job it re-enqueues.
  await prisma.$queryRaw<TResponsePipelineOutboxEntry[]>`
    WITH claimable AS (
      SELECT "id"
      FROM "ResponsePipelineOutbox"
      WHERE "processedAt" IS NULL
        AND "deadLetteredAt" IS NULL
        AND "availableAt" <= NOW()
        AND ("leaseExpiresAt" IS NULL OR "leaseExpiresAt" <= NOW())
      ORDER BY "created_at" ASC
      LIMIT ${limit}
      FOR UPDATE SKIP LOCKED
    )
    UPDATE "ResponsePipelineOutbox" AS outbox
    SET "attempts" = outbox."attempts" + 1,
        "lastAttemptAt" = NOW(),
        "leasedAt" = NOW(),
        "leaseExpiresAt" = NOW() + (${RESPONSE_PIPELINE_OUTBOX_LEASE_MS} * INTERVAL '1 millisecond'),
        "leaseOwner" = ${leaseOwner},
        -- Raw SQL bypasses Prisma's @updatedAt, which a per-row updateMany would have got free.
        "updated_at" = NOW()
    FROM claimable
    WHERE outbox."id" = claimable."id"
    RETURNING outbox."id", outbox."jobId", outbox."event", outbox."responseId", outbox."surveyId",
              outbox."workspaceId", outbox."payload", outbox."attempts",
              outbox."created_at" AS "createdAt"
  `;

export const markResponsePipelineOutboxEntriesDelivered = async (
  leaseOwner: string,
  entryIds: ReadonlyArray<string>
): Promise<void> => {
  if (entryIds.length === 0) return;

  // tenant-scope-exempt: releases rows this drain pass leased, addressed by their own ids and lease owner; the drain spans every workspace by design.
  await prisma.responsePipelineOutbox.updateMany({
    data: {
      lastErrorMessage: null,
      leaseExpiresAt: null,
      leaseOwner: null,
      leasedAt: null,
      processedAt: new Date(),
    },
    where: { id: { in: [...entryIds] }, leaseOwner, processedAt: null },
  });
};

/**
 * Release a failed lease with backoff, and dead-letter what has run out of attempts.
 *
 * One statement rather than one per row: this path runs while delivery is already failing, which is
 * when the database is least likely to have headroom for a round trip per row. The backoff is computed
 * from each row's own `attempts`, so nothing has to be grouped by attempt count.
 */
export const markResponsePipelineOutboxEntriesFailed = async (
  leaseOwner: string,
  entryIds: ReadonlyArray<string>,
  errorMessage: string,
  { permanent }: Readonly<{ permanent: boolean }> = { permanent: false }
): Promise<number> => {
  if (entryIds.length === 0) return 0;

  // tenant-scope-exempt: releases rows this drain pass leased, addressed by their own ids and lease owner; the drain spans every workspace by design.
  const [row] = await prisma.$queryRaw<ReadonlyArray<{ dead_lettered: bigint }>>`
    WITH released AS (
      UPDATE "ResponsePipelineOutbox"
      SET "availableAt" = NOW() + (
            LEAST(
              ${RESPONSE_PIPELINE_OUTBOX_MAX_RETRY_DELAY_MS}::double precision,
              1000 * 2 ^ (LEAST("attempts", ${RESPONSE_PIPELINE_OUTBOX_MAX_BACKOFF_ATTEMPTS}) - 1)
            ) * INTERVAL '1 millisecond'
          ),
          "deadLetteredAt" = CASE
            WHEN ${permanent}::boolean OR "attempts" >= ${RESPONSE_PIPELINE_OUTBOX_MAX_ATTEMPTS}
            THEN NOW()
            ELSE NULL
          END,
          "lastErrorMessage" = ${errorMessage},
          "leaseExpiresAt" = NULL,
          "leasedAt" = NULL,
          "leaseOwner" = NULL,
          "updated_at" = NOW()
      WHERE "id" = ANY(${[...entryIds]}::text[])
        AND "leaseOwner" = ${leaseOwner}
        AND "processedAt" IS NULL
        AND "deadLetteredAt" IS NULL
      RETURNING "deadLetteredAt"
    )
    SELECT COUNT(*) FILTER (WHERE "deadLetteredAt" IS NOT NULL) AS dead_lettered FROM released
  `;

  return Number(row?.dead_lettered ?? 0);
};

const pruneResponsePipelineOutboxBatch = async (): Promise<number> =>
  // tenant-scope-exempt: retention sweep over every workspace's delivered and dead-lettered rows; a per-tenant window would leave respondent answers behind for the tenants it did not visit.
  await prisma.$executeRaw`
    WITH expired AS (
      SELECT "id"
      FROM "ResponsePipelineOutbox"
      WHERE (
        "processedAt" < NOW() - (${RESPONSE_PIPELINE_OUTBOX_HISTORY_RETENTION_DAYS} * INTERVAL '1 day')
        OR "deadLetteredAt" < NOW() - (${RESPONSE_PIPELINE_OUTBOX_HISTORY_RETENTION_DAYS} * INTERVAL '1 day')
      )
      ORDER BY "created_at" ASC
      LIMIT ${RESPONSE_PIPELINE_OUTBOX_HISTORY_DELETE_BATCH_SIZE}
    )
    DELETE FROM "ResponsePipelineOutbox" AS outbox
    USING expired
    WHERE outbox."id" = expired."id"
  `;

/**
 * Retire settled rows, dead letters included.
 *
 * The divergence from the AuthZed outbox, which keeps its dead letters as evidence: `payload` here is
 * the whole `TResponsePipelineJobData`, so it holds respondent answers. A settled row is a copy of
 * survey data, not a log line, and it has no business outliving the retention every other copy gets.
 * The `Response` foreign key cascade covers response and workspace deletion; nothing else does.
 */
export const pruneResponsePipelineOutbox = async (): Promise<number> => {
  let deleted = 0;

  for (let batch = 0; batch < RESPONSE_PIPELINE_OUTBOX_HISTORY_MAX_DELETE_BATCHES; batch++) {
    const count = await pruneResponsePipelineOutboxBatch();
    deleted += count;
    if (count < RESPONSE_PIPELINE_OUTBOX_HISTORY_DELETE_BATCH_SIZE) break;
  }

  return deleted;
};

type TStatusRow = Readonly<{
  dead_lettered: bigint;
  oldest_pending_age_seconds: number | null;
  pending: bigint;
}>;

/**
 * Aggregate backlog, scoped to undelivered rows so the retained history stays out of the scan. A
 * dead-lettered row always has a NULL `processedAt`, so `dead_lettered` is unaffected by that scoping.
 */
export const getResponsePipelineOutboxStatus = async (): Promise<TResponsePipelineOutboxStatus> => {
  // tenant-scope-exempt: deployment-wide backlog gauge for the drain; a per-workspace count would not answer whether the outbox is draining.
  const [row] = await prisma.$queryRaw<TStatusRow[]>`
    SELECT
      COUNT(*) FILTER (WHERE "deadLetteredAt" IS NULL) AS pending,
      COUNT(*) FILTER (WHERE "deadLetteredAt" IS NOT NULL) AS dead_lettered,
      EXTRACT(EPOCH FROM NOW() - MIN("created_at") FILTER (
        WHERE "deadLetteredAt" IS NULL
      ))::double precision AS oldest_pending_age_seconds
    FROM "ResponsePipelineOutbox"
    WHERE "processedAt" IS NULL
  `;

  return {
    deadLettered: Number(row?.dead_lettered ?? 0),
    oldestPendingAgeSeconds:
      row?.oldest_pending_age_seconds === null || row?.oldest_pending_age_seconds === undefined
        ? null
        : Math.max(0, Math.floor(row.oldest_pending_age_seconds)),
    pending: Number(row?.pending ?? 0),
  };
};
