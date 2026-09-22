import { describe, expect, test } from "vitest";
import { reorderRankedIds } from "./ranking";

describe("reorderRankedIds", () => {
  test("moves an id up one position", () => {
    const result = reorderRankedIds(["a", "b", "c"], "b", "up");
    expect(result).toEqual({ rankedIds: ["b", "a", "c"], index: 0, changed: true });
  });

  test("moves an id down one position", () => {
    const result = reorderRankedIds(["a", "b", "c"], "b", "down");
    expect(result).toEqual({ rankedIds: ["a", "c", "b"], index: 2, changed: true });
  });

  test("reports no change when moving the first id up", () => {
    const result = reorderRankedIds(["a", "b", "c"], "a", "up");
    expect(result.changed).toBe(false);
    expect(result.index).toBe(0);
    expect(result.rankedIds).toEqual(["a", "b", "c"]);
  });

  test("reports no change when moving the last id down", () => {
    const result = reorderRankedIds(["a", "b", "c"], "c", "down");
    expect(result.changed).toBe(false);
    expect(result.index).toBe(2);
    expect(result.rankedIds).toEqual(["a", "b", "c"]);
  });

  test("reports no change for an id that is not ranked", () => {
    const result = reorderRankedIds(["a", "b"], "z", "up");
    expect(result).toEqual({ rankedIds: ["a", "b"], index: -1, changed: false });
  });

  test("does not mutate the input", () => {
    const input = ["a", "b", "c"];
    reorderRankedIds(input, "c", "up");
    expect(input).toEqual(["a", "b", "c"]);
  });
});
