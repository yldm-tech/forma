import { describe, expect, test } from "vite-plus/test";
import { convertDatesInObject, formatDate, timeSinceDate } from "./time";

describe("Time Utilities", () => {
  describe("timeSinceDate", () => {
    test("should format time since from Date object", () => {
      const now = new Date();
      const oneHourAgo = new Date(now.getTime() - 60 * 60 * 1000);
      expect(timeSinceDate(oneHourAgo)).toBe("about 1 hour ago");
    });

    test("should format time since from Date object in the provided locale", () => {
      const now = new Date();
      const oneHourAgo = new Date(now.getTime() - 60 * 60 * 1000);
      expect(timeSinceDate(oneHourAgo, "de-DE")).toBe("vor etwa 1 Stunde");
    });
  });

  describe("formatDate", () => {
    test("should format date correctly", () => {
      const date = new Date(2024, 2, 20); // March is month 2 (0-based)
      expect(formatDate(date)).toBe("March 20, 2024");
    });

    test("should format date with the provided locale", () => {
      const date = new Date(2024, 2, 20);

      expect(formatDate(date, "de-DE")).toBe(
        new Intl.DateTimeFormat("de-DE", {
          year: "numeric",
          month: "long",
          day: "numeric",
        }).format(date)
      );
    });
  });

  describe("convertDatesInObject", () => {
    test("should convert date strings to Date objects in an object", () => {
      const input = {
        id: 1,
        createdAt: "2024-03-20T15:30:00",
        updatedAt: "2024-03-20T16:30:00",
        nested: {
          createdAt: "2024-03-20T17:30:00",
        },
      };

      const result = convertDatesInObject(input);
      expect(result.createdAt).toBeInstanceOf(Date);
      expect(result.updatedAt).toBeInstanceOf(Date);
      expect(result.nested.createdAt).toBeInstanceOf(Date);
      expect(result.id).toBe(1);
    });

    test("should handle arrays", () => {
      const input = [{ createdAt: "2024-03-20T15:30:00" }, { createdAt: "2024-03-20T16:30:00" }];

      const result = convertDatesInObject(input);
      expect(result[0]!.createdAt).toBeInstanceOf(Date);
      expect(result[1]!.createdAt).toBeInstanceOf(Date);
    });

    test("should return non-objects as is", () => {
      expect(convertDatesInObject(null)).toBe(null);
      expect(convertDatesInObject("string")).toBe("string");
      expect(convertDatesInObject(123)).toBe(123);
    });

    test("should not convert dates in ignored keys when keysToIgnore is provided", () => {
      const keysToIgnore = new Set(["contactAttributes", "variables", "data", "meta"]);
      const input = {
        createdAt: "2024-03-20T15:30:00",
        contactAttributes: {
          createdAt: "2024-03-20T16:30:00",
          email: "test@example.com",
        },
      };

      const result = convertDatesInObject(input, keysToIgnore);
      expect(result.createdAt).toBeInstanceOf(Date);
      expect(result.contactAttributes.createdAt).toBe("2024-03-20T16:30:00");
      expect(result.contactAttributes.email).toBe("test@example.com");
    });

    test("should not convert dates in variables when keysToIgnore is provided", () => {
      const keysToIgnore = new Set(["contactAttributes", "variables", "data", "meta"]);
      const input = {
        updatedAt: "2024-03-20T15:30:00",
        variables: {
          createdAt: "2024-03-20T16:30:00",
          userId: "123",
        },
      };

      const result = convertDatesInObject(input, keysToIgnore);
      expect(result.updatedAt).toBeInstanceOf(Date);
      expect(result.variables.createdAt).toBe("2024-03-20T16:30:00");
      expect(result.variables.userId).toBe("123");
    });

    test("should not convert dates in data or meta when keysToIgnore is provided", () => {
      const keysToIgnore = new Set(["contactAttributes", "variables", "data", "meta"]);
      const input = {
        createdAt: "2024-03-20T15:30:00",
        data: {
          createdAt: "2024-03-20T16:30:00",
        },
        meta: {
          updatedAt: "2024-03-20T17:30:00",
        },
      };

      const result = convertDatesInObject(input, keysToIgnore);
      expect(result.createdAt).toBeInstanceOf(Date);
      expect(result.data.createdAt).toBe("2024-03-20T16:30:00");
      expect(result.meta.updatedAt).toBe("2024-03-20T17:30:00");
    });

    test("should recurse into all keys when keysToIgnore is not provided", () => {
      const input = {
        createdAt: "2024-03-20T15:30:00",
        contactAttributes: {
          createdAt: "2024-03-20T16:30:00",
        },
      };

      const result = convertDatesInObject(input);
      expect(result.createdAt).toBeInstanceOf(Date);
      expect(result.contactAttributes.createdAt).toBeInstanceOf(Date);
    });
  });
});
