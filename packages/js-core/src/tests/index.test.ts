import { beforeEach, describe, expect, test, vi } from "vitest";
import type * as CommandQueueModule from "@/lib/common/command-queue";
import { CommandType } from "@/lib/common/command-queue";
import { checkPageUrl } from "@/lib/survey/no-code-action";

const mockQueue = {
  add: vi.fn().mockResolvedValue({ ok: true, data: undefined }),
  wait: vi.fn().mockResolvedValue(undefined),
};

vi.mock("@/lib/common/command-queue", async (importOriginal) => {
  const actual = await importOriginal<typeof CommandQueueModule>();
  return {
    ...actual,
    CommandQueue: {
      getInstance: vi.fn(() => mockQueue),
    },
  };
});

vi.mock("@/lib/survey/no-code-action", () => ({
  checkPageUrl: vi.fn().mockResolvedValue({ ok: true, data: undefined }),
}));

describe("public api", () => {
  beforeEach(() => {
    mockQueue.add.mockClear();
    vi.mocked(checkPageUrl).mockClear();
  });

  test("setup() schedules the page-view check through the command queue, not as a bare call", async () => {
    vi.useFakeTimers();

    const forma = (await import("@/index")).default;

    await forma.setup({ workspaceId: "ws_abc", appUrl: "https://fake.app" });

    mockQueue.add.mockClear();
    await vi.advanceTimersByTimeAsync(0);

    // Queued, so the queue's setup guard turns a failed setup into a console warning instead of an
    // unhandled rejection out of Config.get() on the host page.
    expect(mockQueue.add).toHaveBeenCalledWith(checkPageUrl, CommandType.GeneralAction);
    expect(vi.mocked(checkPageUrl)).not.toHaveBeenCalled();

    vi.useRealTimers();
  });
});
