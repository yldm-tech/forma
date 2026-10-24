import { expect, test } from "vite-plus/test";
import { formatDateForDisplay, timeSince } from "./time";

test("timeSince reads relative to now in the user's locale, English otherwise", () => {
  const now = new Date("2026-10-22T12:00:00Z");
  expect(timeSince("2026-10-19T12:00:00Z", "en-US", now)).toBe("3 days ago");
  expect(timeSince("2026-10-19T12:00:00Z", "xx-XX", now)).toBe("3 days ago");
  expect(timeSince("2026-10-19T12:00:00Z", "de-DE", now)).toBe("vor 3 Tagen");
});

test("dates display in the locale's short form", () => {
  expect(formatDateForDisplay("2026-10-22T12:00:00Z", "en-US")).toBe("Oct 22, 2026");
});
