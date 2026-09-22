import { type Queue, QueueEvents } from "bullmq";
import IORedis from "ioredis";
import { afterAll, beforeAll, describe, expect, test, vi } from "vitest";
import { logger } from "@forma/logger";
import { JOBS_DEFAULT_JOB_OPTIONS, JOBS_PREFIX, JOBS_QUEUE_NAME, JOB_NAMES } from "./constants";
import {
  enqueueTestLogJob,
  resetJobsQueueFactory,
  scheduleTestLogJobAt,
  upsertRecurringTestLogJobSchedule,
} from "./queue";
import { startJobsRuntime } from "./runtime";
import { getRecurringJobSchedulerId } from "./schedules";

/**
 * A healthy job scheduler always keeps exactly one of its jobs pending — that job is what produces the
 * next one. An `every` schedule's first iteration is due immediately, so it starts in `waiting`; once a
 * worker has taken it, the following iteration sits in `delayed`. BullMQ names both
 * `repeat:<schedulerId>:<timestamp>`.
 */
const getPendingJobIdsForScheduler = async (queue: Queue, schedulerId: string): Promise<string[]> => {
  const pendingJobs = await queue.getJobs(["delayed", "prioritized", "waiting"]);

  return pendingJobs
    .map((job) => job.id)
    .filter((jobId): jobId is string => jobId !== undefined && jobId.startsWith(`repeat:${schedulerId}:`));
};

let redisUrl!: string;
let runtime: Awaited<ReturnType<typeof startJobsRuntime>> | null = null;
let queueEvents: QueueEvents | null = null;
let queueEventsConnection: IORedis | null = null;

async function isQueueReady(url: string): Promise<boolean> {
  let probe: Awaited<ReturnType<typeof startJobsRuntime>> | null = null;

  try {
    probe = await startJobsRuntime({ redisUrl: url });
    return true;
  } catch (error) {
    logger.info({ error }, "BullMQ integration tests skipped because Redis is not available");
    return false;
  } finally {
    if (probe) {
      try {
        await probe.close();
      } catch (error) {
        logger.warn({ err: error }, "Failed to close BullMQ runtime probe cleanly");
      }
    }
  }
}

/** Resolves REDIS_URL and confirms a BullMQ runtime can actually start against it. */
async function isQueueConfiguredAndReady(): Promise<boolean> {
  const configuredRedisUrl = process.env.REDIS_URL;

  if (!configuredRedisUrl) {
    logger.info("BullMQ integration tests skipped because REDIS_URL is not configured");
    return false;
  }

  if (!(await isQueueReady(configuredRedisUrl))) {
    return false;
  }

  redisUrl = configuredRedisUrl;
  return true;
}

// Probed at collection time, not in `beforeAll`: `describe.skipIf` has to know the answer before the suite
// is registered, and that is what makes a Redis-less run report these tests as SKIPPED. An early `return`
// inside each test body reported them as PASSED instead, so the suite claimed coverage it never ran.
const isRedisAvailable = await isQueueConfiguredAndReady();

/**
 * The suite is skipped when Redis is absent, so `beforeAll` has run by the time any test body does. The
 * throw is what turns a removed skip guard into a loud failure rather than a silent pass.
 */
const requireQueueEvents = (): QueueEvents => {
  if (!queueEvents) {
    throw new Error("QueueEvents was not initialised by beforeAll");
  }

  return queueEvents;
};

const requireRuntime = (): NonNullable<typeof runtime> => {
  if (!runtime) {
    throw new Error("The jobs runtime was not initialised by beforeAll");
  }

  return runtime;
};

