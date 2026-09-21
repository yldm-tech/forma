import { beforeEach, describe, expect, test, vi } from "vitest";
import { PipelineTriggers } from "@forma/database/prisma";
import { TResponsePipelineJobData, getBackgroundJobProducer } from "@forma/jobs";
import { logger } from "@forma/logger";
import { TResponse } from "@forma/types/responses";
import { getJobsQueueingConfig } from "@/lib/jobs/config";
import { sendToPipeline } from "@/lib/pipelines";
import { findMatchingLocale } from "@/lib/utils/locale";

const mockEnqueueResponsePipeline = vi.fn();

vi.mock("@forma/jobs", () => ({
  getBackgroundJobProducer: vi.fn(() => ({
    enqueueResponsePipeline: mockEnqueueResponsePipeline,
  })),
}));

vi.mock("@/lib/jobs/config", () => ({
  getJobsQueueingConfig: vi.fn(),
}));

vi.mock("@/lib/utils/locale", () => ({
  findMatchingLocale: vi.fn(() => Promise.resolve("en-US")),
}));

vi.mock("@forma/logger", () => ({
  logger: {
    error: vi.fn(),
    warn: vi.fn(),
  },
}));

describe("sendToPipeline", () => {
  const testData: TResponsePipelineJobData = {
    event: PipelineTriggers.responseCreated,
    surveyId: "cm8ckvchx000008lb710n0gdn",
    workspaceId: "cm8cmp9hp000008jf7l570ml2",
    response: { id: "cm8cmpnjj000108jfdr9dfqe6" } as TResponse,
  };

  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(getJobsQueueingConfig).mockReturnValue({
      enabled: true,
      redisUrl: "redis://localhost:6379",
    });
  });

  test("enqueues the pipeline job through the BullMQ producer", async () => {
    mockEnqueueResponsePipeline.mockResolvedValue({
      jobId: "job-1",
      jobName: "response-pipeline.process",
      queueName: "background-jobs",
    });

    await sendToPipeline(testData);

    expect(getBackgroundJobProducer).toHaveBeenCalledTimes(1);
    expect(mockEnqueueResponsePipeline).toHaveBeenCalledWith({ ...testData, locale: "en-US" });
  });

  test("retries a failing enqueue rather than surfacing it on the first attempt", async () => {
    const testError = new Error("Redis unavailable");
    mockEnqueueResponsePipeline.mockRejectedValueOnce(testError).mockResolvedValueOnce({
      jobId: "job-1",
      jobName: "response-pipeline.process",
      queueName: "background-jobs",
    });

    await expect(sendToPipeline(testData)).resolves.toBeUndefined();

    // The producer runs with enableOfflineQueue: false, so a sub-second reconnect fails instantly;
    // one retry is enough to cover it.
    expect(mockEnqueueResponsePipeline).toHaveBeenCalledTimes(2);
    expect(logger.error).not.toHaveBeenCalled();
  });

  test("gives up without throwing, because the response row is already committed", async () => {
    const testError = new Error("Redis unavailable");
    mockEnqueueResponsePipeline.mockRejectedValue(testError);

    // Throwing here turned into a 500 on a request whose response already exists. The survey runtime
    // retries 5xx and only records responseId after a successful create, so each retry POSTed
    // another response - one submission becoming up to four rows.
    await expect(sendToPipeline(testData)).resolves.toBeUndefined();

    expect(logger.error).toHaveBeenCalledWith(
      expect.objectContaining({
        event: testData.event,
        surveyId: testData.surveyId,
        workspaceId: testData.workspaceId,
        responseId: testData.response.id,
      }),
      "Response pipeline event dropped after retries"
    );
  });

  test("does not throw when BullMQ queueing is disabled", async () => {
    vi.mocked(getJobsQueueingConfig).mockReturnValue({
      enabled: false,
      redisUrl: null,
    });

    await expect(sendToPipeline(testData)).resolves.toBeUndefined();
    expect(getBackgroundJobProducer).not.toHaveBeenCalled();
    expect(logger.error).toHaveBeenCalledWith(
      expect.objectContaining({ event: testData.event }),
      "Response pipeline event dropped: BullMQ queueing is not enabled"
    );
  });

  test("falls back to undefined locale when findMatchingLocale throws", async () => {
    vi.mocked(findMatchingLocale).mockRejectedValueOnce(new Error("headers unavailable"));
    mockEnqueueResponsePipeline.mockResolvedValue({
      jobId: "job-1",
      jobName: "response-pipeline.process",
      queueName: "background-jobs",
    });

    await sendToPipeline(testData);

    expect(mockEnqueueResponsePipeline).toHaveBeenCalledWith({ ...testData, locale: undefined });
  });

  test("preserves an existing job.locale instead of resolving it", async () => {
    mockEnqueueResponsePipeline.mockResolvedValue({
      jobId: "job-1",
      jobName: "response-pipeline.process",
      queueName: "background-jobs",
    });

    await sendToPipeline({ ...testData, locale: "de-DE" });

    expect(findMatchingLocale).not.toHaveBeenCalled();
    expect(mockEnqueueResponsePipeline).toHaveBeenCalledWith({ ...testData, locale: "de-DE" });
  });
});
