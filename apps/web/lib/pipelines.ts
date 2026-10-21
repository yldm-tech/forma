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
 * Deterministic queue id for one pipeline event, so the retry below cannot enqueue the same event twice.
 *
 * BullMQ deduplicates on an explicit job id: `addStandardJob` returns the id of the job already holding it instead of queueing a second one. Without it, a Valkey blip that drops the reply *after* the Lua script ran leaves attempt 1's job in the queue and attempt 2 adds another under a fresh random id — one submission becoming two follow-up emails, two notification emails, a duplicate row in every connected Google Sheet / Airtable / Notion, and two POSTs to every webhook. Only the workflow-run step was idempotent on its own.
 *
 * `event` is part of the key because one finished submission sends `responseCreated` and `responseFinished` for the same row in the same request, and `updatedAt` because a later edit of that response is a new event rather than a replay of this one. Left readable rather than hashed so the queue shows which response a job belongs to; the separator is `-` because BullMQ rejects a custom job id containing `:`, and neither component can contain one (a cuid2 is alphanumeric, the event is an enum). `getTime()` rather than `toISOString()`: it yields `NaN` instead of throwing on a malformed date, and this runs outside the retry loop's try, where a throw would break `sendToPipeline`'s never-throw contract.
 *
 * Bounded by `removeOnComplete` in `JOBS_DEFAULT_JOB_OPTIONS` — once the completed job is evicted the id is free again, so this dedupes a retry seconds apart, not a redelivery days later.
 *
 * Both reads are optional-chained for the same reason `getTime()` is used: the type says `response` is always there, but this runs outside the retry loop's try, and a throw here would break the never-throw contract documented below. A missing row yields a degenerate id rather than a 500 on a request whose Response is already committed.
 */
const getEnqueueJobId = (job: TResponsePipelineJobData): string =>
  `response-pipeline-${job.event}-${job.response?.id}-${new Date(job.response?.updatedAt).getTime()}`;

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

  // Hoisted because it does not vary per attempt, not because recomputing would be wrong.
  const jobId = getEnqueueJobId(job);

  for (let attempt = 1; attempt <= ENQUEUE_ATTEMPTS; attempt++) {
    try {
      const producer = getBackgroundJobProducer();
      await producer.enqueueResponsePipeline({ ...job, locale }, { jobId });
      return;
    } catch (error) {
      if (attempt === ENQUEUE_ATTEMPTS) {
        // Alertable: the response exists but nothing downstream of it will run — no webhook, no
        // follow-up email, no integration row, no workflow. The job id is logged because it is what a
        // replay has to reuse to stay idempotent.
        logger.error(
          { ...logContext, err: error, attempts: attempt, jobId },
          "Response pipeline event dropped after retries"
        );
        return;
      }

      logger.warn({ ...logContext, err: error, attempt }, "Response pipeline enqueue failed, retrying");
      await delay(ENQUEUE_RETRY_BASE_DELAY_MS * attempt);
    }
  }
};
