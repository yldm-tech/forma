import { describe, expect, test, vi } from "vitest";
import {
  convertDatesInObject,
  formatDate,
  getTodaysDateTimeFormatted,
  timeSince,
  timeSinceDate,
} from "./time";

describe("Time Utilities", () => {
  describe("timeSince", () => {
    test("should format time since in English", () => {
      const now = new Date();
      const oneHourAgo = new Date(now.getTime() - 60 * 60 * 1000);
      expect(timeSince(oneHourAgo.toISOString(), "en-US")).toBe("about 1 hour ago");
    });

    test("should format time since in German", () => {
      const now = new Date();
      const oneHourAgo = new Date(now.getTime() - 60 * 60 * 1000);
      expect(timeSince(oneHourAgo.toISOString(), "de-DE")).toBe("vor etwa 1 Stunde");
    });

    test("should format time since in Swedish", () => {
      const now = new Date();
      const oneHourAgo = new Date(now.getTime() - 60 * 60 * 1000);
      expect(timeSince(oneHourAgo.toISOString(), "sv-SE")).toBe("ungefär en timme sedan");
    });

    test("should format time since in Brazilian Portuguese", () => {
      const now = new Date();
      const oneHourAgo = new Date(now.getTime() - 60 * 60 * 1000);
      expect(timeSince(oneHourAgo.toISOString(), "pt-BR")).toBe("há cerca de 1 hora");
    });

    test("should format time since in European Portuguese", () => {
      const now = new Date();
      const oneHourAgo = new Date(now.getTime() - 60 * 60 * 1000);
      expect(timeSince(oneHourAgo.toISOString(), "pt-PT")).toBe("há aproximadamente 1 hora");
    });
  });

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

  describe("getTodaysDateTimeFormatted", () => {
    // Asserted against a fixed instant rather than recomputed the way the implementation computes it: the old expectation mirrored the mixed-zone bug line for line, so it passed on both the broken and the correct implementation.
    const instant = new Date("2026-09-19T23:30:45.000Z");

    test("formats the date and time with the given separator", () => {
      expect(getTodaysDateTimeFormatted(".", instant)).toBe("2026.09.19.23.30.45");
      expect(getTodaysDateTimeFormatted("-", instant)).toBe("2026-09-19-23-30-45");
    });

    test("reads both halves in UTC, so they describe one moment under a non-UTC server zone", () => {
      // 08:30 on the 20th in Tokyo is 23:30 on the 19th UTC. Taking the date from `toISOString()` and the clock from `toTimeString()` produced "2026.09.19.08.30.45" here — a calendar day behind the time printed beside it, naming a moment that never existed.
      // `stubEnv` rather than assigning `process.env.TZ`: it restores an originally-unset key by deleting it, where an assignment would write back the string "undefined" and pin every later test in this file to UTC.
      vi.stubEnv("TZ", "Asia/Tokyo");
      vi.useFakeTimers();
      vi.setSystemTime(instant);
      try {
        expect(getTodaysDateTimeFormatted(".")).toBe("2026.09.19.23.30.45");
      } finally {
        vi.useRealTimers();
        vi.unstubAllEnvs();
      }
    });

    test("defaults to now", () => {
      expect(getTodaysDateTimeFormatted("-")).toMatch(/^\d{4}-\d{2}-\d{2}-\d{2}-\d{2}-\d{2}$/);
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
      expect(result[0].createdAt).toBeInstanceOf(Date);
      expect(result[1].createdAt).toBeInstanceOf(Date);
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
