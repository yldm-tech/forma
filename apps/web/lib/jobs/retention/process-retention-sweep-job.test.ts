import { beforeEach, describe, expect, test, vi } from "vitest";
import type { JobExecutionContext } from "@forma/jobs";
import { logger } from "@forma/logger";
import { processRetentionSweepJob } from "./process-retention-sweep-job";
import { runRetentionSweep } from "./retention-sweep";

vi.mock("@forma/logger", () => ({
  logger: { error: vi.fn(), info: vi.fn(), warn: vi.fn() },
}));

vi.mock("./retention-sweep", () => ({
  runRetentionSweep: vi.fn(),
}));

const context: JobExecutionContext = {
  attempt: 1,
  jobId: "job_1",
  jobName: "retention-sweep.process",
  maxAttempts: 3,
  queueName: "background-jobs",
};

const passResult = { cappedUnits: 0, deleted: 0, matched: 12 };

beforeEach(() => {
  vi.clearAllMocks();
});

describe("processRetentionSweepJob", () => {
  test("logs what a dry run matched, so an operator sees the volume before any row goes", async () => {
    vi.mocked(runRetentionSweep).mockResolvedValue({
      displays: passResult,
      dryRun: true,
      invalidWindows: 0,
      organizations: 1,
      workflowRuns: passResult,
    });

    await processRetentionSweepJob({ scope: "global" }, context);

    expect(logger.info).toHaveBeenLastCalledWith(
      expect.objectContaining({ dryRun: true, organizations: 1, workflowRuns: passResult }),
      "Retention sweep job completed (dry run, nothing deleted)"
    );
  });

  test("names the completion line differently once rows are really deleted", async () => {
    vi.mocked(runRetentionSweep).mockResolvedValue({
      displays: { cappedUnits: 0, deleted: 5, matched: 0 },
      dryRun: false,
      invalidWindows: 0,
      organizations: 1,
      workflowRuns: { cappedUnits: 1, deleted: 9, matched: 0 },
    });

    await processRetentionSweepJob({ scope: "global" }, context);

    expect(logger.info).toHaveBeenLastCalledWith(
      expect.objectContaining({ dryRun: false }),
      "Retention sweep job completed"
    );
  });
});
