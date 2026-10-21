import { beforeEach, describe, expect, test, vi } from "vitest";
import type { Prisma } from "@forma/database/prisma";
import { getBackgroundJobProducer } from "@forma/jobs";
import { getJobsQueueingConfig } from "@/lib/jobs/config";
import {
  drainResponsePipelineOutbox,
  processResponsePipelineOutboxBatch,
  processResponsePipelineOutboxJob,
} from "./outbox-drain";
import {
  type TResponsePipelineOutboxEntry,
  claimResponsePipelineOutboxEntries,
  getResponsePipelineOutboxStatus,
  markResponsePipelineOutboxEntriesDelivered,
  markResponsePipelineOutboxEntriesFailed,
  pruneResponsePipelineOutbox,
} from "./outbox-repository";

const mockEnqueueResponsePipeline = vi.fn();

vi.mock(import("@forma/jobs"), async (importOriginal) => ({
  ...(await importOriginal()),
  getBackgroundJobProducer: vi.fn(() => ({ enqueueResponsePipeline: mockEnqueueResponsePipeline })),
}));

vi.mock("@/lib/jobs/config", () => ({
  getJobsQueueingConfig: vi.fn(() => ({ enabled: true, redisUrl: "redis://localhost:6379" })),
}));

vi.mock("@forma/logger", () => ({
  logger: { error: vi.fn(), info: vi.fn(), warn: vi.fn() },
}));

vi.mock("./outbox-repository", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./outbox-repository")>()),
  claimResponsePipelineOutboxEntries: vi.fn(),
  createResponsePipelineOutboxLeaseOwner: vi.fn(() => "lease-owner"),
  getResponsePipelineOutboxStatus: vi.fn(),
  markResponsePipelineOutboxEntriesDelivered: vi.fn(),
  markResponsePipelineOutboxEntriesFailed: vi.fn(),
  pruneResponsePipelineOutbox: vi.fn(),
}));

const SURVEY_ID = "cm8ckvchx000008lb710n0gdn";
const WORKSPACE_ID = "cm8cmp9hp000008jf7l570ml2";

// Exactly what the column holds: `TResponsePipelineJobData` after a round trip through JSON, so every
// date is an ISO string rather than a Date.
const validPayload = (responseId: string) => ({
  event: "responseFinished",
  response: {
    contact: null,
    contactAttributes: null,
    createdAt: "2026-09-22T10:00:00.000Z",
    data: {},
    finished: true,
    id: responseId,
    language: null,
    meta: {},
    singleUseId: null,
    surveyId: SURVEY_ID,
    tags: [],
    updatedAt: "2026-09-22T10:00:00.000Z",
    variables: {},
  },
  surveyId: SURVEY_ID,
  workspaceId: WORKSPACE_ID,
});

const entry = (id: string, payload: unknown): TResponsePipelineOutboxEntry => ({
  attempts: 1,
  createdAt: new Date("2026-09-22T10:00:00.000Z"),
  event: "responseFinished",
  id,
  jobId: `response-pipeline:responseFinished:${id}:1758535200000`,
  payload: payload as Prisma.JsonValue,
  responseId: id,
  surveyId: SURVEY_ID,
  workspaceId: WORKSPACE_ID,
});

