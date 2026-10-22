import { describe, expect, test } from "vite-plus/test";
import { BULK_DELETE_CHUNK_SIZE, deleteInChunks } from "./selected-row-settings";

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
