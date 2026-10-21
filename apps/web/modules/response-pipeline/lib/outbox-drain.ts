import "server-only";
import { ZResponsePipelineJobData, getBackgroundJobProducer } from "@forma/jobs";
import { logger } from "@forma/logger";
import { getJobsQueueingConfig } from "@/lib/jobs/config";
import {
  RESPONSE_PIPELINE_OUTBOX_BATCH_SIZE,
  claimResponsePipelineOutboxEntries,
  createResponsePipelineOutboxLeaseOwner,
  getResponsePipelineOutboxStatus,
  markResponsePipelineOutboxEntriesDelivered,
  markResponsePipelineOutboxEntriesFailed,
  pruneResponsePipelineOutbox,
  toOutboxErrorMessage,
} from "./outbox-repository";

const INVALID_PAYLOAD_ERROR = "Outbox payload does not match ZResponsePipelineJobData";

export interface TResponsePipelineOutboxBatchResult {
  claimed: number;
  deadLettered: number;
  delivered: number;
  failed: number;
}

export interface TResponsePipelineOutboxDrainResult extends TResponsePipelineOutboxBatchResult {
  remaining: number;
}

const EMPTY_BATCH: TResponsePipelineOutboxBatchResult = {
  claimed: 0,
  deadLettered: 0,
  delivered: 0,
  failed: 0,
};

/**
 * Re-enqueue one claimed batch.
 *
 * Every row goes back under the `jobId` the fast path derived for it, so redelivery is idempotent at
 * the queue: BullMQ keys a job by its id, and an id it already holds is not added twice. That is what
 * lets the drain re-enqueue blindly instead of reconciling against a delivered marker — a row whose
 * event in fact reached the queue produces a no-op, not a second webhook.
 *
 * The loop stops at the first enqueue failure. There is exactly one reason an enqueue fails here, and
 * it is not specific to the row: the queue will not take work. Trying the other forty-nine would spend
 * a round trip each to learn what the first already reported, and every one of them is released
 * untried, keeping its place at the front of the next pass.
 */
const deliverClaimedEntries = async (
  entries: ReadonlyArray<Readonly<{ id: string; jobId: string; payload: unknown }>>
): Promise<
  Readonly<{ delivered: string[]; failed: string[]; failureMessage: string; invalid: string[] }>
> => {
  const delivered: string[] = [];
  const invalid: string[] = [];
  const failed: string[] = [];
  let failureMessage = "";

  for (let index = 0; index < entries.length; index++) {
    const entry = entries[index];
    // Persisted DB JSON is never validated on write, so the shape is a claim rather than a guarantee.
    const parsed = ZResponsePipelineJobData.safeParse(entry.payload);
    if (!parsed.success) {
      invalid.push(entry.id);
      continue;
    }

    try {
      await getBackgroundJobProducer().enqueueResponsePipeline(parsed.data, { jobId: entry.jobId });
      delivered.push(entry.id);
    } catch (error) {
      failureMessage = toOutboxErrorMessage(error);
      failed.push(...entries.slice(index).map(({ id }) => id));
      break;
    }
  }

  return { delivered, failed, failureMessage, invalid };
};

export const processResponsePipelineOutboxBatch = async (
  leaseOwner = createResponsePipelineOutboxLeaseOwner(),
  batchSize = RESPONSE_PIPELINE_OUTBOX_BATCH_SIZE
): Promise<TResponsePipelineOutboxBatchResult> => {
  const entries = await claimResponsePipelineOutboxEntries(leaseOwner, batchSize);
  if (entries.length === 0) return EMPTY_BATCH;

  const outcome = await deliverClaimedEntries(entries);

  await markResponsePipelineOutboxEntriesDelivered(leaseOwner, outcome.delivered);

  // A payload that will not parse cannot be fixed by waiting, so it dead-letters on its first pass
  // rather than burning twenty attempts to reach the same verdict.
  let deadLettered = await markResponsePipelineOutboxEntriesFailed(
    leaseOwner,
    outcome.invalid,
    INVALID_PAYLOAD_ERROR,
    { permanent: true }
  );
  deadLettered += await markResponsePipelineOutboxEntriesFailed(
    leaseOwner,
    outcome.failed,
    outcome.failureMessage,
    { permanent: false }
  );

  const failed = outcome.failed.length + outcome.invalid.length;
  if (failed > 0) {
    logger.warn(
      {
        component: "response-pipeline",
        deadLettered,
        failed,
        invalidPayloads: outcome.invalid.length,
        operation: "response_pipeline_outbox_delivery",
      },
      "Response pipeline outbox delivery failed"
    );
  }

  return { claimed: entries.length, deadLettered, delivered: outcome.delivered.length, failed };
};

export const drainResponsePipelineOutbox = async (
  maxBatches = 10
): Promise<TResponsePipelineOutboxDrainResult> => {
  const leaseOwner = createResponsePipelineOutboxLeaseOwner();
  const totals = { claimed: 0, deadLettered: 0, delivered: 0, failed: 0 };

  for (let batch = 0; batch < maxBatches; batch++) {
    const result = await processResponsePipelineOutboxBatch(leaseOwner);
    totals.claimed += result.claimed;
    totals.deadLettered += result.deadLettered;
    totals.delivered += result.delivered;
    totals.failed += result.failed;
    // Delivered rows get a `processedAt` and failed rows a future `availableAt`, so every pass that
    // made progress strictly shrinks the claimable set. A pass that delivered nothing means the queue
    // is still unreachable; continuing would only re-lease rows that cannot move.
    if (result.claimed === 0 || result.delivered === 0) break;
  }

  const status = await getResponsePipelineOutboxStatus();
  return { ...totals, remaining: status.pending };
};

/**
 * The recurring job's handler: drain, then retire settled rows.
 *
 * Both halves live in one job because both are no-ops in the healthy state — the table only ever
 * receives rows while the queue is unreachable, so an idle pass is one index probe for the claim and
 * one for the prune, and a second schedule would buy nothing.
 */
export const processResponsePipelineOutboxJob = async (): Promise<void> => {
  if (!getJobsQueueingConfig().enabled) return;

  const result = await drainResponsePipelineOutbox();
  const logContext = {
    component: "response-pipeline",
    deadLettered: result.deadLettered,
    delivered: result.delivered,
    operation: "response_pipeline_outbox_delivery",
    remaining: result.remaining,
  };

  // A remaining backlog is the alertable half and has to clear the production log level, which is
  // `warn`; a pass that emptied the outbox is only worth an `info` an operator reads after the fact.
  if (result.remaining > 0) {
    logger.warn(logContext, "Response pipeline outbox still has undelivered events");
  } else if (result.delivered > 0) {
    logger.info(logContext, "Response pipeline outbox drained");
  }

  await pruneResponsePipelineOutbox();
};
