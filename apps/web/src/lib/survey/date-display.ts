import type { TSurveyDateElement } from "@forma/types/surveys/elements";
import { formatDateForDisplay, formatDateWithOrdinal } from "./datetime";

export type TSurveyDateFormatMap = Partial<Record<string, TSurveyDateElement["format"]>>;

const ISO_STORED_DATE_PATTERN = /^(\d{4})-(\d{1,2})-(\d{1,2})$/;

/** A date element that includes the time of day stores the local date and time picked, YYYY-MM-DDTHH:mm. */
const STORED_DATE_TIME_PATTERN = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})$/;

/** The local date and time a date-and-time answer names, null when it names none. */
export const parseStoredDateTimeValue = (value: string): Date | null => {
  const match = STORED_DATE_TIME_PATTERN.exec(value);
  if (!match) return null;
  const day = buildDate(Number(match[1]), Number(match[2]), Number(match[3]));
  const hours = Number(match[4]);
  const minutes = Number(match[5]);
  if (!day || hours > 23 || minutes > 59) return null;
  day.setHours(hours, minutes);
  return day;
};

const buildDate = (year: number, month: number, day: number): Date | null => {
  if ([year, month, day].some((value) => Number.isNaN(value))) {
    return null;
  }

  const parsedDate = new Date(year, month - 1, day);

  if (
    parsedDate.getFullYear() !== year ||
    parsedDate.getMonth() !== month - 1 ||
    parsedDate.getDate() !== day
  ) {
    return null;
  }

  return parsedDate;
};

const parseLegacyStoredDateValue = (value: string, format: TSurveyDateElement["format"]): Date | null => {
  const parts = value.split("-");

  if (parts.length !== 3 || parts.some((part) => !/^\d{1,4}$/.test(part))) {
    return null;
  }

  const [first = Number.NaN, second = Number.NaN, third = Number.NaN] = parts.map(Number);

  switch (format) {
    case "M-d-y":
      return buildDate(third, first, second);
    case "d-M-y":
      return buildDate(third, second, first);
    case "y-M-d":
      return buildDate(first, second, third);
  }
};

export const parseStoredDateValue = (value: string, format?: TSurveyDateElement["format"]): Date | null => {
  const isoMatch = ISO_STORED_DATE_PATTERN.exec(value);

  if (isoMatch) {
    return buildDate(Number(isoMatch[1]), Number(isoMatch[2]), Number(isoMatch[3]));
  }

  if (format) {
    return parseLegacyStoredDateValue(value, format);
  }

  if (/^\d{1,2}-\d{1,2}-\d{4}$/.test(value)) {
    return parseLegacyStoredDateValue(value, "d-M-y");
  }

  return null;
};

export const formatStoredDateForDisplay = (
  value: string,
  format: TSurveyDateElement["format"] | undefined,
  locale: string = "en-US"
): string | null => {
  const parsedDateTime = parseStoredDateTimeValue(value);
  if (parsedDateTime) {
    return formatDateForDisplay(parsedDateTime, locale, {
      weekday: "long",
      year: "numeric",
      month: "long",
      day: "numeric",
      hour: "numeric",
      minute: "2-digit",
    });
  }

  const parsedDate = parseStoredDateValue(value, format);

  if (!parsedDate) {
    return null;
  }

  return formatDateWithOrdinal(parsedDate, locale);
};
