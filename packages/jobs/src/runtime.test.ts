import { cpus } from "node:os";
import { beforeEach, describe, expect, test, vi } from "vitest";
import {
  type MockRedisConnection,
  type MockWorker,
  asQueue,
  asRedisConnection,
  asWorker,
  createMockLogger,
  createMockQueue,
  createMockRedisConnection,
  createMockWorker,
} from "../test/boundary-mocks";
import { JOBS_PREFIX, JOBS_QUEUE_NAME, JOB_NAMES } from "./constants";
import { UNKNOWN_JOB_NAME, setJobsObserver } from "./observability";
import type * as QueueModule from "./queue";
import { DEFAULT_WORKER_CONCURRENCY, startJobsRuntime } from "./runtime";

type TQueueModule = typeof QueueModule;

let producerConnection = createMockRedisConnection();
let queueMock = createMockQueue();
let workerConnections: MockRedisConnection[] = [];
let workerMocks: MockWorker[] = [];

const mockLogger = createMockLogger();
const mockProcessJob = vi
  .fn<(job: unknown, handlerOverrides?: unknown) => Promise<void>>()
  .mockResolvedValue(undefined);
const mockCloseRedisConnection = vi.fn<(connection: unknown) => Promise<void>>().mockResolvedValue(undefined);
const mockCreateProducerConnection = vi.fn((_: unknown) => asRedisConnection(producerConnection));
const mockCreateWorkerConnection = vi.fn((_: unknown) => {
  const workerConnection = createMockRedisConnection();
  workerConnections.push(workerConnection);
  return asRedisConnection(workerConnection);
});
const mockCreateJobsQueue = vi.fn((_: unknown) => asQueue(queueMock));
const mockWorkerConstructor = vi.fn(function MockWorker(_: string, __: unknown, ___?: unknown) {
  const worker = createMockWorker();
  workerMocks.push(worker);
  return asWorker(worker);
});

vi.mock("@forma/logger", () => ({
  logger: {
    error: (context: unknown, message?: string): void => {
      mockLogger.error(context, message);
    },
    info: (context: unknown, message?: string): void => {
      mockLogger.info(context, message);
    },
    warn: (context: unknown, message?: string): void => {
      mockLogger.warn(context, message);
    },
    debug: (context: unknown, message?: string): void => {
      mockLogger.debug(context, message);
    },
  },
}));

vi.mock("./connection", () => ({
  createProducerConnection: (config: unknown) => mockCreateProducerConnection(config),
  createWorkerConnection: (config: unknown) => mockCreateWorkerConnection(config),
  closeRedisConnection: (connection: unknown) => mockCloseRedisConnection(connection),
}));

vi.mock("./processors/registry", () => ({
  processJob: (job: unknown, handlerOverrides?: unknown) => mockProcessJob(job, handlerOverrides),
}));

vi.mock("./queue", async () => {
  const actual: TQueueModule = await vi.importActual("./queue");

  return {
    ...actual,
    createJobsQueue: (options: unknown) => mockCreateJobsQueue(options),
  };
});

vi.mock("bullmq", () => ({
  Queue: vi.fn(),
  Worker: function MockWorker(queueName: string, processor: unknown, options?: unknown) {
    return mockWorkerConstructor(queueName, processor, options);
  },
}));

