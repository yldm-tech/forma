import { expect, test } from "vite-plus/test";
import { formatDateForDisplay, timeSince } from "./time";

const now = new Date("2026-10-22T12:00:00.000Z");

test("timeSince words the distance in the dashboard language, in English for any other", () => {
  expect(timeSince("2026-10-22T11:55:00.000Z", "en-US", now)).toBe("5 minutes ago");
  expect(timeSince("2026-10-22T11:55:00.000Z", "ja-JP", now)).toBe("5分前");
  expect(timeSince("2026-10-22T11:55:00.000Z", "xx-XX", now)).toBe("5 minutes ago");
});

test("formatDateForDisplay prints a short calendar date", () => {
  expect(formatDateForDisplay("2026-10-22T12:00:00.000Z", "en-US")).toBe("Oct 22, 2026");
});