describe.skipIf(!isRedisAvailable)("BullMQ integration tests", () => {
  beforeAll(async () => {
    runtime = await startJobsRuntime({ redisUrl });
    queueEventsConnection = new IORedis(redisUrl, {
      connectionName: "forma-jobs-queue-events",
      maxRetriesPerRequest: null,
    });
    queueEvents = new QueueEvents(JOBS_QUEUE_NAME, {
      connection: queueEventsConnection,
      prefix: JOBS_PREFIX,
    });
    await queueEvents.waitUntilReady();
  });

  afterAll(async () => {
    if (queueEvents) {
      await queueEvents.close();
    }

    if (queueEventsConnection) {
      await queueEventsConnection.quit();
    }

    if (runtime) {
      await runtime.close();
    }

    await resetJobsQueueFactory();
  });

  test("processes the test log job end-to-end", async () => {
    const events = requireQueueEvents();

    const job = await enqueueTestLogJob({ message: "integration success" });

    // `null`, not `undefined`: BullMQ round-trips the processor's return value through JSON, so a
    // handler returning nothing reads back as null.
    await expect(job.waitUntilFinished(events)).resolves.toBeNull();
    expect(job.name).toBe(JOB_NAMES.testLog);
    expect(job.opts.attempts).toBe(JOBS_DEFAULT_JOB_OPTIONS.attempts);
    expect(job.opts.backoff).toEqual(JOBS_DEFAULT_JOB_OPTIONS.backoff);
  }, 15000);

  test("retries and fails the test log job when instructed", async () => {
    const events = requireQueueEvents();

    const errorSpy = vi.spyOn(logger, "error");
    const job = await enqueueTestLogJob({ message: "integration failure", shouldFail: true });

    await expect(job.waitUntilFinished(events)).rejects.toThrow();

    // `waitUntilFinished` resolves off the QueueEvents stream, which can beat the worker's own `failed`
    // listener to logging, so poll rather than assert the final attempt has already been recorded.
    await vi.waitFor(
      () => {
        expect(errorSpy).toHaveBeenCalledWith(
          expect.objectContaining({
            attemptsMade: JOBS_DEFAULT_JOB_OPTIONS.attempts,
            jobId: job.id,
            jobName: JOB_NAMES.testLog,
          }),
          "BullMQ job failed"
        );
      },
      { interval: 100, timeout: 10_000 }
    );
  }, 35000);

  test("processes delayed jobs after their scheduled time", async () => {
    const events = requireQueueEvents();

    const startedAt = Date.now();
    const scheduledFor = startedAt + 500;
    const job = await scheduleTestLogJobAt(
      { runAt: new Date(scheduledFor) },
      { message: "integration delayed success" }
    );

    await expect(job.waitUntilFinished(events)).resolves.toBeNull();
    expect(Date.now()).toBeGreaterThanOrEqual(scheduledFor);
  }, 15000);

  test("upserts a recurring schedule that actually produces jobs", async () => {
    const { queue } = requireRuntime();
    const debugSpy = vi.spyOn(logger, "debug");
    const scope = "integration-tests";
    const message = `integration recurring ${Date.now().toString()}`;
    const scheduleId = `integration-recurring-${Date.now().toString()}`;
    const schedulerId = getRecurringJobSchedulerId(JOB_NAMES.testLog, { scheduleId, scope });

    try {
      const scheduledJob = await upsertRecurringTestLogJobSchedule(
        { scheduleId, scope },
        { everyMs: 200, kind: "every", limit: 2 },
        { message }
      );

      await vi.waitFor(
        () => {
          const processorLogs = debugSpy.mock.calls.filter((call) => call[1] === message);
          expect(processorLogs).toHaveLength(2);
        },
        { interval: 100, timeout: 10_000 }
      );

      expect(scheduledJob.name).toBe(JOB_NAMES.testLog);
      expect(scheduledJob.queueName).toBe(JOBS_QUEUE_NAME);
      expect(scheduledJob.id).toEqual(expect.any(String));
    } finally {
      // Otherwise every run leaves a scheduler behind in the shared integration Redis.
      await queue.removeJobScheduler(schedulerId);
    }
  }, 15000);

  test("keeps producing work when a scheduler's repeat options change", async () => {
    const { queue } = requireRuntime();
    const scope = "integration-tests";
    const scheduleId = `integration-reupsert-${Date.now().toString()}`;
    const schedulerId = getRecurringJobSchedulerId(JOB_NAMES.testLog, { scheduleId, scope });
    // An hour out, so the worker cannot consume the pending job while the queue is inspected.
    const upsertEvery = async (everyMs: number): Promise<void> => {
      await upsertRecurringTestLogJobSchedule(
        { scheduleId, scope },
        { everyMs, kind: "every" },
        { message: `integration re-upsert ${scheduleId}` }
      );
    };

    const expectExactlyOnePendingJob = async (): Promise<void> => {
      await vi.waitFor(
        async () => {
          const pendingJobIds = await getPendingJobIdsForScheduler(queue, schedulerId);
          expect(pendingJobIds).toHaveLength(1);
        },
        { interval: 100, timeout: 5_000 }
      );
    };

    try {
      await upsertEvery(60 * 60 * 1000);

      await expectExactlyOnePendingJob();

      // Changing the repeat options on an existing scheduler must leave it still producing work. This is
      // the shape bullmq#3063 breaks when a remove precedes the upsert: the scheduler survives with a
      // correct next-run time but no job pending, so it never fires again.
      await upsertEvery(2 * 60 * 60 * 1000);

      await expectExactlyOnePendingJob();

      const schedulers = await queue.getJobSchedulers();
      expect(schedulers.map((scheduler) => scheduler.key)).toContain(schedulerId);
    } finally {
      await queue.removeJobScheduler(schedulerId);
    }
  }, 15000);
});