describe("processResponsePipelineOutboxBatch", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(markResponsePipelineOutboxEntriesFailed).mockResolvedValue(0);
    vi.mocked(getResponsePipelineOutboxStatus).mockResolvedValue({
      deadLettered: 0,
      oldestPendingAgeSeconds: null,
      pending: 0,
    });
  });

  test("touches nothing when there is no backlog, which is the steady state", async () => {
    vi.mocked(claimResponsePipelineOutboxEntries).mockResolvedValue([]);

    const result = await processResponsePipelineOutboxBatch("lease-owner");

    expect(result).toEqual({ claimed: 0, deadLettered: 0, delivered: 0, failed: 0 });
    expect(getBackgroundJobProducer).not.toHaveBeenCalled();
    expect(markResponsePipelineOutboxEntriesDelivered).not.toHaveBeenCalled();
  });

  test("replays each row under the jobId the lost attempt would have used", async () => {
    const rows = [
      entry("cm8cmpnjj000108jfdr9dfqe6", validPayload("cm8cmpnjj000108jfdr9dfqe6")),
      entry("cm8cmpnjj000108jfdr9dfqe7", validPayload("cm8cmpnjj000108jfdr9dfqe7")),
    ];
    vi.mocked(claimResponsePipelineOutboxEntries).mockResolvedValue(rows);
    mockEnqueueResponsePipeline.mockResolvedValue({});

    const result = await processResponsePipelineOutboxBatch("lease-owner");

    // The id is what makes the replay safe against a fast path that in fact succeeded: BullMQ will not
    // add an id it already holds, so a row whose event did reach the queue is a no-op here.
    expect(mockEnqueueResponsePipeline.mock.calls.map(([, options]) => options)).toEqual([
      { jobId: rows[0].jobId },
      { jobId: rows[1].jobId },
    ]);
    expect(markResponsePipelineOutboxEntriesDelivered).toHaveBeenCalledWith("lease-owner", [
      rows[0].id,
      rows[1].id,
    ]);
    expect(result).toMatchObject({ claimed: 2, delivered: 2, failed: 0 });
  });

  test("parses the stored payload rather than trusting it, and dead-letters what will not parse", async () => {
    const rows = [
      entry("cm8cmpnjj000108jfdr9dfqe6", { event: "responseFinished", response: { id: 42 } }),
      entry("cm8cmpnjj000108jfdr9dfqe7", validPayload("cm8cmpnjj000108jfdr9dfqe7")),
    ];
    vi.mocked(claimResponsePipelineOutboxEntries).mockResolvedValue(rows);
    mockEnqueueResponsePipeline.mockResolvedValue({});
    // Only the first release call — the one carrying the unparsable row — dead-letters anything.
    vi.mocked(markResponsePipelineOutboxEntriesFailed).mockResolvedValueOnce(1);

    const result = await processResponsePipelineOutboxBatch("lease-owner");

    // Waiting cannot turn a malformed payload into a valid one, so it dead-letters on its first pass
    // instead of holding a lease and an attempt slot for the next twenty.
    expect(markResponsePipelineOutboxEntriesFailed).toHaveBeenCalledWith(
      "lease-owner",
      [rows[0].id],
      expect.any(String),
      { permanent: true }
    );
    expect(mockEnqueueResponsePipeline).toHaveBeenCalledTimes(1);
    expect(result).toMatchObject({ deadLettered: 1, delivered: 1, failed: 1 });
  });

  test("stops at the first enqueue failure and releases the rest untried", async () => {
    const rows = [
      entry("cm8cmpnjj000108jfdr9dfqe6", validPayload("cm8cmpnjj000108jfdr9dfqe6")),
      entry("cm8cmpnjj000108jfdr9dfqe7", validPayload("cm8cmpnjj000108jfdr9dfqe7")),
      entry("cm8cmpnjj000108jfdr9dfqe8", validPayload("cm8cmpnjj000108jfdr9dfqe8")),
    ];
    vi.mocked(claimResponsePipelineOutboxEntries).mockResolvedValue(rows);
    mockEnqueueResponsePipeline.mockResolvedValueOnce({}).mockRejectedValue(new Error("Redis unavailable"));

    const result = await processResponsePipelineOutboxBatch("lease-owner");

    // One reason an enqueue fails here, and it is the queue rather than the row: trying the remaining
    // rows would spend a round trip each to learn what the first already reported.
    expect(mockEnqueueResponsePipeline).toHaveBeenCalledTimes(2);
    expect(markResponsePipelineOutboxEntriesFailed).toHaveBeenCalledWith(
      "lease-owner",
      [rows[1].id, rows[2].id],
      "Redis unavailable",
      { permanent: false }
    );
    expect(result).toMatchObject({ claimed: 3, delivered: 1, failed: 2 });
  });
});

describe("drainResponsePipelineOutbox", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(markResponsePipelineOutboxEntriesFailed).mockResolvedValue(0);
    vi.mocked(getResponsePipelineOutboxStatus).mockResolvedValue({
      deadLettered: 0,
      oldestPendingAgeSeconds: null,
      pending: 4,
    });
  });

  test("stops looping once a pass delivers nothing, instead of re-leasing rows that cannot move", async () => {
    const rows = [entry("cm8cmpnjj000108jfdr9dfqe6", validPayload("cm8cmpnjj000108jfdr9dfqe6"))];
    vi.mocked(claimResponsePipelineOutboxEntries).mockResolvedValue(rows);
    mockEnqueueResponsePipeline.mockRejectedValue(new Error("Redis unavailable"));

    const result = await drainResponsePipelineOutbox(5);

    expect(claimResponsePipelineOutboxEntries).toHaveBeenCalledTimes(1);
    expect(result).toMatchObject({ claimed: 1, delivered: 0, failed: 1, remaining: 4 });
  });

  test("reports the backlog left behind, which is the signal an operator acts on", async () => {
    vi.mocked(claimResponsePipelineOutboxEntries).mockResolvedValue([]);

    await expect(drainResponsePipelineOutbox(5)).resolves.toMatchObject({ remaining: 4 });
  });
});

describe("processResponsePipelineOutboxJob", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(getJobsQueueingConfig).mockReturnValue({ enabled: true, redisUrl: "redis://localhost:6379" });
    vi.mocked(claimResponsePipelineOutboxEntries).mockResolvedValue([]);
    vi.mocked(getResponsePipelineOutboxStatus).mockResolvedValue({
      deadLettered: 0,
      oldestPendingAgeSeconds: null,
      pending: 0,
    });
    vi.mocked(pruneResponsePipelineOutbox).mockResolvedValue(0);
  });

  test("retires settled rows in the same pass, because the payload holds respondent answers", async () => {
    await processResponsePipelineOutboxJob();

    expect(pruneResponsePipelineOutbox).toHaveBeenCalledTimes(1);
  });

  test("does nothing at all when the queue this drains into is not configured", async () => {
    vi.mocked(getJobsQueueingConfig).mockReturnValue({ enabled: false, redisUrl: null });

    await processResponsePipelineOutboxJob();

    expect(claimResponsePipelineOutboxEntries).not.toHaveBeenCalled();
    expect(pruneResponsePipelineOutbox).not.toHaveBeenCalled();
  });
});
