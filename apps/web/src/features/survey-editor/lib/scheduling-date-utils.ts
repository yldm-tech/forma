import type { TSurveySchedulingConfig } from "./scheduling-config";

const DATE_ONLY_SELECTION_UTC_HOUR = 12;
const LOCAL_CALENDAR_NOON_HOUR = 12;

/** Minutes between two runs of the server's scheduler (apps/server/internal/survey/scheduling, Runs): the publish and close times the editor offers. */
export const SURVEY_SCHEDULING_STEP_MINUTES = 15;

/** A time of day a scheduled survey can open or close at on a given day, in the scheduling zone, and the instant it is. */
export interface TSurveySchedulingTimeOption {
  hour: number;
  minute: number;
  instant: Date;
}

interface TimeZoneDateParts {
  day: number;
  hour: number;
  minute: number;
  month: number;
  year: number;
}

const createLocalCalendarDate = ({
  day,
  month,
  year,
}: Pick<TimeZoneDateParts, "day" | "month" | "year">): Date =>
  new Date(year, month - 1, day, LOCAL_CALENDAR_NOON_HOUR, 0, 0, 0);

// Config-independent helper. `toDateOnlySelection` stores the picked day as noon UTC, so it does not depend on the scheduling time zone.
export const toDateOnlySelection = (date: Date): Date =>
  new Date(
    Date.UTC(date.getFullYear(), date.getMonth(), date.getDate(), DATE_ONLY_SELECTION_UTC_HOUR, 0, 0, 0)
  );

/**
 * Builds the timezone-aware scheduling date helpers for a given configuration. The config is
 * sourced from server-only env vars and passed in explicitly so the same logic can run on the
 * server (reconciliation) and on the client (survey editor) with runtime-configured values.
 */
