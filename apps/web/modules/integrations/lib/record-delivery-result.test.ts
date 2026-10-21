import { beforeEach, describe, expect, test, vi } from "vitest";
import { prisma } from "@forma/database";
import { logger } from "@forma/logger";
import { recordIntegrationResult } from "./record-delivery-result";

vi.mock("server-only", () => ({}));

vi.mock("@forma/database", () => ({
  prisma: {
    integration: {
      updateMany: vi.fn(),
    },
  },
}));

vi.mock("@forma/logger", () => ({
  logger: {
    error: vi.fn(),
  },
}));

const target = {
  id: "int_1",
  workspaceId: "ws_1",
  type: "airtable" as const,
};

describe("recordIntegrationResult", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(prisma.integration.updateMany).mockResolvedValue({ count: 1 });
  });

  test("issues no statement when a healthy integration delivers", async () => {
    await recordIntegrationResult({ ...target, consecutiveFailures: 0 }, { ok: true });

    expect(prisma.integration.updateMany).not.toHaveBeenCalled();
  });

  test("increments and stamps on a failure, scoped to the workspace", async () => {
    await recordIntegrationResult(
      { ...target, consecutiveFailures: 0 },
      { ok: false, error: new Error("base not found") }
    );

    expect(prisma.integration.updateMany).toHaveBeenCalledTimes(1);
    const call = vi.mocked(prisma.integration.updateMany).mock.calls[0][0];
    expect(call.where).toEqual({ id: "int_1", workspaceId: "ws_1" });
    expect(call.data).toMatchObject({
      consecutiveFailures: { increment: 1 },
      lastErrorMessage: "base not found",
      lastErrorAt: expect.any(Date),
    });
  });

  test("clears the row on the first success after a failure", async () => {
    await recordIntegrationResult({ ...target, consecutiveFailures: 3 }, { ok: true });

    expect(prisma.integration.updateMany).toHaveBeenCalledWith({
      where: { id: "int_1", workspaceId: "ws_1", consecutiveFailures: { gt: 0 } },
      data: { consecutiveFailures: 0, lastErrorAt: null, lastErrorMessage: null },
    });
  });

  test("keeps the recovery write conditional when the counter was not loaded", async () => {
    await recordIntegrationResult(target, { ok: true });

    expect(vi.mocked(prisma.integration.updateMany).mock.calls[0][0].where).toEqual({
      id: "int_1",
      workspaceId: "ws_1",
      consecutiveFailures: { gt: 0 },
    });
  });

  test("swallows a write failure so it cannot escape the response pipeline", async () => {
    vi.mocked(prisma.integration.updateMany).mockRejectedValue(new Error("connection lost"));

    await expect(
      recordIntegrationResult({ ...target, consecutiveFailures: 0 }, { ok: false, error: new Error("nope") })
    ).resolves.toBeUndefined();
    expect(logger.error).toHaveBeenCalledTimes(1);
  });

  test("does not store a credential the provider echoed back", async () => {
    await recordIntegrationResult(
      { ...target, consecutiveFailures: 0 },
      { ok: false, error: new Error('rejected {"access_token":"patABC.secret"}') }
    );

    expect(vi.mocked(prisma.integration.updateMany).mock.calls[0][0].data).toMatchObject({
      lastErrorMessage: expect.not.stringContaining("patABC.secret"),
    });
  });
});
