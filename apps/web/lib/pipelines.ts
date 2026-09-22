import { TResponsePipelineJobData, getBackgroundJobProducer } from "@forma/jobs";
import { logger } from "@forma/logger";
import type { TUserLocale } from "@forma/types/user";
import { getJobsQueueingConfig } from "@/lib/jobs/config";
import { findMatchingLocale } from "@/lib/utils/locale";
import {
  recordDroppedResponsePipelineEvent,
  toOutboxErrorMessage,
} from "@/modules/response-pipeline/lib/outbox-repository";

// The producer connection runs with `enableOfflineQueue: false` and `maxRetriesPerRequest: 1`
// (packages/jobs/src/connection.ts), so a sub-second Valkey reconnect surfaces here immediately.
// A couple of short retries cover exactly that window.
const ENQUEUE_ATTEMPTS = 3;
const ENQUEUE_RETRY_BASE_DELAY_MS = 50;

const delay = (ms: number) =>
  new Promise((resolve) => {
    setTimeout(resolve, ms);
  });

/**
 * A queue id that is a function of the event and the exact response version it describes.
 *
 * BullMQ keys a job by its id and will not add an id it already holds, so every path that can enqueue
 * this event twice — the retry loop below, and the outbox drain replaying a row for it — becomes
 * idempotent for free. Two genuinely different events never collide: an update to the response moves
 * `updatedAt`, and the three trigger kinds carry different `event` values for the same version.
 *
 * `undefined` rather than a fabricated id when the version is unusable: a `Date.now()` fallback would
 * be non-deterministic, which is the one property this exists to provide. The caller then enqueues
 * exactly as it did before, which is correct, only not deduplicated.
 */
const buildResponsePipelineJobId = ({ event, response }: TResponsePipelineJobData): string | undefined => {
  const versionMs = response?.updatedAt instanceof Date ? response.updatedAt.getTime() : Number.NaN;
  if (!response?.id || Number.isNaN(versionMs)) return undefined;
  return `response-pipeline:${event}:${response.id}:${versionMs}`;
};

/**
 * Persist an event the queue refused, without ever becoming a second way for this path to fail.
 *
 * The response row is already committed and the foreign key cascades, so the insert can legitimately
 * lose a race with a deletion of that response. This is the one place a `catch` is not defensive
 * noise: the whole point of `sendToPipeline` is that nothing it does reaches the caller.
 */
const persistDroppedEvent = async (
  jobId: string,
  job: TResponsePipelineJobData,
  enqueueError: unknown
): Promise<boolean> => {
  try {
    await recordDroppedResponsePipelineEvent(jobId, job, toOutboxErrorMessage(enqueueError));
    return true;
  } catch (error) {
    logger.error(
      { err: error, responseId: job.response?.id, workspaceId: job.workspaceId },
      "Response pipeline outbox write failed"
    );
    return false;
  }
};

/**
 * Hand a response pipeline event to the background queue.
 *
 * Never throws. Every caller reaches this *after* the Response row is committed, so a throw became a
 * 500 on a request whose response already exists. The survey runtime treats 5xx as retryable and only
 * records `responseId` on a successful create, so each retry POSTed a new response — one submission
 * turning into up to four rows, inflating response counts, quota evaluation and the billing meter,
 * while the pipeline still never ran for them. Losing the pipeline event is the smaller failure, and
 * unlike a duplicate row it is visible in the logs.
 *
 * When every attempt fails the event is written to `ResponsePipelineOutbox` instead, and a recurring
 * drain replays it once the queue is reachable again. That write happens here rather than in each
 * caller's Response transaction on purpose: the queue being unreachable is precisely when PostgreSQL
 * is not, and it keeps the recovery path in one file instead of at all twelve Response-creating
 * boundaries. What it gives up in exchange is the window between the Response commit and this line —
 * a crash inside it still loses the event.
 */
export const sendToPipeline = async (job: TResponsePipelineJobData): Promise<void> => {
  const logContext = {
    event: job.event,
    surveyId: job.surveyId,
    workspaceId: job.workspaceId,
    responseId: job.response?.id,
  };

  const jobsQueueingConfig = getJobsQueueingConfig();
  if (!jobsQueueingConfig.enabled) {
    logger.error(logContext, "Response pipeline event dropped: BullMQ queueing is not enabled");
    return;
  }

  // Resolve the locale here (request scope) so the worker can localize follow-up emails
  // without calling headers()/cookies(), which are unavailable outside a request.
  let locale: TUserLocale | undefined = job.locale;
  if (!locale) {
    try {
      locale = await findMatchingLocale();
    } catch {
      locale = undefined;
    }
  }

  const queuedJob: TResponsePipelineJobData = { ...job, locale };
  const jobId = buildResponsePipelineJobId(queuedJob);

  for (let attempt = 1; attempt <= ENQUEUE_ATTEMPTS; attempt++) {
    try {
      const producer = getBackgroundJobProducer();
      await producer.enqueueResponsePipeline(queuedJob, jobId ? { jobId } : undefined);
      return;
    } catch (error) {
      if (attempt === ENQUEUE_ATTEMPTS) {
        const recovered = jobId ? await persistDroppedEvent(jobId, queuedJob, error) : false;
        // Alertable either way: `recovered` only means the event is queued for replay, so until the
        // drain succeeds nothing downstream of the response has run — no webhook, no follow-up email,
        // no integration row, no workflow.
        logger.error(
          { ...logContext, attempts: attempt, err: error, recovered },
          recovered
            ? "Response pipeline event deferred to the outbox after retries"
            : "Response pipeline event dropped after retries"
        );
        return;
      }

      logger.warn({ ...logContext, err: error, attempt }, "Response pipeline enqueue failed, retrying");
      await delay(ENQUEUE_RETRY_BASE_DELAY_MS * attempt);
    }
  }
};
