import { describe, expect, test } from "vite-plus/test";
import { SURVEY_SCHEDULING_CONFIG } from "./constants";
import { createSurveySchedulingDateUtils, toDateOnlySelection } from "./scheduling-date-utils";

const defaultConfig = {
  timeZone: "Europe/Berlin",
  localHour: 0,
  localMinute: 0,
};

const {
  toCalendarDate,
  getMinimumSurveySchedulingCalendarDate,
  normalizeDateOnlySelectionToSurveySchedulingDateTime,
} = createSurveySchedulingDateUtils(defaultConfig);

describe("survey scheduling date utils", () => {
  test("stores selected dates as noon UTC date-only values", () => {
    const selectedDate = new Date("2026-04-17T09:15:00.000Z");
    const storedDate = toDateOnlySelection(selectedDate);

    expect(storedDate.toISOString()).toBe("2026-04-17T12:00:00.000Z");
  });

  test("round-trips stored date-only values back to the selected calendar day", () => {
    const storedDate = new Date("2026-04-17T12:00:00.000Z");
    const calendarDate = toCalendarDate(storedDate);

    expect(calendarDate.getFullYear()).toBe(2026);
    expect(calendarDate.getMonth()).toBe(3);
    expect(calendarDate.getDate()).toBe(17);
    expect(calendarDate.getHours()).toBe(12);
  });

  test("normalizes selected dates to the configured survey scheduling time", () => {
    const storedDate = new Date("2026-01-17T12:00:00.000Z");

    expect(normalizeDateOnlySelectionToSurveySchedulingDateTime(storedDate)?.toISOString()).toBe(
      "2026-01-16T23:00:00.000Z"
    );
  });

  test("keeps already-normalized survey scheduling values stable when re-saved", () => {
    const normalizedStoredDate = new Date("2026-04-16T22:00:00.000Z");
    const calendarDate = toCalendarDate(normalizedStoredDate);
    const reselectedDate = toDateOnlySelection(calendarDate);

    expect(reselectedDate.toISOString()).toBe("2026-04-17T12:00:00.000Z");
    expect(normalizeDateOnlySelectionToSurveySchedulingDateTime(normalizedStoredDate)?.toISOString()).toBe(
      "2026-04-16T22:00:00.000Z"
    );
    expect(normalizeDateOnlySelectionToSurveySchedulingDateTime(reselectedDate)?.toISOString()).toBe(
      "2026-04-16T22:00:00.000Z"
    );
  });

  test("uses the timezone's DST offset when normalizing summer dates", () => {
    const storedDate = new Date("2026-07-10T12:00:00.000Z");

    expect(normalizeDateOnlySelectionToSurveySchedulingDateTime(storedDate)?.toISOString()).toBe(
      "2026-07-09T22:00:00.000Z"
    );
  });

  test("uses the next schedulable calendar day after today's run time passes", () => {
    const minSelectableDate = getMinimumSurveySchedulingCalendarDate(new Date("2026-04-16T21:30:00.000Z"));

    expect(minSelectableDate.getFullYear()).toBe(2026);
    expect(minSelectableDate.getMonth()).toBe(3);
    expect(minSelectableDate.getDate()).toBe(17);
    expect(minSelectableDate.getHours()).toBe(12);
  });

  test("honors a runtime-configured timezone, hour, and minute", () => {
    const { normalizeDateOnlySelectionToSurveySchedulingDateTime: normalize } =
      createSurveySchedulingDateUtils({
        timeZone: "America/New_York",
        localHour: 18,
        localMinute: 45,
      });
    const storedDate = new Date("2026-01-17T12:00:00.000Z");

    // 2026-01-17 18:45 in America/New_York (EST, UTC-5) is 23:45 UTC.
    expect(normalize(storedDate)?.toISOString()).toBe("2026-01-17T23:45:00.000Z");
  });

  // At UTC+12 and beyond, noon UTC is already the next day, so a picked day has to be sent as that day's run time in the zone itself.
  test.each([
    ["Pacific/Auckland", "2026-04-30T12:00:00.000Z"],
    ["Pacific/Chatham", "2026-04-30T11:15:00.000Z"],
    ["Pacific/Kiritimati", "2026-04-30T10:00:00.000Z"],
    ["America/Los_Angeles", "2026-05-01T07:00:00.000Z"],
  ])("keeps the picked day in %s", (timeZone, expected) => {
    const utils = createSurveySchedulingDateUtils({ timeZone, localHour: 0, localMinute: 0 });
    // The calendar hands back the picked day as a browser-local date.
    const picked = new Date(2026, 4, 1, 12, 0, 0, 0);
    const sent = utils.toSurveySchedulingDateTime(picked);

    expect(sent.toISOString()).toBe(expected);
    // The calendar shows the day that was picked, and the server's snap leaves the instant where it is.
    const shown = utils.toCalendarDate(sent);
    expect([shown.getFullYear(), shown.getMonth(), shown.getDate()]).toEqual([2026, 4, 1]);
    expect(utils.normalizeDateOnlySelectionToSurveySchedulingDateTime(sent)?.toISOString()).toBe(expected);
  });

  test("documents the default scheduling configuration", () => {
    expect(SURVEY_SCHEDULING_CONFIG.timeZone).toBe("Europe/Berlin");
    expect(SURVEY_SCHEDULING_CONFIG.localHour).toBe(0);
    expect(SURVEY_SCHEDULING_CONFIG.localMinute).toBe(0);
  });
});