describe("@forma/jobs runtime", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    producerConnection = createMockRedisConnection();
    queueMock = createMockQueue();
    workerConnections = [];
    workerMocks = [];
  });

  test("starts a worker with the expected queue, prefix, and processor bridge", async () => {
    const runtime = await startJobsRuntime({ redisUrl: "redis://localhost:6379" });

    expect(runtime.workers).toHaveLength(1);
    expect(mockWorkerConstructor).toHaveBeenCalledWith(
      JOBS_QUEUE_NAME,
      expect.any(Function),
      expect.objectContaining({
        concurrency: DEFAULT_WORKER_CONCURRENCY,
        prefix: JOBS_PREFIX,
      })
    );

    const worker = workerMocks[0];
    expect(worker.on).toHaveBeenCalledWith("error", expect.any(Function));
    expect(worker.on).toHaveBeenCalledWith("failed", expect.any(Function));
    expect(worker.on).toHaveBeenCalledWith("completed", expect.any(Function));

    const processor = mockWorkerConstructor.mock.calls[0]?.[1] as (job: unknown) => Promise<void>;
    const job = {
      attemptsMade: 1,
      id: "job-1",
      name: JOB_NAMES.testLog,
      queueName: JOBS_QUEUE_NAME,
    };
    await processor(job);

    expect(mockProcessJob).toHaveBeenCalledWith(job, undefined);

    const registeredWorkerEvents = new Map<string, (...args: unknown[]) => void>(
      worker.on.mock.calls.map(([event, handler]) => [event, handler])
    );
    const workerError = new Error("worker error");
    const failedError = new Error("job failed");

    registeredWorkerEvents.get("error")?.(workerError);
    registeredWorkerEvents.get("failed")?.(
      {
        attemptsMade: 2,
        id: "job-2",
        name: JOB_NAMES.testLog,
        queueName: JOBS_QUEUE_NAME,
      },
      failedError
    );
    registeredWorkerEvents.get("completed")?.({
      attemptsMade: 1,
      id: "job-3",
      name: JOB_NAMES.testLog,
      queueName: JOBS_QUEUE_NAME,
    });

    expect(mockLogger.error).toHaveBeenCalledWith(
      { err: workerError, queueName: JOBS_QUEUE_NAME, workerNumber: 1 },
      "BullMQ worker error"
    );
    expect(mockLogger.error).toHaveBeenCalledWith(
      expect.objectContaining({
        attemptsMade: 2,
        err: failedError,
        jobId: "job-2",
        jobName: JOB_NAMES.testLog,
        queueName: JOBS_QUEUE_NAME,
        workerNumber: 1,
      }),
      "BullMQ job failed"
    );
    expect(mockLogger.debug).toHaveBeenCalledWith(
      {
        attemptsMade: 1,
        jobId: "job-3",
        jobName: JOB_NAMES.testLog,
        queueName: JOBS_QUEUE_NAME,
        workerNumber: 1,
      },
      "BullMQ job completed"
    );

    await runtime.close();

    expect(worker.close).toHaveBeenCalledTimes(1);
    expect(queueMock.close).toHaveBeenCalledTimes(1);
    expect(mockCloseRedisConnection).toHaveBeenCalledTimes(2);
  });

  /**
   * The queue that carries every survey response emitted nothing an operator could alert on: with a
   * single worker it can grow without bound while logging nothing, because nothing fails — jobs wait.
   * The wait is measured from the job's intended run time so a recurring sweep queued 24h ahead does not
   * report a day of backlog on an empty queue.
   */
  test("reports settled jobs to the observer with a delay-corrected wait", async () => {
    const onJobOutcome = vi.fn();
    setJobsObserver({ onJobOutcome });

    const runtime = await startJobsRuntime({ redisUrl: "redis://localhost:6379" });

    try {
      const worker = workerMocks[0];
      const registeredWorkerEvents = new Map<string, (...args: unknown[]) => void>(
        worker.on.mock.calls.map(([event, handler]) => [event, handler])
      );

      registeredWorkerEvents.get("completed")?.({
        attemptsMade: 1,
        delay: 60_000,
        finishedOn: 1_000_250,
        id: "job-1",
        name: JOB_NAMES.surveyScheduling,
        processedOn: 1_000_000,
        queueName: JOBS_QUEUE_NAME,
        timestamp: 936_000,
      });

      expect(onJobOutcome).toHaveBeenCalledWith({
        attemptsMade: 1,
        durationMs: 250,
        jobName: JOB_NAMES.surveyScheduling,
        status: "completed",
        waitDurationMs: 4_000,
      });
    } finally {
      setJobsObserver(undefined);
      await runtime.close();
    }
  });

  /**
   * BullMQ emits `failed` without a job when it cannot load one back, and the job name it does emit
   * comes from Redis — a schedule outliving its code feeds an arbitrary string in. Both are counted, and
   * both land in the bounded `unknown` bucket rather than becoming a metric attribute Redis chooses.
   */
  test("counts failures under a bounded job name, with or without a job", async () => {
    const onJobOutcome = vi.fn();
    setJobsObserver({ onJobOutcome });

    const runtime = await startJobsRuntime({ redisUrl: "redis://localhost:6379" });

    try {
      const worker = workerMocks[0];
      const registeredWorkerEvents = new Map<string, (...args: unknown[]) => void>(
        worker.on.mock.calls.map(([event, handler]) => [event, handler])
      );

      registeredWorkerEvents.get("failed")?.(
        {
          attemptsMade: 3,
          delay: 0,
          id: "job-2",
          name: "response-pipeline.process-v2",
          queueName: JOBS_QUEUE_NAME,
          timestamp: 1_000_000,
        },
        new Error("job failed")
      );
      registeredWorkerEvents.get("failed")?.(undefined, new Error("job lost"));

      expect(onJobOutcome).toHaveBeenNthCalledWith(1, {
        attemptsMade: 3,
        durationMs: undefined,
        jobName: UNKNOWN_JOB_NAME,
        status: "failed",
        waitDurationMs: undefined,
      });
      expect(onJobOutcome).toHaveBeenNthCalledWith(2, {
        attemptsMade: 0,
        durationMs: undefined,
        jobName: UNKNOWN_JOB_NAME,
        status: "failed",
        waitDurationMs: undefined,
      });
    } finally {
      setJobsObserver(undefined);
      await runtime.close();
    }
  });

  test("starts multiple workers when configured", async () => {
    const runtime = await startJobsRuntime({
      redisUrl: "redis://localhost:6379",
      concurrency: 6,
      workerCount: 2,
    });

    expect(runtime.workers).toHaveLength(2);
    expect(mockWorkerConstructor).toHaveBeenCalledTimes(2);
    expect(mockWorkerConstructor).toHaveBeenNthCalledWith(
      1,
      JOBS_QUEUE_NAME,
      expect.any(Function),
      expect.objectContaining({
        concurrency: 6,
        prefix: JOBS_PREFIX,
      })
    );
    expect(mockWorkerConstructor).toHaveBeenNthCalledWith(
      2,
      JOBS_QUEUE_NAME,
      expect.any(Function),
      expect.objectContaining({
        concurrency: 6,
        prefix: JOBS_PREFIX,
      })
    );

    await runtime.close();

    expect(workerMocks[0]?.close).toHaveBeenCalledTimes(1);
    expect(workerMocks[1]?.close).toHaveBeenCalledTimes(1);
  });

  test("passes handler overrides into the processor bridge", async () => {
    const overrideHandler = vi.fn().mockResolvedValue(undefined);
    const runtime = await startJobsRuntime({
      redisUrl: "redis://localhost:6379",
      jobHandlerOverrides: {
        [JOB_NAMES.responsePipeline]: overrideHandler,
      },
    });

    const processor = mockWorkerConstructor.mock.calls[0]?.[1] as (job: unknown) => Promise<void>;
    const job = {
      attemptsMade: 0,
      id: "job-override",
      name: JOB_NAMES.responsePipeline,
      queueName: JOBS_QUEUE_NAME,
    };

    await processor(job);

    expect(mockProcessJob).toHaveBeenCalledWith(job, {
      [JOB_NAMES.responsePipeline]: overrideHandler,
    });

    await runtime.close();
  });

  test("rejects invalid runtime tuning values", async () => {
    await expect(startJobsRuntime({ redisUrl: "redis://localhost:6379", concurrency: 0 })).rejects.toThrow(
      "BullMQ worker concurrency must be a positive integer"
    );
    await expect(startJobsRuntime({ redisUrl: "redis://localhost:6379", workerCount: 0 })).rejects.toThrow(
      "BullMQ worker count must be a positive integer"
    );

    expect(mockWorkerConstructor).not.toHaveBeenCalled();
  });

  test("cleans up workers and connections when startup fails", async () => {
    const startupError = new Error("queue not ready");
    queueMock.waitUntilReady.mockRejectedValueOnce(startupError);

    await expect(startJobsRuntime({ redisUrl: "redis://localhost:6379", workerCount: 2 })).rejects.toThrow(
      "queue not ready"
    );

    expect(workerMocks).toHaveLength(2);
    expect(workerMocks[0]?.close).toHaveBeenCalledTimes(1);
    expect(workerMocks[1]?.close).toHaveBeenCalledTimes(1);
    expect(queueMock.close).toHaveBeenCalledTimes(1);
    expect(mockCloseRedisConnection).toHaveBeenCalledTimes(3);
    expect(mockLogger.error).toHaveBeenCalledWith(
      { err: startupError, queueName: JOBS_QUEUE_NAME, prefix: JOBS_PREFIX },
      "Failed to start BullMQ runtime"
    );
  });

  test("deduplicates concurrent close calls", async () => {
    const runtime = await startJobsRuntime({ redisUrl: "redis://localhost:6379" });

    await Promise.all([runtime.close(), runtime.close()]);

    expect(workerMocks[0]?.close).toHaveBeenCalledTimes(1);
    expect(queueMock.close).toHaveBeenCalledTimes(1);
    expect(mockCloseRedisConnection).toHaveBeenCalledTimes(2);
  });

  test("logs signal shutdown failures", async () => {
    const processOnceSpy = vi.spyOn(process, "once");
    const processExitSpy = vi.spyOn(process, "exit").mockImplementation((() => undefined) as never);
    await startJobsRuntime({ redisUrl: "redis://localhost:6379" });
    const sigtermRegistration = processOnceSpy.mock.calls.find(
      (call): call is ["SIGTERM", () => void] => call[0] === "SIGTERM"
    );

    expect(sigtermRegistration).toBeDefined();

    mockCloseRedisConnection.mockRejectedValueOnce(new Error("connection close failed"));

    sigtermRegistration?.[1]();

    await vi.waitFor(() => {
      const shutdownFailureCall = mockLogger.error.mock.calls.find(
        (call) => call[1] === "Failed to close BullMQ Redis connection cleanly"
      ) as [unknown, string] | undefined;

      expect(shutdownFailureCall).toBeDefined();
      const shutdownFailureContext = shutdownFailureCall?.[0] as { err?: unknown } | undefined;
      expect(shutdownFailureContext?.err).toBeInstanceOf(Error);
    });

    expect(processExitSpy).toHaveBeenCalledWith(0);

    processExitSpy.mockRestore();
    processOnceSpy.mockRestore();
  });

  // The default is derived from the cpu count, so the invariant is the bound, not the number: it must
  // stay well inside the Prisma pool (`2 * cpus + 1`, min 2) so background work cannot starve the
  // request path on a small pod.
  test("keeps the derived worker concurrency inside the database pool", () => {
    const pool = Math.max(2 * cpus().length + 1, 2);

    expect(DEFAULT_WORKER_CONCURRENCY).toBeGreaterThanOrEqual(2);
    expect(DEFAULT_WORKER_CONCURRENCY).toBeLessThanOrEqual(4);
    expect(pool - DEFAULT_WORKER_CONCURRENCY).toBeGreaterThanOrEqual(1);
  });
});
