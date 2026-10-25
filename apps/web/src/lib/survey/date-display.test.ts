import { describe, expect, test } from "vite-plus/test";
import { formatStoredDateForDisplay, parseStoredDateTimeValue, parseStoredDateValue } from "./date-display";

describe("date display utils", () => {
  test("parses ISO stored dates", () => {
    const parsedDate = parseStoredDateValue("2025-05-06");

    expect(parsedDate).not.toBeNull();
    expect(parsedDate?.getFullYear()).toBe(2025);
    expect(parsedDate?.getMonth()).toBe(4);
    expect(parsedDate?.getDate()).toBe(6);
  });

  test("parses legacy stored dates using the element format", () => {
    const parsedDate = parseStoredDateValue("5-6-2025", "M-d-y");

    expect(parsedDate).not.toBeNull();
    expect(parsedDate?.getFullYear()).toBe(2025);
    expect(parsedDate?.getMonth()).toBe(4);
    expect(parsedDate?.getDate()).toBe(6);
  });

  test("parses day-first stored dates when no format is provided", () => {
    const parsedDate = parseStoredDateValue("06-05-2025");

    expect(parsedDate).not.toBeNull();
    expect(parsedDate?.getFullYear()).toBe(2025);
    expect(parsedDate?.getMonth()).toBe(4);
    expect(parsedDate?.getDate()).toBe(6);
  });

  test("formats stored dates using the selected locale", () => {
    const date = new Date(2025, 4, 6);

    expect(formatStoredDateForDisplay("2025-05-06", undefined, "de-DE")).toBe(
      new Intl.DateTimeFormat("de-DE", {
        weekday: "long",
        year: "numeric",
        month: "long",
        day: "numeric",
      }).format(date)
    );
  });

  test("returns null for invalid stored dates", () => {
    expect(formatStoredDateForDisplay("2025-02-30", "y-M-d")).toBeNull();
  });

  test("formats a date and time answer with its time of day, in local time", () => {
    const date = new Date(2025, 4, 6, 14, 30);

    expect(parseStoredDateTimeValue("2025-05-06T14:30")?.getTime()).toBe(date.getTime());
    expect(formatStoredDateForDisplay("2025-05-06T14:30", "y-M-d", "en-US")).toBe(
      new Intl.DateTimeFormat("en-US", {
        weekday: "long",
        year: "numeric",
        month: "long",
        day: "numeric",
        hour: "numeric",
        minute: "2-digit",
      }).format(date)
    );
  });

  test("refuses a date and time answer that names no real time", () => {
    expect(parseStoredDateTimeValue("2025-05-06T24:10")).toBeNull();
    expect(parseStoredDateTimeValue("2025-02-30T10:00")).toBeNull();
  });
});
