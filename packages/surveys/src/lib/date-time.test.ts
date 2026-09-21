import { describe, expect, test } from "vitest";
import { formatDateWithOrdinal, isValidDateString, parseDateOnly } from "./date-time";

describe("isValidDateString", () => {
  test("should return true for valid YYYY-MM-DD format", () => {
    expect(isValidDateString("2023-01-15")).toBe(true);
    expect(isValidDateString("2024-02-29")).toBe(true); // Leap year
  });

  test("should return true for valid DD-MM-YYYY format", () => {
    expect(isValidDateString("15-01-2023")).toBe(true);
    expect(isValidDateString("29-02-2024")).toBe(true);
  });

  test("should return false for invalid dates in valid format", () => {
    // Was asserted as `true`, against this test's own name: the day was never range-checked, so
    // February 30th rolled over into March and recall rendered a day the respondent never picked.
    expect(isValidDateString("2023-02-30")).toBe(false);
    expect(isValidDateString("2023-13-01")).toBe(false);
    expect(isValidDateString("32-01-2023")).toBe(false);
    expect(isValidDateString("01-13-2023")).toBe(false);
  });
});

describe("parseDateOnly", () => {
  test("names the same calendar day in every time zone", () => {
    // `new Date("2026-09-21")` is UTC midnight, which Intl then renders in the viewer's zone -
    // the previous day for everyone west of UTC.
    const parsed = parseDateOnly("2026-09-21");

    expect(parsed).not.toBeNull();
    expect(parsed?.getFullYear()).toBe(2026);
    expect(parsed?.getMonth()).toBe(8);
    expect(parsed?.getDate()).toBe(21);
  });

  test("reads the day-first form the date element also stores", () => {
    // `new Date("21-09-2026")` is not a format the parser accepts, so this used to reach
    // Intl.DateTimeFormat as an Invalid Date and throw a RangeError, taking the card down.
    const parsed = parseDateOnly("21-09-2026");

    expect(parsed?.getFullYear()).toBe(2026);
    expect(parsed?.getMonth()).toBe(8);
    expect(parsed?.getDate()).toBe(21);
  });

  test("formats without throwing for either stored form", () => {
    for (const value of ["2026-09-21", "21-09-2026"]) {
      const parsed = parseDateOnly(value);
      expect(parsed).not.toBeNull();
      expect(() => formatDateWithOrdinal(parsed as Date, "en-US")).not.toThrow();
    }
    expect(formatDateWithOrdinal(parseDateOnly("21-09-2026") as Date, "en-US")).toBe(
      "Monday, September 21, 2026"
    );
  });

  test("returns null for a day that does not exist", () => {
    expect(parseDateOnly("2023-02-30")).toBeNull();
    expect(parseDateOnly("31-04-2023")).toBeNull();
  });

  test("returns null for anything that is not a date-only value", () => {
    expect(parseDateOnly("2023/01/15")).toBeNull();
    expect(parseDateOnly("")).toBeNull();
    expect(parseDateOnly("not a date")).toBeNull();
  });

  test("should return false for invalid formats", () => {
    expect(isValidDateString("2023/01/15")).toBe(false);
    expect(isValidDateString("01/15/2023")).toBe(false);
    expect(isValidDateString("Jan 15, 2023")).toBe(false);
    expect(isValidDateString("20230115")).toBe(false);
    expect(isValidDateString("")).toBe(false);
    expect(isValidDateString("not a date")).toBe(false);
  });
});

describe("formatDateWithOrdinal", () => {
  const getExpectedLocaleDate = (date: Date, locale: string) =>
    new Intl.DateTimeFormat(locale, {
      weekday: "long",
      year: "numeric",
      month: "long",
      day: "numeric",
    }).format(date);

  test("formats a known en-US date with the expected output", () => {
    expect(formatDateWithOrdinal(new Date(2024, 0, 1), "en-US")).toBe("Monday, January 1, 2024");
  });

  test("formats survey dates with locale-native en-US output", () => {
    const date = new Date(2024, 0, 1);

    expect(formatDateWithOrdinal(date, "en-US")).toBe(getExpectedLocaleDate(date, "en-US"));
  });

  test("formats survey dates with locale-native fr-FR output", () => {
    const date = new Date(2024, 0, 1);

    expect(formatDateWithOrdinal(date, "fr-FR")).toBe(getExpectedLocaleDate(date, "fr-FR"));
  });

  test("formats survey dates with locale-native de-DE output", () => {
    const date = new Date(2024, 2, 20);

    expect(formatDateWithOrdinal(date, "de-DE")).toBe(getExpectedLocaleDate(date, "de-DE"));
  });
});
