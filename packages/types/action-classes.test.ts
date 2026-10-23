import { describe, expect, test } from "vite-plus/test";
import { ZActionClassInput } from "./action-classes";

const input = (workspaceId: string) => ({
  name: "New Session",
  description: null,
  workspaceId,
  type: "noCode",
  noCodeConfig: { type: "pageView", urlFilters: [] },
});

describe("ZActionClassInput", () => {
  test("accepts the Go server's UUID workspace ids, including a uuidv5 migrated from a forma2 id", () => {
    expect(ZActionClassInput.safeParse(input("0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d0f")).success).toBe(true);
    expect(ZActionClassInput.safeParse(input("6f1c9a8e-2b4d-5e7f-8a9b-0c1d2e3f4a5b")).success).toBe(true);
  });

  test("still accepts forma2's cuid2 workspace ids", () => {
    expect(ZActionClassInput.safeParse(input("clx0workspace00000000000")).success).toBe(true);
  });

  test("rejects an empty or malformed workspace id", () => {
    expect(ZActionClassInput.safeParse(input("")).success).toBe(false);
    expect(ZActionClassInput.safeParse(input("Not-An-Id")).success).toBe(false);
    expect(ZActionClassInput.safeParse(input("0192f7a8-1c3e-7b2a-9d4f")).success).toBe(false);
  });
});
