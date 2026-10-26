const DATE_ONLY_PATTERN = /^(?:(\d{4})-(\d{2})-(\d{2})|(\d{2})-(\d{2})-(\d{4}))$/;

/**
 * Parse a stored date-only value (`YYYY-MM-DD`, or the day-first `DD-MM-YYYY` the date element also
 * writes) into the calendar day it names, in the viewer's own time zone.
 *
 * Built from the parts rather than handed to `new Date(value)` for two reasons. `new Date("2026-09-21")`
 * is specified to parse as UTC midnight while `Intl.DateTimeFormat` renders in the local zone, so
 * every respondent west of UTC is shown the previous day. And `new Date("21-09-2026")` is not a
 * format the parser accepts at all, so a day-first value became an Invalid Date that
 * `Intl.DateTimeFormat.format` then threw a RangeError on.
 *
 * Returns null when the string is not a date-only value or does not name a real day — the component
 * constructor rolls 31 February over into March rather than rejecting it, so the round-trip is
 * checked.
 */
export const parseDateOnly = (value: string): Date | null => {
  const match = DATE_ONLY_PATTERN.exec(value);

  if (!match) {
    return null;
  }

  const [year, month, day] = match[1] ? [match[1], match[2], match[3]] : [match[6], match[5], match[4]];

  const yearNumber = Number(year);
  const monthNumber = Number(month);
  const dayNumber = Number(day);

  const date = new Date(yearNumber, monthNumber - 1, dayNumber);

  if (
    date.getFullYear() !== yearNumber ||
    date.getMonth() !== monthNumber - 1 ||
    date.getDate() !== dayNumber
  ) {
    return null;
  }

  return date;
};

export const isValidDateString = (value: string) => parseDateOnly(value) !== null;

const DATE_TIME_PATTERN = /^(\d{4}-\d{2}-\d{2})T(\d{2}):(\d{2})$/;

/** Parse the answer of a date element that includes the time of day (`YYYY-MM-DDTHH:mm`) into the local date and time it names; null for any other value. */
export const parseDateTime = (value: string): Date | null => {
  const match = DATE_TIME_PATTERN.exec(value);
  if (!match) return null;
  const date = parseDateOnly(match[1]);
  const hours = Number(match[2]);
  const minutes = Number(match[3]);
  if (!date || hours > 23 || minutes > 59) return null;
  date.setHours(hours, minutes);
  return date;
};

export const formatDateWithOrdinal = (date: Date, locale: string = "en-US"): string => {
  return new Intl.DateTimeFormat(locale, {
    weekday: "long",
    year: "numeric",
    month: "long",
    day: "numeric",
  }).format(date);
};

export const formatDateTimeWithOrdinal = (date: Date, locale: string = "en-US"): string => {
  return new Intl.DateTimeFormat(locale, {
    weekday: "long",
    year: "numeric",
    month: "long",
    day: "numeric",
    hour: "numeric",
    minute: "2-digit",
  }).format(date);
};
