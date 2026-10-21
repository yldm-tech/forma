import { beforeEach, describe, expect, test, vi } from "vitest";
import { PipelineTriggers } from "@forma/database/prisma";
import { TResponsePipelineJobData, getBackgroundJobProducer } from "@forma/jobs";
import { logger } from "@forma/logger";
import { TResponse } from "@forma/types/responses";
import { getJobsQueueingConfig } from "@/lib/jobs/config";
import { sendToPipeline } from "@/lib/pipelines";
import { findMatchingLocale } from "@/lib/utils/locale";
import { recordDroppedResponsePipelineEvent } from "@/modules/response-pipeline/lib/outbox-repository";

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

vi.mock("@/modules/response-pipeline/lib/outbox-repository", () => ({
  recordDroppedResponsePipelineEvent: vi.fn(),
  toOutboxErrorMessage: (error: unknown) => (error instanceof Error ? error.message : String(error)),
}));

describe("sendToPipeline", () => {
  const responseUpdatedAt = new Date("2026-09-22T10:00:00.000Z");
  const testData: TResponsePipelineJobData = {
    event: PipelineTriggers.responseCreated,
    surveyId: "cm8ckvchx000008lb710n0gdn",
    workspaceId: "cm8cmp9hp000008jf7l570ml2",
    response: { id: "cm8cmpnjj000108jfdr9dfqe6", updatedAt: responseUpdatedAt } as TResponse,
  };
  const expectedJobId = `response-pipeline:responseCreated:cm8cmpnjj000108jfdr9dfqe6:${responseUpdatedAt.getTime()}`;

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
    expect(mockEnqueueResponsePipeline).toHaveBeenCalledWith(
      { ...testData, locale: "en-US" },
      { jobId: expectedJobId }
    );
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
        responseId: testData.response.id,
        surveyId: testData.surveyId,
        workspaceId: testData.workspaceId,
      }),
      "Response pipeline event deferred to the outbox after retries"
    );
  });

  test("reuses one jobId across the whole retry loop, so a retry cannot double-deliver", async () => {
    mockEnqueueResponsePipeline.mockRejectedValueOnce(new Error("Redis unavailable")).mockResolvedValueOnce({
      jobId: "job-1",
      jobName: "response-pipeline.process",
      queueName: "background-jobs",
    });

    await sendToPipeline(testData);

    // Both attempts carry the same deterministic id, so if the first attempt in fact reached Valkey and
    // only its acknowledgement was lost, the second is a no-op at the queue rather than a second run of
    // every webhook, follow-up email and billing event hanging off this response.
    expect(mockEnqueueResponsePipeline.mock.calls.map(([, options]) => options)).toEqual([
      { jobId: expectedJobId },
      { jobId: expectedJobId },
    ]);
  });

  test("gives different jobIds to two events about the same response version", async () => {
    mockEnqueueResponsePipeline.mockResolvedValue({
      jobId: "job-1",
      jobName: "response-pipeline.process",
      queueName: "background-jobs",
    });

    await sendToPipeline(testData);
    await sendToPipeline({ ...testData, event: PipelineTriggers.responseFinished });

    const [firstOptions, secondOptions] = mockEnqueueResponsePipeline.mock.calls.map(([, opts]) => opts);
    expect(firstOptions).not.toEqual(secondOptions);
  });

  test("enqueues without a jobId rather than a fabricated one when the response has no version", async () => {
    mockEnqueueResponsePipeline.mockResolvedValue({
      jobId: "job-1",
      jobName: "response-pipeline.process",
      queueName: "background-jobs",
    });

    await sendToPipeline({ ...testData, response: { id: "cm8cmpnjj000108jfdr9dfqe6" } as TResponse });

    expect(mockEnqueueResponsePipeline).toHaveBeenCalledWith(expect.objectContaining({}), undefined);
  });

  test("writes the event to the outbox once the retry budget is exhausted", async () => {
    mockEnqueueResponsePipeline.mockRejectedValue(new Error("Redis unavailable"));

    await sendToPipeline(testData);

    expect(recordDroppedResponsePipelineEvent).toHaveBeenCalledWith(
      expectedJobId,
      { ...testData, locale: "en-US" },
      "Redis unavailable"
    );
  });

  test("still does not throw when the outbox write fails as well", async () => {
    mockEnqueueResponsePipeline.mockRejectedValue(new Error("Redis unavailable"));
    vi.mocked(recordDroppedResponsePipelineEvent).mockRejectedValueOnce(new Error("response deleted"));

    // The response row is committed and its foreign key cascades, so this insert can legitimately lose
    // a race with a deletion. Escaping here would be the 500 the never-throw contract exists to prevent.
    await expect(sendToPipeline(testData)).resolves.toBeUndefined();

    expect(logger.error).toHaveBeenCalledWith(
      expect.objectContaining({ responseId: testData.response.id }),
      "Response pipeline outbox write failed"
    );
    expect(logger.error).toHaveBeenCalledWith(
      expect.objectContaining({ recovered: false }),
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

    expect(mockEnqueueResponsePipeline).toHaveBeenCalledWith(
      { ...testData, locale: undefined },
      { jobId: expectedJobId }
    );
  });

  test("preserves an existing job.locale instead of resolving it", async () => {
    mockEnqueueResponsePipeline.mockResolvedValue({
      jobId: "job-1",
      jobName: "response-pipeline.process",
      queueName: "background-jobs",
    });

    await sendToPipeline({ ...testData, locale: "de-DE" });

    expect(findMatchingLocale).not.toHaveBeenCalled();
    expect(mockEnqueueResponsePipeline).toHaveBeenCalledWith(
      { ...testData, locale: "de-DE" },
      { jobId: expectedJobId }
    );
  });
});
