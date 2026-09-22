import { type Job, type Queue, Worker } from "bullmq";
import type IORedis from "ioredis";
import { cpus } from "node:os";
import { logger } from "@forma/logger";
import { closeRedisConnection, createProducerConnection, createWorkerConnection } from "@/src/connection";
import { JOBS_PREFIX, JOBS_QUEUE_NAME } from "@/src/constants";
import type { JobHandlerOverrides } from "@/src/contracts";
import {
  type TJobOutcomeObservation,
  type TJobOutcomeStatus,
  recordJobOutcome,
  toBoundedJobName,
} from "@/src/observability";
import { processJob } from "@/src/processors/registry";
import { createJobsQueue } from "@/src/queue";

// In-flight jobs per worker. A concurrency of 1 serialises every job type behind the slowest handler, so one webhook timeout or a slow SMTP send stalls the response pipeline, workflow runs and AuthZed projection delivery alike. Handlers are written for overlap: they are required to be idempotent (see recurring.ts), the outbox claim is lease-based, and the workflow writes are status-guarded.
//
// Derived from the cpu count rather than fixed, because every in-flight job can hold a database connection and the Prisma pool is sized from the same number - `2 * cpus + 1`, minimum 2 (packages/database/src/prisma-adapter.ts). A flat default starves the request path on a small pod: at 2 cpus the pool is 5, so 4 job slots would leave the requests one connection. This formula keeps jobs to at most half the cpus and never more than 4, which leaves at least 3 connections for requests at every pod size we ship. Operators override it with `BULLMQ_WORKER_CONCURRENCY`.
export const DEFAULT_WORKER_CONCURRENCY = Math.max(2, Math.min(4, cpus().length));
const DEFAULT_WORKER_COUNT = 1;

export interface JobsRuntimeOptions {
  redisUrl: string;
  prefix?: string;
  concurrency?: number;
  workerCount?: number;
  jobHandlerOverrides?: JobHandlerOverrides;
}

export interface JobsRuntimeHandle {
  queue: Queue;
  workers: Worker[];
  close: () => Promise<void>;
}

type TSignalHandler = () => void;

const removeProcessListener = (event: "SIGTERM" | "SIGINT", handler: TSignalHandler): void => {
  process.removeListener(event, handler);
};

const getPositiveInteger = (value: number, label: string): number => {
  if (!Number.isInteger(value) || value < 1) {
    throw new Error(`${label} must be a positive integer`);
  }

  return value;
};

/**
 * Turns a settled BullMQ job into the bounded facts the observer reports.
 *
 * The wait is measured from the job's *intended* run time (`timestamp + delay`), not from when it was
 * created: recurring sweeps are queued as delayed jobs up to 24h ahead, so measuring from creation would
 * report a day of backlog on a queue that is empty. Clamped at zero because the two clocks are BullMQ's
 * own and a delayed job is picked up a few milliseconds early often enough to matter.
 *
 * `job` is optional on the `failed` event — BullMQ emits it without one when it cannot load the job
 * back — and the outcome is still counted, under the `unknown` bucket, so failures are never undercounted.
 */
const toJobOutcomeObservation = (job: Job | undefined, status: TJobOutcomeStatus): TJobOutcomeObservation => {
  const processedOn = job?.processedOn;
  const finishedOn = job?.finishedOn;
  const runnableAt = job === undefined ? undefined : job.timestamp + (job.delay || 0);

  return {
    attemptsMade: job?.attemptsMade ?? 0,
    durationMs:
      processedOn !== undefined && finishedOn !== undefined
        ? Math.max(0, finishedOn - processedOn)
        : undefined,
    jobName: toBoundedJobName(job?.name),
    status,
    waitDurationMs:
      processedOn !== undefined && runnableAt !== undefined
        ? Math.max(0, processedOn - runnableAt)
        : undefined,
  };
};

const registerWorkerLogging = (worker: Worker, workerNumber: number): void => {
  worker.on("error", (error) => {
    logger.error({ err: error, queueName: JOBS_QUEUE_NAME, workerNumber }, "BullMQ worker error");
  });

  worker.on("failed", (job, error) => {
    logger.error(
      {
        err: error,
        attemptsMade: job?.attemptsMade,
        jobId: job?.id,
        jobName: job?.name,
        queueName: job?.queueName,
        workerNumber,
      },
      "BullMQ job failed"
    );

    recordJobOutcome(toJobOutcomeObservation(job, "failed"));
  });

  worker.on("completed", (job) => {
    logger.debug(
      {
        attemptsMade: job.attemptsMade,
        jobId: job.id,
        jobName: job.name,
        queueName: job.queueName,
        workerNumber,
      },
      "BullMQ job completed"
    );

    recordJobOutcome(toJobOutcomeObservation(job, "completed"));
  });
};

