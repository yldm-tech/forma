import { describe, expect, test } from "vite-plus/test";
import { isSameDay } from "./date-utils";

describe("date-utils", () => {
  describe("isSameDay", () => {
    test("returns true for dates on the same day", () => {
      const date1 = new Date("2024-01-15T10:00:00Z");
      const date2 = new Date("2024-01-15T22:00:00Z");
      expect(isSameDay(date1, date2)).toBe(true);
    });

    test("returns false for dates on different days", () => {
      const date1 = new Date("2024-01-15T23:59:59Z");
      const date2 = new Date("2024-01-16T00:00:01Z");
      expect(isSameDay(date1, date2)).toBe(false);
    });

    test("returns false for dates in different months", () => {
      const date1 = new Date("2024-01-31T12:00:00Z");
      const date2 = new Date("2024-02-01T12:00:00Z");
      expect(isSameDay(date1, date2)).toBe(false);
    });

    test("returns false for dates in different years", () => {
      const date1 = new Date("2023-12-31T12:00:00Z");
      const date2 = new Date("2024-01-01T12:00:00Z");
      expect(isSameDay(date1, date2)).toBe(false);
    });
  });
});
