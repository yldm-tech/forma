import { describe, expect, test } from "vitest";
import { getSampleTruncation } from "./sample-truncation";

describe("getSampleTruncation", () => {
  test("returns the shown/total pair when the sample list was capped", () => {
    expect(getSampleTruncation(50, 312)).toEqual({ shown: 50, total: 312 });
  });

  test("returns null when every answer made it into the sample list", () => {
    expect(getSampleTruncation(12, 12)).toBeNull();
  });

  test("returns null for an empty sample list so the empty state stays the only message", () => {
    expect(getSampleTruncation(0, 0)).toBeNull();
    expect(getSampleTruncation(0, 7)).toBeNull();
  });

  test("returns null when the sample list is longer than the reported count", () => {
    // Defensive: a count that lags behind the samples must not render a "50 / 10" line.
    expect(getSampleTruncation(50, 10)).toBeNull();
  });
});