export const startJobsRuntime = async ({
  redisUrl,
  prefix = JOBS_PREFIX,
  concurrency = DEFAULT_WORKER_CONCURRENCY,
  workerCount = DEFAULT_WORKER_COUNT,
  jobHandlerOverrides,
}: JobsRuntimeOptions): Promise<JobsRuntimeHandle> => {
  const resolvedConcurrency = getPositiveInteger(concurrency, "BullMQ worker concurrency");
  const resolvedWorkerCount = getPositiveInteger(workerCount, "BullMQ worker count");
  const producerConnection = createProducerConnection({
    redisUrl,
    connectionName: "forma-jobs-runtime-producer",
  });

  let queue: Queue | undefined;
  const workerConnections: IORedis[] = [];
  const workers: Worker[] = [];
  let closeRuntimePromise: Promise<void> | undefined;

  const closeRuntime = async (): Promise<void> => {
    if (!closeRuntimePromise) {
      closeRuntimePromise = (async () => {
        removeProcessListener("SIGTERM", handleSigterm);
        removeProcessListener("SIGINT", handleSigint);

        const closeConnectionSafely = async (connection: IORedis, connectionName: string): Promise<void> => {
          try {
            await closeRedisConnection(connection);
          } catch (error) {
            logger.error({ err: error, connectionName }, "Failed to close BullMQ Redis connection cleanly");
          }
        };

        await Promise.all(
          workers.map(async (worker, index) => {
            try {
              await worker.close();
            } catch (error) {
              logger.error({ err: error, workerNumber: index + 1 }, "Failed to close BullMQ worker cleanly");
            }
          })
        );

        if (queue) {
          try {
            await queue.close();
          } catch (error) {
            logger.error({ err: error }, "Failed to close BullMQ queue cleanly");
          }
        }

        await Promise.all([
          closeConnectionSafely(producerConnection, "producer"),
          ...workerConnections.map((workerConnection, index) =>
            closeConnectionSafely(workerConnection, `worker-${(index + 1).toString()}`)
          ),
        ]);
      })();
    }

    await closeRuntimePromise;
  };

  const handleSigterm = (): void => {
    void closeRuntime()
      .catch((error: unknown) => {
        logger.error({ err: error }, "BullMQ shutdown failed in closeRuntime after SIGTERM");
      })
      .finally(() => {
        process.exit(0);
      });
  };

  const handleSigint = (): void => {
    void closeRuntime()
      .catch((error: unknown) => {
        logger.error({ err: error }, "BullMQ shutdown failed in closeRuntime after SIGINT");
      })
      .finally(() => {
        process.exit(0);
      });
  };

  try {
    queue = createJobsQueue({ connection: producerConnection, prefix });

    for (let workerIndex = 0; workerIndex < resolvedWorkerCount; workerIndex++) {
      const workerConnection = createWorkerConnection({
        redisUrl,
        connectionName: `forma-jobs-runtime-worker-${(workerIndex + 1).toString()}`,
      });
      workerConnections.push(workerConnection);
      const worker = new Worker(
        JOBS_QUEUE_NAME,
        async (job: Job) => {
          await processJob(job, jobHandlerOverrides);
        },
        {
          connection: workerConnection,
          concurrency: resolvedConcurrency,
          prefix,
        }
      );

      workers.push(worker);
      registerWorkerLogging(worker, workerIndex + 1);
    }

    await Promise.all([queue.waitUntilReady(), ...workers.map((worker) => worker.waitUntilReady())]);

    process.once("SIGTERM", handleSigterm);
    process.once("SIGINT", handleSigint);

    logger.info(
      {
        queueName: JOBS_QUEUE_NAME,
        prefix,
        workerConcurrency: resolvedConcurrency,
        workerCount: resolvedWorkerCount,
      },
      "BullMQ runtime started"
    );

    return {
      queue,
      workers,
      close: closeRuntime,
    };
  } catch (error) {
    logger.error({ err: error, queueName: JOBS_QUEUE_NAME, prefix }, "Failed to start BullMQ runtime");
    await closeRuntime();
    throw error;
  }
};
