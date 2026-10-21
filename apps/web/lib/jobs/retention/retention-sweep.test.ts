import { beforeEach, describe, expect, test, vi } from "vitest";
import { prisma } from "@forma/database";
import { logger } from "@forma/logger";
import {
  RETENTION_SWEEP_DELETE_BATCH_SIZE,
  RETENTION_SWEEP_MAX_DELETE_BATCHES,
  RETENTION_SWEEP_SURVEY_PAGE_SIZE,
} from "./constants";
import { getRetentionCutoff, runRetentionSweep } from "./retention-sweep";

vi.mock("@forma/database", () => ({
  prisma: {
    display: { count: vi.fn() },
    organization: { findMany: vi.fn() },
    survey: { findMany: vi.fn() },
    workflowRun: { count: vi.fn() },
    workspace: { findMany: vi.fn() },
    $executeRaw: vi.fn(),
  },
}));

vi.mock("@forma/logger", () => ({
  logger: { error: vi.fn(), info: vi.fn(), warn: vi.fn() },
}));

const NOW = new Date("2026-09-22T00:00:00.000Z");

interface TOrganizationRow {
  id: string;
  displayRetentionDays: number | null;
  workflowRunRetentionDays: number | null;
}

const makeOrganization = (overrides: Partial<TOrganizationRow> = {}): TOrganizationRow => ({
  id: "org_1",
  displayRetentionDays: null,
  workflowRunRetentionDays: null,
  ...overrides,
});

/** The raw SQL a `$executeRaw` call was made with, reassembled from the tagged-template fragments. */
const executedSql = (callIndex: number): string => {
  const [fragments] = vi.mocked(prisma.$executeRaw).mock.calls[callIndex] as unknown as [string[]];
  return fragments.join(" ? ");
};

beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(prisma.workspace.findMany).mockResolvedValue([{ id: "ws_1" }] as never);
  vi.mocked(prisma.survey.findMany).mockResolvedValue([] as never);
  vi.mocked(prisma.workflowRun.count).mockResolvedValue(0 as never);
  vi.mocked(prisma.display.count).mockResolvedValue(0 as never);
  vi.mocked(prisma.$executeRaw).mockResolvedValue(0 as never);
});

describe("getRetentionCutoff", () => {
  test("subtracts whole days from the given instant", () => {
    expect(getRetentionCutoff(NOW, 90).toISOString()).toBe("2026-06-24T00:00:00.000Z");
  });
});

