import { describe, expect, test } from "vite-plus/test";
import { SURVEY_SCHEDULING_CONFIG } from "./constants";
import {
  describeScheduledMoment,
  getSurveySchedulingTimeLabel,
  resolveSurveySchedulingConfig,
} from "./scheduling-config";
import { createSurveySchedulingDateUtils } from "./scheduling-date-utils";

describe("resolveSurveySchedulingConfig", () => {
  test("uses the schedule the server sends with the workspace", () => {
    const fromServer = { timeZone: "Asia/Tokyo", localHour: 9, localMinute: 30, instanceTimeZone: "Europe/Berlin" };
    const config = resolveSurveySchedulingConfig(fromServer);
    expect(config).toBe(fromServer);
    expect(getSurveySchedulingTimeLabel(config)).toBe("09:30");
  });

  test("falls back to the built-in default only without one", () => {
    expect(resolveSurveySchedulingConfig(undefined)).toBe(SURVEY_SCHEDULING_CONFIG);
  });
});

describe("describeScheduledMoment", () => {
  // Midnight of 2026-10-30 in Tokyo.
  const instant = new Date("2026-10-29T15:00:00.000Z");
  // ICU separates the time from AM/PM with a narrow no-break space.
  const plain = (label: { moment: string; timeZone: string } | null) =>
    label && { ...label, moment: label.moment.replace(/\s/g, " ") };

  test("a day picked in the editor stands for midnight in the organization's zone", () => {
    const tokyo = { timeZone: "Asia/Tokyo", localHour: 0, localMinute: 0 };
    const snapped = createSurveySchedulingDateUtils(tokyo).normalizeDateOnlySelectionToSurveySchedulingDateTime(
      new Date("2026-10-30T12:00:00.000Z")
    );
    expect(snapped?.toISOString()).toBe(instant.toISOString());
  });

  test("adds the browser's clock when it reads differently", () => {
    const { scheduled, browser } = describeScheduledMoment(instant, "Asia/Tokyo", "Europe/Berlin", "en-US");
    expect(plain(scheduled)).toEqual({ moment: "Oct 30, 2026, 12:00 AM", timeZone: "Asia/Tokyo" });
    expect(plain(browser)).toEqual({ moment: "Oct 29, 2026, 4:00 PM", timeZone: "Europe/Berlin" });
  });

  test("formats in the user's language", () => {
    const { scheduled, browser } = describeScheduledMoment(instant, "Asia/Tokyo", "Europe/Berlin", "ja-JP");
    expect(scheduled.moment).toBe("2026/10/30 0:00");
    expect(browser?.moment).toBe("2026/10/29 16:00");
  });

  test("leaves the browser's clock out when it is the same zone or reads the same", () => {
    expect(describeScheduledMoment(instant, "Asia/Tokyo", "Asia/Tokyo", "en-US").browser).toBeNull();
    expect(describeScheduledMoment(instant, "Asia/Tokyo", "Asia/Seoul", "en-US").browser).toBeNull();
  });
});
