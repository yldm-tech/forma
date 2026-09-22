import { beforeEach, describe, expect, test, vi } from "vitest";

const counters = new Map<string, { add: ReturnType<typeof vi.fn> }>();
const histograms = new Map<string, { record: ReturnType<typeof vi.fn> }>();
const mockSetJobsObserver = vi.fn();

vi.mock("@opentelemetry/api", () => ({
  metrics: {
    getMeter: vi.fn(() => ({
      createCounter: vi.fn((name: string) => {
        const instrument = { add: vi.fn() };
        counters.set(name, instrument);
        return instrument;
      }),
      createHistogram: vi.fn((name: string) => {
        const instrument = { record: vi.fn() };
        histograms.set(name, instrument);
        return instrument;
      }),
    })),
  },
}));

vi.mock("@forma/jobs", () => ({
  setJobsObserver: mockSetJobsObserver,
}));

const { jobsMetricsObserver, registerJobsMetrics } = await import("./metrics");

const counter = (name: string) => counters.get(name)!;
const histogram = (name: string) => histograms.get(name)!;

beforeEach(() => {
  for (const instrument of counters.values()) {
    instrument.add.mockClear();
  }
  for (const instrument of histograms.values()) {
    instrument.record.mockClear();
  }
  mockSetJobsObserver.mockClear();
});

describe("registerJobsMetrics", () => {
  test("installs the observer @forma/jobs reports through", () => {
    registerJobsMetrics();

    expect(mockSetJobsObserver).toHaveBeenCalledWith(jobsMetricsObserver);
  });
});

describe("onEnqueue", () => {
  /**
   * The failure count is the only trace a dropped response pipeline event leaves: `sendToPipeline` never
   * throws, so the submission still returns 200 with no webhook, no follow-up email and no billing event.
   */
  test.each(["enqueued", "failed"] as const)("counts a %s enqueue by job name", (status) => {
    jobsMetricsObserver.onEnqueue?.({ jobName: "response-pipeline.process", status });

    expect(counter("forma_jobs_enqueue_total").add).toHaveBeenCalledWith(1, {
      job_name: "response-pipeline.process",
      status,
    });
  });
});

describe("onJobOutcome", () => {
  test("counts a settled job and records its handler duration in seconds", () => {
    jobsMetricsObserver.onJobOutcome?.({
      attemptsMade: 1,
      durationMs: 2_500,
      jobName: "response-pipeline.process",
      status: "completed",
      waitDurationMs: 4_000,
    });

    expect(counter("forma_jobs_processed_total").add).toHaveBeenCalledWith(1, {
      job_name: "response-pipeline.process",
      status: "completed",
    });
    expect(histogram("forma_jobs_duration_seconds").record).toHaveBeenCalledWith(2.5, {
      job_name: "response-pipeline.process",
      status: "completed",
    });
    expect(histogram("forma_jobs_wait_duration_seconds").record).toHaveBeenCalledWith(4, {
      job_name: "response-pipeline.process",
    });
  });

  /**
   * The wait histogram is the backlog signal, and a retried job keeps its original timestamp: BullMQ's
   * exponential backoff would otherwise report tens of seconds of queue wait on an idle queue every time
   * one webhook endpoint is flaky.
   */
  test("leaves a retried job out of the wait histogram but still counts it", () => {
    jobsMetricsObserver.onJobOutcome?.({
      attemptsMade: 3,
      durationMs: 1_000,
      jobName: "workflow-run.process",
      status: "failed",
      waitDurationMs: 15_000,
    });

    expect(counter("forma_jobs_processed_total").add).toHaveBeenCalledWith(1, {
      job_name: "workflow-run.process",
      status: "failed",
    });
    expect(histogram("forma_jobs_wait_duration_seconds").record).not.toHaveBeenCalled();
  });

  test("records nothing it was not given", () => {
    jobsMetricsObserver.onJobOutcome?.({
      attemptsMade: 0,
      durationMs: undefined,
      jobName: "unknown",
      status: "failed",
      waitDurationMs: undefined,
    });

    expect(counter("forma_jobs_processed_total").add).toHaveBeenCalledWith(1, {
      job_name: "unknown",
      status: "failed",
    });
    expect(histogram("forma_jobs_duration_seconds").record).not.toHaveBeenCalled();
    expect(histogram("forma_jobs_wait_duration_seconds").record).not.toHaveBeenCalled();
  });
});