describe("runRetentionSweep", () => {
  test("asks only for organizations that configured a window", async () => {
    vi.mocked(prisma.organization.findMany).mockResolvedValue([] as never);

    await runRetentionSweep(NOW, true);

    expect(prisma.organization.findMany).toHaveBeenCalledWith(
      expect.objectContaining({
        where: {
          OR: [{ workflowRunRetentionDays: { not: null } }, { displayRetentionDays: { not: null } }],
        },
      })
    );
  });

  // Null means "keep forever", which disables the pass — never "use a default window".
  test("sweeps nothing for an organization whose windows are both null", async () => {
    vi.mocked(prisma.organization.findMany).mockResolvedValue([makeOrganization()] as never);

    const result = await runRetentionSweep(NOW, false);

    expect(result.organizations).toBe(0);
    expect(prisma.$executeRaw).not.toHaveBeenCalled();
    expect(prisma.workspace.findMany).not.toHaveBeenCalled();
    expect(prisma.survey.findMany).not.toHaveBeenCalled();
  });

  // A zero or negative window would otherwise delete the organization's entire history.
  test("skips a window that is not a positive whole number of days", async () => {
    vi.mocked(prisma.organization.findMany).mockResolvedValue([
      makeOrganization({ workflowRunRetentionDays: 0 }),
      makeOrganization({ displayRetentionDays: -1, id: "org_2" }),
    ] as never);

    const result = await runRetentionSweep(NOW, false);

    expect(result.invalidWindows).toBe(2);
    expect(result.organizations).toBe(0);
    expect(prisma.$executeRaw).not.toHaveBeenCalled();
    expect(logger.warn).toHaveBeenCalledTimes(2);
  });

  test("dry run reports matched rows and deletes nothing", async () => {
    vi.mocked(prisma.organization.findMany).mockResolvedValue([
      makeOrganization({ displayRetentionDays: 90, workflowRunRetentionDays: 30 }),
    ] as never);
    vi.mocked(prisma.survey.findMany).mockResolvedValueOnce([{ id: "survey_1" }] as never);
    vi.mocked(prisma.workflowRun.count).mockResolvedValue(4 as never);
    vi.mocked(prisma.display.count).mockResolvedValue(7 as never);

    const result = await runRetentionSweep(NOW, true);

    // One count per terminal status for the workspace, and one per survey for displays.
    expect(result.workflowRuns).toEqual({ cappedUnits: 0, deleted: 0, matched: 12 });
    expect(result.displays).toEqual({ cappedUnits: 0, deleted: 0, matched: 7 });
    expect(result.dryRun).toBe(true);
    expect(prisma.$executeRaw).not.toHaveBeenCalled();
  });

  test("counts terminal workflow runs per status, never queued or running ones", async () => {
    vi.mocked(prisma.organization.findMany).mockResolvedValue([
      makeOrganization({ workflowRunRetentionDays: 30 }),
    ] as never);

    await runRetentionSweep(NOW, true);

    const statuses = vi
      .mocked(prisma.workflowRun.count)
      .mock.calls.map(([args]) => (args as { where: { status: string } }).where.status);
    expect(statuses).toEqual(["completed", "failed", "canceled"]);
    expect(prisma.workflowRun.count).toHaveBeenCalledWith({
      where: { createdAt: { lt: getRetentionCutoff(NOW, 30) }, status: "completed", workspaceId: "ws_1" },
    });
  });

  // `Response_displayId_fkey` is ON DELETE SET NULL, so a converted impression has to be excluded by the
  // predicate — the foreign key would blank the response's displayId rather than refuse the delete.
  test("excludes converted impressions from both the count and the delete", async () => {
    vi.mocked(prisma.organization.findMany).mockResolvedValue([
      makeOrganization({ displayRetentionDays: 90 }),
    ] as never);
    vi.mocked(prisma.survey.findMany).mockResolvedValueOnce([{ id: "survey_1" }] as never);

    await runRetentionSweep(NOW, true);
    expect(prisma.display.count).toHaveBeenCalledWith({
      where: { createdAt: { lt: getRetentionCutoff(NOW, 90) }, response: { is: null }, surveyId: "survey_1" },
    });

    vi.mocked(prisma.survey.findMany).mockResolvedValueOnce([{ id: "survey_1" }] as never);
    await runRetentionSweep(NOW, false);
    expect(executedSql(0)).toContain("NOT EXISTS");
    expect(executedSql(0)).toContain('FROM "Response" AS response');
  });

  test("deletes in bounded batches and stops on a short one", async () => {
    vi.mocked(prisma.organization.findMany).mockResolvedValue([
      makeOrganization({ workflowRunRetentionDays: 30 }),
    ] as never);
    vi.mocked(prisma.$executeRaw)
      .mockResolvedValueOnce(RETENTION_SWEEP_DELETE_BATCH_SIZE as never)
      .mockResolvedValue(0 as never);

    const result = await runRetentionSweep(NOW, false);

    // Two statements for `completed` (a full batch then an empty one), one each for the other statuses.
    expect(prisma.$executeRaw).toHaveBeenCalledTimes(4);
    expect(result.workflowRuns.deleted).toBe(RETENTION_SWEEP_DELETE_BATCH_SIZE);
    expect(result.workflowRuns.cappedUnits).toBe(0);
    expect(executedSql(0)).toContain(`LIMIT`);
  });

  test("defers the remainder to the next tick instead of looping forever", async () => {
    vi.mocked(prisma.organization.findMany).mockResolvedValue([
      makeOrganization({ workflowRunRetentionDays: 30 }),
    ] as never);
    vi.mocked(prisma.$executeRaw).mockResolvedValue(RETENTION_SWEEP_DELETE_BATCH_SIZE as never);

    const result = await runRetentionSweep(NOW, false);

    expect(result.workflowRuns.cappedUnits).toBe(3);
    expect(prisma.$executeRaw).toHaveBeenCalledTimes(3 * RETENTION_SWEEP_MAX_DELETE_BATCHES);
  });

  test("pages an organization's surveys instead of loading them all", async () => {
    const fullPage = Array.from({ length: RETENTION_SWEEP_SURVEY_PAGE_SIZE }, (_, index) => ({
      id: `survey_${index}`,
    }));
    vi.mocked(prisma.organization.findMany).mockResolvedValue([
      makeOrganization({ displayRetentionDays: 90 }),
    ] as never);
    vi.mocked(prisma.survey.findMany)
      .mockResolvedValueOnce(fullPage as never)
      .mockResolvedValueOnce([{ id: "survey_last" }] as never);

    await runRetentionSweep(NOW, true);

    expect(prisma.survey.findMany).toHaveBeenCalledTimes(2);
    expect(prisma.survey.findMany).toHaveBeenLastCalledWith(
      expect.objectContaining({
        cursor: { id: `survey_${RETENTION_SWEEP_SURVEY_PAGE_SIZE - 1}` },
        skip: 1,
      })
    );
    expect(prisma.display.count).toHaveBeenCalledTimes(RETENTION_SWEEP_SURVEY_PAGE_SIZE + 1);
  });

  test("keeps sweeping after one organization fails", async () => {
    vi.mocked(prisma.organization.findMany).mockResolvedValue([
      makeOrganization({ workflowRunRetentionDays: 30 }),
      makeOrganization({ id: "org_2", workflowRunRetentionDays: 30 }),
    ] as never);
    vi.mocked(prisma.workspace.findMany)
      .mockRejectedValueOnce(new Error("connection lost"))
      .mockResolvedValue([{ id: "ws_2" }] as never);
    vi.mocked(prisma.workflowRun.count).mockResolvedValue(5 as never);

    const result = await runRetentionSweep(NOW, true);

    expect(result.workflowRuns.matched).toBe(15);
    expect(logger.error).toHaveBeenCalledTimes(1);
  });
});
