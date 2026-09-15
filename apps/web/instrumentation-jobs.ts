import { type JobHandlerOverrides, type JobsRuntimeHandle, startJobsRuntime } from "@forma/jobs";
import { logger } from "@forma/logger";
import { getJobsQueueingConfig, getJobsWorkerBootstrapConfig } from "@/lib/jobs/config";
import { RECURRING_JOB_REGISTRATIONS, getJobHandlerOverrides } from "@/lib/jobs/recurring-registrations";

const WORKER_STARTUP_RETRY_DELAY_MS = 30_000;

type TJobsRuntimeGlobal = typeof globalThis & {
  formaJobsRecurringRegistration: Promise<void> | undefined;
  formaJobsRecurringRegistered: boolean | undefined;
  formaJobsRecurringRetryTimeout: ReturnType<typeof setTimeout> | undefined;
  formaJobsRuntime: JobsRuntimeHandle | undefined;
  formaJobsRuntimeInitializing: Promise<JobsRuntimeHandle> | undefined;
  formaJobsRuntimeRetryTimeout: ReturnType<typeof setTimeout> | undefined;
};

const globalForJobsRuntime = globalThis as TJobsRuntimeGlobal;

/**
 * Upsert only — never remove first. `upsertJobScheduler` updates an existing scheduler's repeat options
 * in place (including switching between cron and every), so a remove is unnecessary; it is also harmful.
 * Removing and re-upserting in close succession can leave the scheduler with **no** delayed job at all
 * (bullmq#3063: the upsert finds a job already holding the expected id, but that job is already
 * completed), so the schedule reports a correct next run and never fires again. It also opens a window
 * with no schedule, which a crash between the two calls makes permanent until the next boot.
 *
 * The remove-first this replaces was a reasonable workaround for bullmq#3378 — upsert not updating an
 * existing scheduler — which affected v5.56.9 and earlier. We are on 5.61.0, past that fix.
 *
 * Known trade-off, verified against a real Redis: upsert updates the scheduler's repeat options but
 * leaves the run it has already queued alone, so a changed cron pattern or time zone takes effect from
 * the *next* iteration — up to 24h later for the daily sweeps, 3 minutes for the reconciler. The
 * remove-first moved that pending run immediately. That is the price of never leaving the schedule
 * without a queued run, and it is the right way round: a config change landing one cycle late is
 * recoverable, a schedule that silently stops firing is not.
 */
const registerRecurringJobSchedules = async (): Promise<void> => {
  for (const registration of RECURRING_JOB_REGISTRATIONS) {
    await registration.job.upsert(registration.schedule);
  }
};

const clearRecurringJobsRetryTimeout = (): void => {
  if (globalForJobsRuntime.formaJobsRecurringRetryTimeout) {
    clearTimeout(globalForJobsRuntime.formaJobsRecurringRetryTimeout);
    globalForJobsRuntime.formaJobsRecurringRetryTimeout = undefined;
  }
};

const scheduleRecurringJobsRetry = (): void => {
  if (
    globalForJobsRuntime.formaJobsRecurringRegistered ||
    globalForJobsRuntime.formaJobsRecurringRegistration ||
    globalForJobsRuntime.formaJobsRecurringRetryTimeout
  ) {
    return;
  }

  globalForJobsRuntime.formaJobsRecurringRetryTimeout = setTimeout(() => {
    globalForJobsRuntime.formaJobsRecurringRetryTimeout = undefined;
    void registerRecurringJobs().catch(() => undefined);
  }, WORKER_STARTUP_RETRY_DELAY_MS);

  logger.warn(
    { retryDelayMs: WORKER_STARTUP_RETRY_DELAY_MS },
    "BullMQ recurring job registration retry scheduled"
  );
};

const clearJobsWorkerRetryTimeout = (): void => {
  if (globalForJobsRuntime.formaJobsRuntimeRetryTimeout) {
    clearTimeout(globalForJobsRuntime.formaJobsRuntimeRetryTimeout);
    globalForJobsRuntime.formaJobsRuntimeRetryTimeout = undefined;
  }
};

const scheduleJobsWorkerRetry = (): void => {
  if (
    globalForJobsRuntime.formaJobsRuntime ||
    globalForJobsRuntime.formaJobsRuntimeInitializing ||
    globalForJobsRuntime.formaJobsRuntimeRetryTimeout
  ) {
    return;
  }

  globalForJobsRuntime.formaJobsRuntimeRetryTimeout = setTimeout(() => {
    globalForJobsRuntime.formaJobsRuntimeRetryTimeout = undefined;
    void registerJobsWorker().catch(() => undefined);
  }, WORKER_STARTUP_RETRY_DELAY_MS);

  logger.warn({ retryDelayMs: WORKER_STARTUP_RETRY_DELAY_MS }, "BullMQ worker registration retry scheduled");
};

