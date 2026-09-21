import { TResponsePipelineJobData, getBackgroundJobProducer } from "@forma/jobs";
import { logger } from "@forma/logger";
import type { TUserLocale } from "@forma/types/user";
import { getJobsQueueingConfig } from "@/lib/jobs/config";
import { findMatchingLocale } from "@/lib/utils/locale";

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
 * Hand a response pipeline event to the background queue.
 *
 * Never throws. Every caller reaches this *after* the Response row is committed, so a throw became a
 * 500 on a request whose response already exists. The survey runtime treats 5xx as retryable and only
 * records `responseId` on a successful create, so each retry POSTed a new response — one submission
 * turning into up to four rows, inflating response counts, quota evaluation and the billing meter,
 * while the pipeline still never ran for them. Losing the pipeline event is the smaller failure, and
 * unlike a duplicate row it is visible in the logs.
 *
 * The event is not yet recovered if all attempts fail. Making it durable needs an outbox row written
 * in the Response transaction and drained by a recurring job, the shape `AuthzedProjectionOutbox`
 * and `reconcileOrphanedWorkflowRuns` already use here.
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

  for (let attempt = 1; attempt <= ENQUEUE_ATTEMPTS; attempt++) {
    try {
      const producer = getBackgroundJobProducer();
      await producer.enqueueResponsePipeline({ ...job, locale });
      return;
    } catch (error) {
      if (attempt === ENQUEUE_ATTEMPTS) {
        // Alertable: the response exists but nothing downstream of it will run — no webhook, no
        // follow-up email, no integration row, no workflow.
        logger.error(
          { ...logContext, err: error, attempts: attempt },
          "Response pipeline event dropped after retries"
        );
        return;
      }

      logger.warn({ ...logContext, err: error, attempt }, "Response pipeline enqueue failed, retrying");
      await delay(ENQUEUE_RETRY_BASE_DELAY_MS * attempt);
    }
  }
};
