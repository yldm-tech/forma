import type { TResponseTableData } from "@forma/types/responses";
import type { TTag } from "@forma/types/tags";
import { describe, expect, test } from "vite-plus/test";
import { BULK_TAG_MAX_RESPONSE_IDS } from "../../api";
import {
  BULK_DELETE_CHUNK_SIZE,
  bulkTagCriteria,
  changeTagInChunks,
  deleteInChunks,
  tagsOfRows,
} from "./selected-row-settings";

describe("deleteInChunks", () => {
  test("deletes five at a time, a chunk only after the previous one finished", async () => {
    const ids = Array.from({ length: 12 }, (_, i) => `r${i}`);
    let inFlight = 0;
    let maxInFlight = 0;
    const deleted: string[] = [];
    await deleteInChunks(ids, async (id) => {
      inFlight++;
      maxInFlight = Math.max(maxInFlight, inFlight);
      await Promise.resolve();
      deleted.push(id);
      inFlight--;
    });
    expect(BULK_DELETE_CHUNK_SIZE).toBe(5);
    expect(maxInFlight).toBe(5);
    expect(deleted).toEqual(ids);
  });

  test("stops at the first failing chunk", async () => {
    const attempted: string[] = [];
    await expect(
      deleteInChunks(["a", "b", "c", "d", "e", "f"], async (id) => {
        attempted.push(id);
        if (id === "b") throw new Error("nope");
      })
    ).rejects.toThrow("nope");
    expect(attempted).toEqual(["a", "b", "c", "d", "e"]);
  });
});

describe("bulkTagCriteria", () => {
  test("sends the view's filter once for all matching responses", () => {
    const criteria = { finished: true, tags: { applied: ["vip"], notApplied: [] } };
    expect(bulkTagCriteria({ kind: "allMatching", criteria, count: 2400 })).toEqual([criteria]);
  });

  test("sends ticked rows as ids, split at the server's bound", () => {
    const ids = Array.from({ length: BULK_TAG_MAX_RESPONSE_IDS + 3 }, (_, i) => `r${i}`);
    const chunks = bulkTagCriteria({ kind: "rows", responseIds: ids });
    expect(chunks.map((c) => c.responseIds?.length)).toEqual([BULK_TAG_MAX_RESPONSE_IDS, 3]);
    expect(chunks.flatMap((c) => c.responseIds)).toEqual(ids);
  });
});

describe("changeTagInChunks", () => {
  test("adds up what each request changed, one request at a time", async () => {
    const ids = Array.from({ length: BULK_TAG_MAX_RESPONSE_IDS * 2 }, (_, i) => `r${i}`);
    let inFlight = 0;
    const changed = await changeTagInChunks({ kind: "rows", responseIds: ids }, async (criteria) => {
      inFlight++;
      expect(inFlight).toBe(1);
      await Promise.resolve();
      inFlight--;
      return { tagId: "t", matched: criteria.responseIds?.length ?? 0, changed: 7 };
    });
    expect(changed).toBe(14);
  });

  test("stops at the first failure", async () => {
    const ids = Array.from({ length: BULK_TAG_MAX_RESPONSE_IDS * 3 }, (_, i) => `r${i}`);
    let calls = 0;
    await expect(
      changeTagInChunks({ kind: "rows", responseIds: ids }, () => {
        calls++;
        return calls === 2
          ? Promise.reject(new Error("nope"))
          : Promise.resolve({ tagId: "t", matched: 1, changed: 1 });
      })
    ).rejects.toThrow("nope");
    expect(calls).toBe(2);
  });
});

describe("tagsOfRows", () => {
  const tag = (id: string, name: string): TTag => ({
    id,
    name,
    workspaceId: "w",
    createdAt: new Date(0),
    updatedAt: new Date(0),
  });

  test("lists each tag of the rows once, by name", () => {
    const vip = tag("1", "vip");
    const churn = tag("2", "churn");
    const rows = [{ tags: [vip] }, { tags: [churn, vip] }, { tags: [] }] as unknown as TResponseTableData[];
    expect(tagsOfRows(rows)).toEqual([churn, vip]);
  });
});