export const registerRecurringJobs = async (): Promise<void> => {
  const jobsQueueingConfig = getJobsQueueingConfig();

  if (!jobsQueueingConfig.enabled || !jobsQueueingConfig.redisUrl) {
    clearRecurringJobsRetryTimeout();
    logger.debug("BullMQ recurring job registration skipped");
    return;
  }

  if (globalForJobsRuntime.formaJobsRecurringRegistered) {
    return;
  }

  if (globalForJobsRuntime.formaJobsRecurringRegistration) {
    return await globalForJobsRuntime.formaJobsRecurringRegistration;
  }

  globalForJobsRuntime.formaJobsRecurringRegistration = (async () => {
    await registerRecurringJobSchedules();
    clearRecurringJobsRetryTimeout();
    globalForJobsRuntime.formaJobsRecurringRegistered = true;
    globalForJobsRuntime.formaJobsRecurringRegistration = undefined;
  })();

  try {
    return await globalForJobsRuntime.formaJobsRecurringRegistration;
  } catch (error) {
    globalForJobsRuntime.formaJobsRecurringRegistration = undefined;
    logger.error({ err: error }, "BullMQ recurring job registration failed");
    scheduleRecurringJobsRetry();
    throw error;
  }
};

export const registerJobsWorker = async (): Promise<JobsRuntimeHandle | null> => {
  const jobsWorkerBootstrapConfig = getJobsWorkerBootstrapConfig();

  if (!jobsWorkerBootstrapConfig.enabled || !jobsWorkerBootstrapConfig.runtimeOptions) {
    clearJobsWorkerRetryTimeout();
    logger.debug("BullMQ worker startup skipped");
    return null;
  }

  if (globalForJobsRuntime.formaJobsRuntime) {
    return globalForJobsRuntime.formaJobsRuntime;
  }

  if (globalForJobsRuntime.formaJobsRuntimeInitializing) {
    return await globalForJobsRuntime.formaJobsRuntimeInitializing;
  }

  const runtimeOptions = jobsWorkerBootstrapConfig.runtimeOptions;
  // The app's handlers win over anything the bootstrap config supplied.
  const jobHandlerOverrides: JobHandlerOverrides = {
    ...runtimeOptions.jobHandlerOverrides,
    ...getJobHandlerOverrides(),
  };

  globalForJobsRuntime.formaJobsRuntimeInitializing = (async () => {
    const runtime = await startJobsRuntime({
      ...runtimeOptions,
      jobHandlerOverrides,
    });

    clearJobsWorkerRetryTimeout();
    globalForJobsRuntime.formaJobsRuntime = runtime;
    globalForJobsRuntime.formaJobsRuntimeInitializing = undefined;
    return runtime;
  })();

  try {
    return await globalForJobsRuntime.formaJobsRuntimeInitializing;
  } catch (error) {
    globalForJobsRuntime.formaJobsRuntimeInitializing = undefined;
    logger.error({ err: error }, "BullMQ worker registration failed");
    scheduleJobsWorkerRetry();
    throw error;
  }
};

export const resetJobsWorkerRegistrationForTests = async (): Promise<void> => {
  const runtime = globalForJobsRuntime.formaJobsRuntime;
  const initializing = globalForJobsRuntime.formaJobsRuntimeInitializing;
  clearRecurringJobsRetryTimeout();
  clearJobsWorkerRetryTimeout();
  globalForJobsRuntime.formaJobsRecurringRegistered = undefined;
  globalForJobsRuntime.formaJobsRecurringRegistration = undefined;
  globalForJobsRuntime.formaJobsRuntime = undefined;
  globalForJobsRuntime.formaJobsRuntimeInitializing = undefined;

  const runtimesToClose = new Set<JobsRuntimeHandle>();

  if (runtime) {
    runtimesToClose.add(runtime);
  }

  if (initializing) {
    try {
      const initializedRuntime = await initializing;
      runtimesToClose.add(initializedRuntime);
    } catch {
      // Startup failures are already surfaced by the test that triggered them.
    }
  }

  if (globalForJobsRuntime.formaJobsRuntime) {
    runtimesToClose.add(globalForJobsRuntime.formaJobsRuntime);
  }

  globalForJobsRuntime.formaJobsRuntime = undefined;
  globalForJobsRuntime.formaJobsRuntimeInitializing = undefined;

  await Promise.all(
    [...runtimesToClose].map(async (runtimeHandle) => {
      try {
        await runtimeHandle.close();
      } catch (error) {
        logger.error({ err: error }, "BullMQ worker test reset close failed");
      }
    })
  );
};
