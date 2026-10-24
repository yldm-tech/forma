const DEFAULT_LOCALE = "en-US";

const DEFAULT_DATE_DISPLAY_OPTIONS: Intl.DateTimeFormatOptions = {
  year: "numeric",
  month: "short",
  day: "numeric",
};

const DEFAULT_DATE_TIME_DISPLAY_OPTIONS: Intl.DateTimeFormatOptions = {
  year: "numeric",
  month: "long",
  day: "numeric",
  hour: "numeric",
  minute: "2-digit",
};

export const formatDateForDisplay = (
  date: Date,
  locale: string = DEFAULT_LOCALE,
  options: Intl.DateTimeFormatOptions = DEFAULT_DATE_DISPLAY_OPTIONS
): string => {
  return new Intl.DateTimeFormat(locale, options).format(date);
};

export const formatDateTimeForDisplay = (
  date: Date,
  locale: string = DEFAULT_LOCALE,
  options: Intl.DateTimeFormatOptions = DEFAULT_DATE_TIME_DISPLAY_OPTIONS
): string => {
  return new Intl.DateTimeFormat(locale, options).format(date);
};

export const formatDateWithOrdinal = (date: Date, locale: string = DEFAULT_LOCALE): string => {
  return formatDateForDisplay(date, locale, {
    weekday: "long",
    year: "numeric",
    month: "long",
    day: "numeric",
  });
};

/**
 * Serialises a local calendar day as `yyyy-MM-dd`.
 *
 * Machine-facing on purpose — this is the value chart and dashboard time filters hand to Cube — so it
 * stays non-localized. Deliberately not `toISOString().slice(0, 10)`: that converts to UTC first, so
 * a user east of UTC would emit tomorrow's date and one west of it yesterday's.
 */
export const formatLocalDay = (date: Date): string => {
  const year = date.getFullYear();
  const month = String(date.getMonth() + 1).padStart(2, "0");
  const day = String(date.getDate()).padStart(2, "0");

  return `${year}-${month}-${day}`;
};

/**
 * Parses a `yyyy-MM-dd` day back to local midnight — the inverse of `formatLocalDay`.
 *
 * `new Date("2026-08-05")` parses as UTC midnight, which displays (and re-emits) a day earlier for
 * anyone west of UTC, so the value is rebuilt from its parts to keep the round trip symmetric.
 */
export const parseLocalDay = (value: string): Date => {
  const [year = Number.NaN, month = Number.NaN, day = Number.NaN] = value.split("-").map(Number);

  return new Date(year, month - 1, day);
};

/**
 * Parses a stored date value — a bare `yyyy-MM-dd` or a full ISO timestamp — to the local midnight of
 * the calendar day it names, or `null` when it names none.
 *
 * The time part is dropped before parsing rather than handed to `Date`: a stored
 * `2026-08-05T00:00:00.000Z` is the 5th as written, and reading it as an instant would select the 4th
 * for any viewer west of UTC. Round-tripping through `formatLocalDay` accepts exactly the values an
 * `<input type="date">` did, so a value that predates the picker still renders, a rolled-over one
 * (`2026-13-45`) is refused rather than silently shown as a different day, and an unparseable one
 * yields `null` instead of the Invalid Date that would break the calendar.
 *
 * Only the day is validated, so a well-formed day followed by a malformed time still resolves to that
 * day. That is deliberate: the day is the part a date picker shows, these values can arrive from the
 * Management API rather than only from this UI, and the `<input type="date">` this replaced split on
 * `T` the same way — blanking a readable day over a broken time suffix would lose information.
 */
export const parseStoredDay = (value: string | null | undefined): Date | null => {
  if (!value) return null;

  const day = value.split("T")[0] ?? "";
  const parsed = parseLocalDay(day);

  return formatLocalDay(parsed) === day ? parsed : null;
};
