import { describe, expect, test } from "vite-plus/test";
import {
  formatDateForDisplay,
  formatDateTimeForDisplay,
  formatDateWithOrdinal,
  formatLocalDay,
  parseLocalDay,
  parseStoredDay,
} from "./datetime";

describe("datetime utils", () => {
  test("formatDateWithOrdinal formats a date using the provided locale", () => {
    // Create a date that's fixed to May 6, 2025 at noon UTC
    // Using noon ensures the date won't change in most timezones
    const date = new Date(Date.UTC(2025, 4, 6, 12, 0, 0));

    expect(formatDateWithOrdinal(date)).toBe(
      new Intl.DateTimeFormat("en-US", {
        weekday: "long",
        year: "numeric",
        month: "long",
        day: "numeric",
      }).format(date)
    );
  });

  test("formatDateForDisplay uses the provided locale", () => {
    const date = new Date(Date.UTC(2025, 4, 6, 12, 0, 0));

    expect(formatDateForDisplay(date, "de-DE")).toBe(
      new Intl.DateTimeFormat("de-DE", {
        year: "numeric",
        month: "short",
        day: "numeric",
      }).format(date)
    );
  });

  test("formatDateTimeForDisplay uses the provided locale", () => {
    const date = new Date(Date.UTC(2025, 4, 6, 12, 30, 0));

    expect(formatDateTimeForDisplay(date, "fr-FR")).toBe(
      new Intl.DateTimeFormat("fr-FR", {
        year: "numeric",
        month: "long",
        day: "numeric",
        hour: "numeric",
        minute: "2-digit",
      }).format(date)
    );
  });
});

describe("formatLocalDay / parseLocalDay", () => {
  test("serialises the local calendar day, zero-padded", () => {
    // Late in the day on purpose: a UTC-based serialiser would roll this to the 6th east of UTC.
    expect(formatLocalDay(new Date(2026, 7, 5, 23, 30))).toBe("2026-08-05");
    expect(formatLocalDay(new Date(2026, 0, 1, 0, 0))).toBe("2026-01-01");
    expect(formatLocalDay(new Date(2026, 11, 31, 12, 0))).toBe("2026-12-31");
  });

  test("round-trips through parseLocalDay to local midnight", () => {
    const parsed = parseLocalDay("2026-08-05");

    expect([parsed.getFullYear(), parsed.getMonth(), parsed.getDate()]).toEqual([2026, 7, 5]);
    expect([parsed.getHours(), parsed.getMinutes()]).toEqual([0, 0]);
    expect(formatLocalDay(parsed)).toBe("2026-08-05");
  });

  test.each(["2026-01-01", "2026-03-08", "2026-08-05", "2026-11-01", "2026-12-31"])(
    "is its own inverse for %s",
    (day) => {
      expect(formatLocalDay(parseLocalDay(day))).toBe(day);
    }
  );
});

describe("parseStoredDay", () => {
  test("reads the calendar day out of a stored ISO timestamp without shifting it", () => {
    // Segment and contact-attribute values are stored at midnight UTC. Parsing the whole string as an
    // instant would land on the 4th for viewers west of UTC; the day named in the string must win.
    const parsed = parseStoredDay("2026-08-05T00:00:00.000Z");

    expect(parsed && formatLocalDay(parsed)).toBe("2026-08-05");
    expect(parsed && [parsed.getHours(), parsed.getMinutes()]).toEqual([0, 0]);
  });

  test("parses a bare yyyy-MM-dd day, as validation rules store it", () => {
    const parsed = parseStoredDay("2026-01-31");

    expect(parsed && [parsed.getFullYear(), parsed.getMonth(), parsed.getDate()]).toEqual([2026, 0, 31]);
  });

  test.each(["2026-08-05T25:99:99Z", "2026-08-05Tnot-a-time", "2026-08-05T"])(
    "keeps the day when only the time part is malformed: %s",
    (value) => {
      // Deliberate: the day is what the picker shows, and the `<input type="date">` this replaced
      // split on "T" the same way. Blanking a readable day over a broken suffix would lose data.
      const parsed = parseStoredDay(value);

      expect(parsed && formatLocalDay(parsed)).toBe("2026-08-05");
    }
  );

  test("returns null when there is no stored value", () => {
    expect(parseStoredDay("")).toBeNull();
    expect(parseStoredDay(null)).toBeNull();
    expect(parseStoredDay(undefined)).toBeNull();
  });

  test.each([
    ["not-a-date", "unparseable text"],
    ["2026-13-45", "an out-of-range day that Date would roll over to 2027-02-14"],
    ["05/08/2026", "a non-ISO order"],
    ["2026-9-7", 'an unpadded day, which an <input type="date"> also rejected'],
  ])("returns null for %s (%s)", (value) => {
    expect(parseStoredDay(value)).toBeNull();
  });
});
