import { afterEach, describe, expect, test, vi } from "vitest";
import { JOB_NAMES } from "./constants";
import {
  UNKNOWN_JOB_NAME,
  recordJobEnqueue,
  recordJobOutcome,
  setJobsObserver,
  toBoundedJobName,
} from "./observability";

afterEach(() => {
  setJobsObserver(undefined);
});

describe("toBoundedJobName", () => {
  test.each(Object.values(JOB_NAMES))("keeps the registered job name %s", (jobName) => {
    expect(toBoundedJobName(jobName)).toBe(jobName);
  });

  /**
   * A recurring schedule lives in Redis, so it outlives the build that understood it and an arbitrary
   * name reaches the worker's listeners. Unbucketed it would be a metric attribute an outside writer
   * chooses — a cardinality leak that survives every restart.
   */
  test.each([["response-pipeline.process-v2"], [""], ["a".repeat(512)]])(
    "buckets the unregistered job name %j as unknown",
    (jobName) => {
      expect(toBoundedJobName(jobName)).toBe(UNKNOWN_JOB_NAME);
    }
  );

  test("buckets a missing job name as unknown", () => {
    expect(toBoundedJobName(undefined)).toBe(UNKNOWN_JOB_NAME);
  });
});

describe("job observations", () => {
  test("reach the registered observer", () => {
    const onEnqueue = vi.fn();
    const onJobOutcome = vi.fn();
    setJobsObserver({ onEnqueue, onJobOutcome });

    recordJobEnqueue({ jobName: JOB_NAMES.responsePipeline, status: "failed" });
    recordJobOutcome({
      attemptsMade: 1,
      durationMs: 120,
      jobName: JOB_NAMES.responsePipeline,
      status: "completed",
      waitDurationMs: 4_000,
    });

    expect(onEnqueue).toHaveBeenCalledWith({ jobName: JOB_NAMES.responsePipeline, status: "failed" });
    expect(onJobOutcome).toHaveBeenCalledWith({
      attemptsMade: 1,
      durationMs: 120,
      jobName: JOB_NAMES.responsePipeline,
      status: "completed",
      waitDurationMs: 4_000,
    });
  });

  test("are a no-op when no observer is registered", () => {
    expect(() => {
      recordJobEnqueue({ jobName: JOB_NAMES.testLog, status: "enqueued" });
    }).not.toThrow();
  });

  /**
   * Observation can never change an outcome. A throwing exporter here would turn a delivered job into a
   * failed one, or an accepted enqueue into a dropped response pipeline event — the exact loss the
   * counter exists to reveal.
   */
  test("never let an observer failure escape to the caller", () => {
    setJobsObserver({
      onEnqueue: () => {
        throw new Error("exporter unavailable");
      },
      onJobOutcome: () => {
        throw new Error("exporter unavailable");
      },
    });

    expect(() => {
      recordJobEnqueue({ jobName: JOB_NAMES.responsePipeline, status: "enqueued" });
    }).not.toThrow();
    expect(() => {
      recordJobOutcome({
        attemptsMade: 1,
        durationMs: undefined,
        jobName: JOB_NAMES.responsePipeline,
        status: "completed",
        waitDurationMs: undefined,
      });
    }).not.toThrow();
  });

  test("stop reaching an observer once it is cleared", () => {
    const onEnqueue = vi.fn();
    setJobsObserver({ onEnqueue });
    setJobsObserver(undefined);

    recordJobEnqueue({ jobName: JOB_NAMES.testLog, status: "enqueued" });

    expect(onEnqueue).not.toHaveBeenCalled();
  });
});