export const createSurveySchedulingDateUtils = (config: TSurveySchedulingConfig) => {
  const timeZoneFormatter = new Intl.DateTimeFormat("en-CA", {
    day: "2-digit",
    hour: "2-digit",
    hourCycle: "h23",
    minute: "2-digit",
    month: "2-digit",
    second: "2-digit",
    timeZone: config.timeZone,
    year: "numeric",
  });

  const timeZoneOffsetFormatter = new Intl.DateTimeFormat("en-US", {
    hour: "2-digit",
    hourCycle: "h23",
    minute: "2-digit",
    timeZone: config.timeZone,
    timeZoneName: "shortOffset",
  });

  const getTimeZoneDateParts = (date: Date): TimeZoneDateParts => {
    const parts = timeZoneFormatter.formatToParts(date);
    const getPartValue = (type: Intl.DateTimeFormatPartTypes): number => {
      const value = parts.find((part) => part.type === type)?.value;

      if (!value) {
        throw new Error(`Missing ${type} in survey scheduling date formatter output`);
      }

      return Number.parseInt(value, 10);
    };

    return {
      day: getPartValue("day"),
      hour: getPartValue("hour"),
      minute: getPartValue("minute"),
      month: getPartValue("month"),
      year: getPartValue("year"),
    };
  };

  const getTimeZoneOffsetMs = (date: Date): number => {
    const offsetValue = timeZoneOffsetFormatter
      .formatToParts(date)
      .find((part) => part.type === "timeZoneName")?.value;

    if (!offsetValue || offsetValue === "GMT") {
      return 0;
    }

    const match = /^GMT(?<sign>[+-])(?<hours>\d{1,2})(?::(?<minutes>\d{2}))?$/.exec(offsetValue);

    if (!match?.groups) {
      throw new Error(`Unsupported survey scheduling timezone offset: ${offsetValue}`);
    }

    const sign = match.groups.sign === "+" ? 1 : -1;
    const hours = Number.parseInt(match.groups.hours ?? "0", 10);
    const minutes = Number.parseInt(match.groups.minutes ?? "0", 10);

    return sign * ((hours * 60 + minutes) * 60 * 1000);
  };

  const matchesSurveySchedulingLocalTime = (
    date: Date,
    year: number,
    month: number,
    day: number,
    hour: number,
    minute: number
  ): boolean => {
    const parts = getTimeZoneDateParts(date);

    return (
      parts.year === year &&
      parts.month === month + 1 &&
      parts.day === day &&
      parts.hour === hour &&
      parts.minute === minute
    );
  };

  /** The instant the scheduling zone's clock reads hour:minute (the schedule's own time by default) on a day; a time a daylight saving change skips comes out at the nearest instant there is. */
  const createSurveySchedulingDateTime = (
    year: number,
    month: number,
    day: number,
    hour: number = config.localHour,
    minute: number = config.localMinute
  ): Date => {
    const utcGuessMs = Date.UTC(year, month, day, hour, minute, 0, 0);
    let candidate = new Date(utcGuessMs);

    for (let attempt = 0; attempt < 3; attempt++) {
      candidate = new Date(utcGuessMs - getTimeZoneOffsetMs(candidate));

      if (matchesSurveySchedulingLocalTime(candidate, year, month, day, hour, minute)) {
        return candidate;
      }
    }

    return candidate;
  };

  /** The scheduler runs every quarter hour at the configured minute, the same instants in every zone (every UTC offset in use is whole quarter hours); a publish or close time is one of them, so the survey opens or closes when it says. */
  const gridOffsetMinutes = config.localMinute % SURVEY_SCHEDULING_STEP_MINUTES;
  const stepMs = SURVEY_SCHEDULING_STEP_MINUTES * 60 * 1000;

  /** The first run strictly after an instant. */
  const nextSurveySchedulingRun = (after: Date): Date => {
    const offsetMs = gridOffsetMinutes * 60 * 1000;
    return new Date(Math.floor((after.getTime() - offsetMs) / stepMs) * stepMs + offsetMs + stepMs);
  };

  /** The time of day an instant has in the scheduling zone. */
  const getSurveySchedulingTimeOfDay = (instant: Date): { hour: number; minute: number } => {
    const { hour, minute } = getTimeZoneDateParts(instant);
    return { hour, minute };
  };

  /** A day picked in the calendar (a browser-local date) at hour:minute in the scheduling zone. */
  const toSurveySchedulingDateTimeAt = (calendarDate: Date, hour: number, minute: number): Date =>
    createSurveySchedulingDateTime(
      calendarDate.getFullYear(),
      calendarDate.getMonth(),
      calendarDate.getDate(),
      hour,
      minute
    );

  /** The runs on a picked day that come after an instant (now, or the publish date for the close date), as the times the editor offers; a time the day does not have (a daylight saving gap) is left out. */
  const getSurveySchedulingTimeOptions = (calendarDate: Date, after: Date): TSurveySchedulingTimeOption[] => {
    const options: TSurveySchedulingTimeOption[] = [];
    for (
      let minuteOfDay = gridOffsetMinutes;
      minuteOfDay < 24 * 60;
      minuteOfDay += SURVEY_SCHEDULING_STEP_MINUTES
    ) {
      const hour = Math.floor(minuteOfDay / 60);
      const minute = minuteOfDay % 60;
      const instant = toSurveySchedulingDateTimeAt(calendarDate, hour, minute);
      const exists = matchesSurveySchedulingLocalTime(
        instant,
        calendarDate.getFullYear(),
        calendarDate.getMonth(),
        calendarDate.getDate(),
        hour,
        minute
      );
      if (exists && instant.getTime() > after.getTime()) {
        options.push({ hour, minute, instant });
      }
    }
    return options;
  };

  /** The first day, as a calendar date, with a run after an instant: the earliest day the calendar offers. */
  const getFirstSurveySchedulingCalendarDate = (after: Date): Date =>
    createLocalCalendarDate(getTimeZoneDateParts(nextSurveySchedulingRun(after)));

  const toCalendarDate = (date: Date): Date => {
    return createLocalCalendarDate(getTimeZoneDateParts(date));
  };

  const getMinimumSurveySchedulingCalendarDate = (now: Date = new Date()): Date => {
    const currentSchedulingDateParts = getTimeZoneDateParts(now);
    const minimumSchedulingDate = createLocalCalendarDate(currentSchedulingDateParts);
    const currentSchedulingRunAt = createSurveySchedulingDateTime(
      currentSchedulingDateParts.year,
      currentSchedulingDateParts.month - 1,
      currentSchedulingDateParts.day
    );

    if (now.getTime() < currentSchedulingRunAt.getTime()) {
      return minimumSchedulingDate;
    }

    const nextSchedulingDate = new Date(minimumSchedulingDate);
    nextSchedulingDate.setDate(nextSchedulingDate.getDate() + 1);
    return nextSchedulingDate;
  };

  /** The instant a day picked in the editor's calendar (a browser-local date) stands for: that day at the scheduling time in the scheduling zone, which is what the server stores. Noon UTC, what toDateOnlySelection gives, is already the next day in zones at UTC+12 and beyond (Auckland, Fiji, Kiritimati). */
  const toSurveySchedulingDateTime = (calendarDate: Date): Date =>
    createSurveySchedulingDateTime(
      calendarDate.getFullYear(),
      calendarDate.getMonth(),
      calendarDate.getDate()
    );

  const normalizeDateOnlySelectionToSurveySchedulingDateTime = (date: Date | null): Date | null => {
    if (!date) {
      return null;
    }

    const { day, month, year } = getTimeZoneDateParts(date);

    return createSurveySchedulingDateTime(year, month - 1, day);
  };

  return {
    toCalendarDate,
    getMinimumSurveySchedulingCalendarDate,
    normalizeDateOnlySelectionToSurveySchedulingDateTime,
    toSurveySchedulingDateTime,
    toSurveySchedulingDateTimeAt,
    getSurveySchedulingTimeOfDay,
    getSurveySchedulingTimeOptions,
    getFirstSurveySchedulingCalendarDate,
  };
};
