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

  test("places a picked day and time in the scheduling zone and reads the time back", () => {
    const utils = createSurveySchedulingDateUtils({ timeZone: "Asia/Tokyo", localHour: 0, localMinute: 0 });
    const instant = utils.toSurveySchedulingDateTimeAt(new Date(2026, 4, 1, 12), 18, 15);

    expect(instant.toISOString()).toBe("2026-05-01T09:15:00.000Z");
    expect(utils.getSurveySchedulingTimeOfDay(instant)).toEqual({ hour: 18, minute: 15 });
  });

  test("offers the scheduler's quarter-hour runs of a day after an instant", () => {
    const { getSurveySchedulingTimeOptions } = createSurveySchedulingDateUtils(defaultConfig);
    const day = new Date(2026, 6, 10, 12);
    const all = getSurveySchedulingTimeOptions(day, new Date("2026-01-01T00:00:00.000Z"));

    expect(all).toHaveLength(96);
    expect(all[0]?.instant.toISOString()).toBe("2026-07-09T22:00:00.000Z");
    // 18:07 in Berlin (CEST): the first time left is 18:15.
    const later = getSurveySchedulingTimeOptions(day, new Date("2026-07-10T16:07:00.000Z"));
    expect(later[0]).toMatchObject({ hour: 18, minute: 15 });
    expect(later[0]?.instant.toISOString()).toBe("2026-07-10T16:15:00.000Z");
    expect(later).toHaveLength(23);
  });

  test("leaves out the times a daylight saving change skips", () => {
    const { getSurveySchedulingTimeOptions } = createSurveySchedulingDateUtils(defaultConfig);
    // Berlin skips 02:00 to 02:59 on March 29, 2026.
    const options = getSurveySchedulingTimeOptions(
      new Date(2026, 2, 29, 12),
      new Date("2026-01-01T00:00:00.000Z")
    );

    expect(options).toHaveLength(92);
    expect(options.some((option) => option.hour === 2)).toBe(false);
  });

  test("keeps the runs on the configured minute's grid", () => {
    const { getSurveySchedulingTimeOptions } = createSurveySchedulingDateUtils({
      timeZone: "UTC",
      localHour: 6,
      localMinute: 20,
    });
    const minutes = new Set(
      getSurveySchedulingTimeOptions(new Date(2026, 4, 1, 12), new Date("2026-01-01T00:00:00.000Z")).map(
        (option) => option.minute
      )
    );

    expect([...minutes].sort((a, b) => a - b)).toEqual([5, 20, 35, 50]);
  });

  test("starts the calendar on the first day with a run left", () => {
    const { getFirstSurveySchedulingCalendarDate } = createSurveySchedulingDateUtils(defaultConfig);
    // 23:40 in Berlin leaves 23:45 today; 23:50 leaves nothing.
    const today = getFirstSurveySchedulingCalendarDate(new Date("2026-07-10T21:40:00.000Z"));
    const tomorrow = getFirstSurveySchedulingCalendarDate(new Date("2026-07-10T21:50:00.000Z"));

    expect([today.getMonth(), today.getDate()]).toEqual([6, 10]);
    expect([tomorrow.getMonth(), tomorrow.getDate()]).toEqual([6, 11]);
  });

  test("documents the default scheduling configuration", () => {
    expect(SURVEY_SCHEDULING_CONFIG.timeZone).toBe("Europe/Berlin");
    expect(SURVEY_SCHEDULING_CONFIG.localHour).toBe(0);
    expect(SURVEY_SCHEDULING_CONFIG.localMinute).toBe(0);
  });
});
